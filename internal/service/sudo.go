package service

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Forgemill runs every action under sudo. Whether that works depends on the
// SSH user's sudoers entry, which for adopted and registered VMs is whatever
// the user set up, not what cloud-init gave the forgemill user. probeSudo
// finds out before the script runs so the failure can be explained.

// SudoState is the outcome of probing sudo for the SSH user.
type SudoState string

const (
	SudoNoPasswd       SudoState = "nopasswd"        // sudo -n works
	SudoViaPassword    SudoState = "password"        // sudo wants a password and ours is accepted
	SudoNeedsPassword  SudoState = "needs_password"  // sudo wants a password; we have none to give
	SudoWrongPassword  SudoState = "wrong_password"  // sudo wants a password; ours is rejected
	SudoNotPermitted   SudoState = "not_permitted"   // user is not in sudoers / command not allowed
	SudoRequiresTTY    SudoState = "requiretty"      // sudoers has Defaults requiretty
	SudoProbeFailed    SudoState = "probe_failed"    // could not run the probe at all
	SudoUnknownFailure SudoState = "unknown_failure" // sudo failed for a reason we don't recognise
)

// SudoProbe is what probeSudo found.
type SudoProbe struct {
	State  SudoState
	Detail string // the first relevant line sudo printed, for the curious
}

// OK reports whether a script can be run under sudo with what we have.
func (p SudoProbe) OK() bool { return p.State == SudoNoPasswd || p.State == SudoViaPassword }

// Err turns a failed probe into the typed error the executor records.
func (p SudoProbe) Err() error {
	if p.OK() {
		return nil
	}
	return &SudoError{Probe: p}
}

// SudoError: SSH login worked but sudo did not. Message() is what a user
// should read instead of the raw sudo output.
type SudoError struct {
	Probe SudoProbe
}

func (e *SudoError) Error() string { return e.Message() }

// Message explains the failure and the fix in plain words.
func (e *SudoError) Message() string {
	switch e.Probe.State {
	case SudoNeedsPassword:
		return "SSH login succeeded, but this user's sudo asks for a password and none is stored. Forgemill actions run under sudo: add the user's sudo password to the VM's SSH credentials, configure passwordless sudo (NOPASSWD) for the user, or use a user that already has it."
	case SudoWrongPassword:
		return "SSH login succeeded, but sudo rejected the stored sudo password. Update the VM's SSH credentials with the user's current password, or configure passwordless sudo (NOPASSWD) for the user."
	case SudoNotPermitted:
		return "SSH login succeeded, but this user is not allowed to run sudo on the VM. Forgemill actions run under sudo: add the user to sudoers (ideally with NOPASSWD), or set credentials for a user that can sudo."
	case SudoRequiresTTY:
		return "SSH login succeeded, but sudo on this VM insists on a terminal (sudoers has `Defaults requiretty`). Forgemill runs actions without one: add `Defaults:USERNAME !requiretty` to sudoers for this user."
	case SudoProbeFailed:
		return "SSH login succeeded, but Forgemill could not check sudo on the VM: " + e.Probe.Detail
	default:
		d := e.Probe.Detail
		if d == "" {
			d = "no output"
		}
		return "SSH login succeeded, but sudo failed on the VM: " + d + ". Forgemill actions run under sudo; check this user's sudoers entry."
	}
}

// sudoWithPassword is the sudo invocation used when the password is fed on
// stdin: -S reads it from there, -p ” keeps the prompt out of stderr.
const sudoWithPassword = "sudo -S -p ''"

const sudoProbeTimeout = 20 * time.Second

// probeSudo asks the guest whether this user can sudo: first without a
// password (-n), then — if sudo asked for one and we hold one — with it.
func probeSudo(client *ssh.Client, sudoPassword string) SudoProbe {
	code, out, err := runQuiet(client, "sudo -n true", "")
	if err != nil {
		return SudoProbe{State: SudoProbeFailed, Detail: err.Error()}
	}
	if code == 0 {
		return SudoProbe{State: SudoNoPasswd}
	}
	state := classifySudoOutput(out)
	if state != SudoNeedsPassword {
		return SudoProbe{State: state, Detail: firstLine(out)}
	}
	if sudoPassword == "" {
		return SudoProbe{State: SudoNeedsPassword, Detail: firstLine(out)}
	}
	// -k: ignore any cached credential so the password itself is tested.
	code, out, err = runQuiet(client, sudoWithPassword+" -k true", sudoPassword+"\n")
	if err != nil {
		return SudoProbe{State: SudoProbeFailed, Detail: err.Error()}
	}
	if code == 0 {
		return SudoProbe{State: SudoViaPassword}
	}
	if st := classifySudoOutput(out); st == SudoNeedsPassword || st == SudoWrongPassword {
		return SudoProbe{State: SudoWrongPassword, Detail: firstLine(out)}
	} else if st != SudoUnknownFailure {
		return SudoProbe{State: st, Detail: firstLine(out)}
	}
	return SudoProbe{State: SudoUnknownFailure, Detail: firstLine(out)}
}

// classifySudoOutput maps sudo's stderr to a SudoState. Patterns are the
// ones sudo has printed for years across distributions.
func classifySudoOutput(out string) SudoState {
	l := strings.ToLower(out)
	switch {
	case strings.Contains(l, "incorrect password attempt"), strings.Contains(l, "sorry, try again"), strings.Contains(l, "no password was provided"):
		return SudoWrongPassword
	case strings.Contains(l, "is not in the sudoers file"), strings.Contains(l, "may not run sudo"), strings.Contains(l, "is not allowed to execute"), strings.Contains(l, "not allowed to run sudo"):
		return SudoNotPermitted
	case strings.Contains(l, "must have a tty"), strings.Contains(l, "requiretty"):
		return SudoRequiresTTY
	case strings.Contains(l, "a password is required"), strings.Contains(l, "a terminal is required to read the password"):
		return SudoNeedsPassword
	}
	return SudoUnknownFailure
}

// runQuiet runs one command on a fresh session with optional stdin and
// returns its exit code and combined output, bounded by sudoProbeTimeout.
func runQuiet(client *ssh.Client, cmd, stdin string) (int, string, error) {
	s, err := client.NewSession()
	if err != nil {
		return -1, "", fmt.Errorf("ssh session: %w", err)
	}
	defer s.Close()
	// stdout and stderr are copied by separate goroutines inside x/crypto/ssh,
	// so they need separate buffers.
	var stdout, stderr bytes.Buffer
	s.Stdout, s.Stderr = &stdout, &stderr
	if stdin != "" {
		s.Stdin = strings.NewReader(stdin)
	}
	done := make(chan error, 1)
	go func() { done <- s.Run(cmd) }()
	select {
	case err := <-done:
		out := stdout.String() + stderr.String()
		if err != nil {
			if exitErr, ok := err.(*ssh.ExitError); ok {
				return exitErr.ExitStatus(), out, nil
			}
			return -1, out, fmt.Errorf("ssh run: %w", err)
		}
		return 0, out, nil
	case <-time.After(sudoProbeTimeout):
		// Run is still copying into the buffers; don't read them.
		_ = s.Close()
		return -1, "", fmt.Errorf("sudo check did not return within %s", sudoProbeTimeout)
	}
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return ""
}
