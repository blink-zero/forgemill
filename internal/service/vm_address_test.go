package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/forgemill/forgemill/internal/crypto"
	"github.com/forgemill/forgemill/internal/db/models"
	"golang.org/x/crypto/ssh"
)

func TestNormalizeAddress(t *testing.T) {
	cases := map[string]struct {
		want string
		ok   bool
	}{
		"":                       {"", true},
		"   ":                    {"", true},
		"10.0.0.5":               {"10.0.0.5", true},
		" 10.0.0.5 ":             {"10.0.0.5", true},
		"fe80::1":                {"fe80::1", true},
		"web-01":                 {"web-01", true},
		"Web-01.Lab.Example.":    {"web-01.lab.example", true},
		"10.0.0.5:22":            {"", false},
		"ssh://10.0.0.5":         {"", false},
		"-bad.example":           {"", false},
		"bad_host":               {"", false},
		"has space.example":      {"", false},
		strings.Repeat("a", 254): {"", false},
	}
	for in, c := range cases {
		got, err := normalizeAddress(in)
		if c.ok && (err != nil || got != c.want) {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, c.want)
		}
		if !c.ok && !errors.Is(err, ErrInvalidAddress) {
			t.Errorf("%q: want ErrInvalidAddress, got %q, %v", in, got, err)
		}
	}
}

func TestManagedVMAddressPrefersOverride(t *testing.T) {
	vm := &models.ManagedVM{IPAddress: "10.0.0.5"}
	if vm.Address() != "10.0.0.5" {
		t.Fatalf("got %q", vm.Address())
	}
	vm.AddressOverride = "web-01.lab"
	if vm.Address() != "web-01.lab" {
		t.Fatalf("got %q", vm.Address())
	}
}

func TestSetAddressOverrideStoresClearsAndSurvivesSync(t *testing.T) {
	database := newTestDB(t)
	enc, _ := crypto.NewEncryptor(nicTestKey)
	encPW, _ := enc.Encrypt("x")
	target := &models.Target{Name: "pve", Type: "proxmox", Hostname: "pve.example.com", Port: 8006, Username: "root@pam", PasswordEncrypt: encPW}
	if err := database.CreateTarget(target); err != nil {
		t.Fatal(err)
	}
	vm := &models.ManagedVM{TargetID: target.ID, VMName: "adopted-01", VMRef: "101", PowerState: "poweredOn", Origin: "adopted"}
	if err := database.CreateManagedVM(vm); err != nil {
		t.Fatal(err)
	}
	svc := NewVMService(database, nil, enc)
	ctx := context.Background()

	if _, err := svc.SetAddressOverride(ctx, vm.ID, "10.0.0.5:22", "alice"); !errors.Is(err, ErrInvalidAddress) {
		t.Fatalf("want ErrInvalidAddress, got %v", err)
	}
	if _, err := svc.SetAddressOverride(ctx, vm.ID+99, "10.0.0.5", "alice"); !errors.Is(err, ErrVMNotFound) {
		t.Fatalf("want ErrVMNotFound, got %v", err)
	}

	got, err := svc.SetAddressOverride(ctx, vm.ID, " 10.0.0.5 ", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if got.AddressOverride != "10.0.0.5" || got.Address() != "10.0.0.5" {
		t.Fatalf("after set: %+v", got)
	}
	reread, _ := database.GetManagedVM(vm.ID)
	if reread.AddressOverride != "10.0.0.5" {
		t.Fatalf("not persisted: %+v", reread)
	}

	// A sync that still can't see an address must not disturb the override,
	// and one that can must not win over it either.
	if _, err := svc.updateVMPowerState(reread, "poweredOn", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.updateVMPowerState(reread, "poweredOn", "10.0.0.77"); err != nil {
		t.Fatal(err)
	}
	reread, _ = database.GetManagedVM(vm.ID)
	if reread.IPAddress != "10.0.0.77" || reread.AddressOverride != "10.0.0.5" || reread.Address() != "10.0.0.5" {
		t.Fatalf("after sync: ip=%q override=%q", reread.IPAddress, reread.AddressOverride)
	}

	if _, err := svc.SetAddressOverride(ctx, vm.ID, "", "alice"); err != nil {
		t.Fatal(err)
	}
	reread, _ = database.GetManagedVM(vm.ID)
	if reread.AddressOverride != "" || reread.Address() != "10.0.0.77" {
		t.Fatalf("after clear: %+v", reread)
	}

	events, err := database.ListVMEvents(vm.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, e := range events {
		msgs = append(msgs, e.Message)
	}
	joined := strings.Join(msgs, "\n")
	if !strings.Contains(joined, "Address set manually to 10.0.0.5 by alice") || !strings.Contains(joined, "Manual address cleared by alice") {
		t.Fatalf("events: %q", msgs)
	}
}

// Execute's preflight refusals carry a reason the caller can show, and the
// no-address case names the guest agent and the manual override.
func TestExecuteRefusalsAreExplainedAndHonourOverride(t *testing.T) {
	database := newTestDB(t)
	enc, _ := crypto.NewEncryptor(nicTestKey)
	encPW, _ := enc.Encrypt("x")
	target := &models.Target{Name: "pve", Type: "proxmox", Hostname: "pve.example.com", Port: 8006, Username: "root@pam", PasswordEncrypt: encPW}
	if err := database.CreateTarget(target); err != nil {
		t.Fatal(err)
	}
	admin := &models.User{Username: "alice", Role: "admin", IsActive: true, PasswordHash: "x"}
	if err := database.CreateUser(admin); err != nil {
		t.Fatal(err)
	}
	vm := &models.ManagedVM{TargetID: target.ID, VMName: "adopted-01", VMRef: "101", PowerState: "poweredOff", Origin: "adopted"}
	if err := database.CreateManagedVM(vm); err != nil {
		t.Fatal(err)
	}
	ex := NewExecutorService(database, nil, enc, nil)
	ctx := context.Background()
	req := ExecuteRequest{Script: "#!/bin/bash\necho hi"}

	var refused *RefusedError
	_, err := ex.Execute(ctx, vm.ID, req, admin.ID)
	if !errors.As(err, &refused) || !strings.Contains(refused.Reason, "powered on") {
		t.Fatalf("powered off: %v", err)
	}

	// Powered on, no address from the hypervisor, no override.
	if err := database.UpdateManagedVMState(vm.ID, "poweredOn", "", nil, nil, nil, 0); err != nil {
		t.Fatal(err)
	}
	_, err = ex.Execute(ctx, vm.ID, req, admin.ID)
	if !errors.As(err, &refused) || !errors.Is(err, ErrNoAddress) {
		t.Fatalf("no address: %v", err)
	}
	for _, want := range []string{"guest agent", "QEMU", "VMware Tools", "manually"} {
		if !strings.Contains(refused.Reason, want) {
			t.Errorf("no-address reason lacks %q: %s", want, refused.Reason)
		}
	}

	// An override satisfies the address check; the next refusal is the
	// missing login.
	if err := database.SetManagedVMAddressOverride(vm.ID, "192.0.2.10"); err != nil {
		t.Fatal(err)
	}
	_, err = ex.Execute(ctx, vm.ID, req, admin.ID)
	if !errors.As(err, &refused) || !errors.Is(err, ErrNoCredentials) || !strings.Contains(refused.Reason, "VM page") {
		t.Fatalf("no credentials: %v", err)
	}
}

// The credential test reports the no-address case the same way, so the
// card can point at the fix instead of saying "off or no address".
func TestTestCredentialsReportsNoAddress(t *testing.T) {
	database := newTestDB(t)
	enc, _ := crypto.NewEncryptor(nicTestKey)
	encPW, _ := enc.Encrypt("x")
	target := &models.Target{Name: "pve", Type: "proxmox", Hostname: "pve.example.com", Port: 8006, Username: "root@pam", PasswordEncrypt: encPW}
	if err := database.CreateTarget(target); err != nil {
		t.Fatal(err)
	}
	vm := &models.ManagedVM{TargetID: target.ID, VMName: "adopted-01", VMRef: "101", PowerState: "poweredOn", Origin: "adopted"}
	if err := database.CreateManagedVM(vm); err != nil {
		t.Fatal(err)
	}
	svc := NewVMService(database, nil, enc)
	check, err := svc.TestCredentials(context.Background(), vm.ID, SetCredentialsRequest{Username: "root", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	if !check.Skipped || !check.NoAddress || !strings.Contains(check.Message, "guest agent") {
		t.Fatalf("check: %+v", check)
	}

	// With an override the dialer is pointed at it.
	var dialed string
	orig := credentialCheckDialer
	credentialCheckDialer = func(host string, port int, username string, auth sshAuth, hk HostKeyStore, vmID int64) (*ssh.Client, error) {
		dialed = host
		return nil, errors.New("unable to authenticate")
	}
	t.Cleanup(func() { credentialCheckDialer = orig })
	if err := database.SetManagedVMAddressOverride(vm.ID, "web-01.lab"); err != nil {
		t.Fatal(err)
	}
	check, err = svc.TestCredentials(context.Background(), vm.ID, SetCredentialsRequest{Username: "root", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	if dialed != "web-01.lab" || check.Skipped || check.CheckedVia != "web-01.lab" {
		t.Fatalf("dialed %q, check %+v", dialed, check)
	}
}
