package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
	"golang.org/x/crypto/ssh"
)

func setVMState(t *testing.T, database *db.DB, id int64, state, ip string) {
	t.Helper()
	if err := database.UpdateManagedVMState(id, state, ip, nil, nil, nil, 0); err != nil {
		t.Fatal(err)
	}
}

// A user whose sudo asks for a password: Forgemill feeds the stored one on
// stdin, the script follows it, and nothing about the password is on the
// command line.
func TestSSHExecuteFeedsSudoPasswordOnStdinWhenSudoAsks(t *testing.T) {
	srv := startFakeSSHD(t, "ok")
	srv.setSudo("password", "hunter2")
	host, port := hostPort(t, srv.addr)
	var lines []string
	code, err := sshExecute(context.Background(), host, port, "u", sshAuth{Password: "hunter2", SudoPassword: "hunter2"}, "echo hi", "", func(l string) { lines = append(lines, l) }, nil, 0, 9)
	if err != nil || code != 0 || len(lines) != 1 {
		t.Fatalf("exit=%d err=%v lines=%v", code, err, lines)
	}
	probes := srv.probeCommands()
	if len(probes) != 2 || probes[0] != "sudo -n true" || probes[1] != sudoWithPassword+" -k true" {
		t.Errorf("probe sequence: %q", probes)
	}
	cmds := srv.commands()
	if len(cmds) != 1 || cmds[0] != sudoWithPassword+" bash -c 'exec -a forgemill-exec-9 bash'" {
		t.Errorf("job command: %q", cmds)
	}
	if strings.Contains(cmds[0], "hunter2") {
		t.Errorf("password must never be on the command line: %q", cmds[0])
	}
	srv.mu.Lock()
	stdins := append([]string(nil), srv.stdins...)
	srv.mu.Unlock()
	if len(stdins) != 1 || !strings.HasPrefix(stdins[0], "set -euo pipefail\nexport DEBIAN_FRONTEND=noninteractive\necho hi\n") {
		t.Errorf("script must follow the password on stdin, got %q", stdins)
	}
}

func TestSSHExecuteExplainsSudoFailures(t *testing.T) {
	cases := []struct {
		name, sudo, pw, want string
		state                SudoState
	}{
		{"needs password, none held", "password", "", "none is stored", SudoNeedsPassword},
		{"wrong password", "password", "nope", "rejected the stored sudo password", SudoWrongPassword},
		{"not in sudoers", "denied", "", "not allowed to run sudo", SudoNotPermitted},
		{"requiretty", "requiretty", "", "requiretty", SudoRequiresTTY},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := startFakeSSHD(t, "ok")
			srv.setSudo(tc.sudo, "hunter2")
			host, port := hostPort(t, srv.addr)
			_, err := sshExecute(context.Background(), host, port, "u", sshAuth{Password: "login", SudoPassword: tc.pw}, "echo hi", "", func(string) {}, nil, 0, 1)
			var se *SudoError
			if !errors.As(err, &se) {
				t.Fatalf("want *SudoError, got %v", err)
			}
			if se.Probe.State != tc.state {
				t.Errorf("state = %s, want %s (%s)", se.Probe.State, tc.state, se.Probe.Detail)
			}
			if !strings.Contains(se.Message(), tc.want) || !strings.Contains(se.Message(), "SSH login succeeded") {
				t.Errorf("message must say the login worked and what to do: %q", se.Message())
			}
			if cmds := srv.commands(); len(cmds) != 0 {
				t.Errorf("the script must not run when sudo is unusable, ran %q", cmds)
			}
		})
	}
}

func TestClassifySudoOutput(t *testing.T) {
	cases := map[string]SudoState{
		"sudo: a terminal is required to read the password; either use the -S option to read from standard input or configure an askpass helper\nsudo: a password is required": SudoNeedsPassword,
		"bob is not in the sudoers file.  This incident will be reported.":       SudoNotPermitted,
		"Sorry, user bob is not allowed to execute '/bin/bash' as root on host.": SudoNotPermitted,
		"sudo: sorry, you must have a tty to run sudo":                           SudoRequiresTTY,
		"Sorry, try again.\nsudo: 1 incorrect password attempt":                  SudoWrongPassword,
		"sudo: no password was provided":                                         SudoWrongPassword,
		"segfault":                                                               SudoUnknownFailure,
	}
	for out, want := range cases {
		if got := classifySudoOutput(out); got != want {
			t.Errorf("%q → %s, want %s", out, got, want)
		}
	}
}

// TestCredentials against the fake guest: four distinct outcomes, nothing stored.
func TestTestCredentialsDistinguishesSSHFromSudo(t *testing.T) {
	svc, database, vmID := newNICTestService(t, "esxi")
	ctx := context.Background()
	srv := startFakeSSHD(t, "ok")
	host, port := hostPort(t, srv.addr)
	// Point the VM at the fake and make it look alive.
	setVMState(t, database, vmID, "poweredOn", host)
	// Dial the fake's port instead of 22.
	credentialCheckDialer = func(h string, _ int, u string, a sshAuth, hk HostKeyStore, id int64) (*ssh.Client, error) {
		return sshDial(h, port, u, a, hk, id)
	}
	t.Cleanup(func() { credentialCheckDialer = sshDial })

	// Off / no address → skipped (nothing tried).
	setVMState(t, database, vmID, "poweredOff", host)
	chk, err := svc.TestCredentials(ctx, vmID, SetCredentialsRequest{Username: "u", Password: "p"})
	if err != nil || !chk.Skipped {
		t.Fatalf("off VM: %+v err %v", chk, err)
	}
	setVMState(t, database, vmID, "poweredOn", host)

	// NOPASSWD: OK.
	chk, err = svc.TestCredentials(ctx, vmID, SetCredentialsRequest{Username: "u", Password: "p"})
	if err != nil || !chk.OK || !chk.SSHOK || chk.Sudo != SudoNoPasswd {
		t.Fatalf("nopasswd: %+v err %v", chk, err)
	}
	// Password sudo, login password accepted by sudo: OK via password.
	srv.setSudo("password", "p")
	chk, _ = svc.TestCredentials(ctx, vmID, SetCredentialsRequest{Username: "u", Password: "p"})
	if !chk.OK || chk.Sudo != SudoViaPassword {
		t.Fatalf("password sudo: %+v", chk)
	}
	// Key login, sudo wants a password, none given: SSH ok, sudo not.
	chk, _ = svc.TestCredentials(ctx, vmID, SetCredentialsRequest{Username: "u", PrivateKey: testPrivateKeyPEM(t)})
	if chk.OK || !chk.SSHOK || chk.Sudo != SudoNeedsPassword {
		t.Fatalf("key w/o sudo pw: %+v", chk)
	}
	// Key login + sudo password: OK.
	chk, _ = svc.TestCredentials(ctx, vmID, SetCredentialsRequest{Username: "u", PrivateKey: testPrivateKeyPEM(t), SudoPassword: "p"})
	if !chk.OK || chk.Sudo != SudoViaPassword {
		t.Fatalf("key + sudo pw: %+v", chk)
	}
	// Wrong sudo password.
	chk, _ = svc.TestCredentials(ctx, vmID, SetCredentialsRequest{Username: "u", PrivateKey: testPrivateKeyPEM(t), SudoPassword: "wrong"})
	if chk.OK || chk.Sudo != SudoWrongPassword {
		t.Fatalf("wrong sudo pw: %+v", chk)
	}
	// Not in sudoers.
	srv.setSudo("denied", "p")
	chk, _ = svc.TestCredentials(ctx, vmID, SetCredentialsRequest{Username: "u", Password: "p"})
	if chk.OK || chk.Sudo != SudoNotPermitted || !strings.Contains(chk.Message, "not allowed to run sudo") {
		t.Fatalf("denied: %+v", chk)
	}
	// Nothing was stored by any of that.
	if got, _ := database.GetVMCredential(vmID); got != nil {
		t.Fatalf("TestCredentials must not store: %+v", got)
	}

	// SetCredentials refuses on a failed check and stores on force.
	_, err = svc.SetCredentials(ctx, vmID, SetCredentialsRequest{Username: "u", Password: "p"}, nil, "api")
	var failed *ErrCredentialCheckFailed
	if !errors.As(err, &failed) || failed.Check.Sudo != SudoNotPermitted {
		t.Fatalf("want ErrCredentialCheckFailed(not_permitted), got %v", err)
	}
	if got, _ := database.GetVMCredential(vmID); got != nil {
		t.Fatalf("refused set must not store: %+v", got)
	}
	chk, err = svc.SetCredentials(ctx, vmID, SetCredentialsRequest{Username: "u", Password: "p", Force: true}, nil, "api")
	if err != nil || !chk.Skipped {
		t.Fatalf("force: %+v err %v", chk, err)
	}
	if got, _ := database.GetVMCredential(vmID); got == nil || got.Kind != models.CredentialKindPassword {
		t.Fatalf("forced set must store: %+v", got)
	}
	// And a passing check stores with the check attached.
	srv.setSudo("nopasswd", "p")
	chk, err = svc.SetCredentials(ctx, vmID, SetCredentialsRequest{Username: "u", Password: "p"}, nil, "api")
	if err != nil || !chk.OK {
		t.Fatalf("ok set: %+v err %v", chk, err)
	}
}
