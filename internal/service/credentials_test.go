package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"strings"
	"testing"

	"github.com/forgemill/forgemill/internal/db/models"
	"golang.org/x/crypto/ssh"
)

func testPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(block))
}

func TestCredentialsResolutionPrefersExplicitOverDeployment(t *testing.T) {
	svc, database, vmID := newNICTestService(t, "esxi")
	ctx := context.Background()

	// Adopted/registered VM with nothing set: a clear, typed error.
	_, err := svc.GetCredentials(ctx, vmID)
	if !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("want ErrNoCredentials, got %v", err)
	}

	// Deployment credentials are the fallback.
	user := &models.User{Username: "alice", Role: "admin", IsActive: true}
	if err := database.CreateUser(user); err != nil {
		t.Fatal(err)
	}
	encPW, _ := svc.encryptor.Encrypt("deploy-pass")
	dep := &models.Deployment{TargetID: 1, VMName: "web-01", Status: "completed", ConfigJSON: "{}", CreatedBy: user.ID, InitialUsername: "forgemill", InitialPwdEnc: encPW}
	if err := database.CreateDeployment(dep); err != nil {
		t.Fatal(err)
	}
	vm2 := &models.ManagedVM{TargetID: 1, VMName: "web-02", VMRef: "vm-101", PowerState: "poweredOn", DeploymentID: &dep.ID}
	if err := database.CreateManagedVM(vm2); err != nil {
		t.Fatal(err)
	}
	vmID = vm2.ID
	c, err := svc.GetCredentials(ctx, vmID)
	if err != nil || c.Source != CredentialSourceDeployment || c.Username != "forgemill" || c.Password != "deploy-pass" || c.Kind != "password" {
		t.Fatalf("deployment creds: %+v err %v", c, err)
	}

	// An explicit password login overrides it.
	if err := svc.SetCredentials(ctx, vmID, SetCredentialsRequest{Username: " root ", Password: "rotated"}, &user.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	c, err = svc.GetCredentials(ctx, vmID)
	if err != nil || c.Source != CredentialSourceVM || c.Username != "root" || c.Password != "rotated" || c.SetAt == nil {
		t.Fatalf("vm creds: %+v err %v", c, err)
	}
	// The executor sees the same resolution.
	vm, _ := database.GetManagedVM(vmID)
	r, err := resolveVMCredentials(database, svc.encryptor, vm)
	if err != nil || r.Password != "rotated" || r.PrivateKey != "" {
		t.Fatalf("resolve: %+v err %v", r, err)
	}

	// A private key is stored but never echoed.
	if err := svc.SetCredentials(ctx, vmID, SetCredentialsRequest{Username: "ops", PrivateKey: testPrivateKeyPEM(t)}, &user.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	c, err = svc.GetCredentials(ctx, vmID)
	if err != nil || c.Kind != "private_key" || !c.HasPrivateKey || c.Password != "" {
		t.Fatalf("key creds: %+v err %v", c, err)
	}
	r, _ = resolveVMCredentials(database, svc.encryptor, vm)
	if r.PrivateKey == "" || r.Password != "" {
		t.Fatalf("resolve key: %+v", r)
	}

	// Clearing falls back to the deployment.
	if err := svc.ClearCredentials(ctx, vmID, "alice"); err != nil {
		t.Fatal(err)
	}
	c, _ = svc.GetCredentials(ctx, vmID)
	if c.Source != CredentialSourceDeployment {
		t.Fatalf("after clear: %+v", c)
	}

	events, _ := database.ListVMEvents(vmID, 10)
	var set, cleared bool
	for _, e := range events {
		set = set || strings.Contains(e.Message, "SSH credentials set (ops, private key) by alice")
		cleared = cleared || strings.Contains(e.Message, "SSH credentials cleared by alice")
	}
	if !set || !cleared {
		t.Errorf("expected set+cleared events, got %+v", events)
	}
}

func TestSetCredentialsValidation(t *testing.T) {
	svc, _, vmID := newNICTestService(t, "esxi")
	ctx := context.Background()
	cases := map[string]SetCredentialsRequest{
		"no username":   {Password: "x"},
		"no secret":     {Username: "root"},
		"both secrets":  {Username: "root", Password: "x", PrivateKey: "y"},
		"garbage key":   {Username: "root", PrivateKey: "not a key"},
		"spaced user":   {Username: "ro ot", Password: "x"},
		"long password": {Username: "root", Password: strings.Repeat("p", 2000)},
	}
	for name, req := range cases {
		if err := svc.SetCredentials(ctx, vmID, req, nil, "api"); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("%s: want ErrInvalidCredentials, got %v", name, err)
		}
	}
	if err := svc.SetCredentials(ctx, 9999, SetCredentialsRequest{Username: "root", Password: "x"}, nil, "api"); !errors.Is(err, ErrVMNotFound) {
		t.Errorf("missing VM: want ErrVMNotFound, got %v", err)
	}
	if _, err := svc.GetCredentials(ctx, vmID); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("nothing should have been stored, got %v", err)
	}
}

// Key-based login works end to end against an SSH server that accepts public keys.
func TestSSHExecuteWithPrivateKey(t *testing.T) {
	srv := startFakeSSHD(t, "ok")
	host, port := hostPort(t, srv.addr)
	var lines []string
	code, err := sshExecute(context.Background(), host, port, "u", sshAuth{PrivateKey: testPrivateKeyPEM(t)}, "echo hi", "", func(l string) { lines = append(lines, l) }, nil, 0, 1)
	if err != nil || code != 0 || len(lines) != 1 {
		t.Fatalf("exit=%d err=%v lines=%v", code, err, lines)
	}
	if _, err := sshExecute(context.Background(), host, port, "u", sshAuth{PrivateKey: "junk"}, "echo hi", "", func(string) {}, nil, 0, 1); err == nil || !strings.Contains(err.Error(), "private key") {
		t.Errorf("bad key should fail before dialing, got %v", err)
	}
}
