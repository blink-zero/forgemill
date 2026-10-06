package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
	"golang.org/x/crypto/ssh"
)

// ErrNoCredentials: the VM has neither an explicit login nor deployment
// credentials — adopted and registered VMs start this way.
var ErrNoCredentials = errors.New("no SSH credentials for this VM")

// ErrInvalidCredentials wraps a rejected Set request (400).
var ErrInvalidCredentials = errors.New("invalid credentials")

// Credential sources, in resolution order.
const (
	CredentialSourceVM         = "vm"         // set explicitly on the VM
	CredentialSourceDeployment = "deployment" // inherited from the deploy
)

// VMCredentials is what the API returns for a VM's SSH login. Password is
// filled only for password logins; a private key is never echoed back.
type VMCredentials struct {
	Username      string `json:"username"`
	Password      string `json:"password,omitempty"`
	Kind          string `json:"kind"`   // password | private_key
	Source        string `json:"source"` // vm | deployment
	HasPrivateKey bool   `json:"has_private_key,omitempty"`
	// HasSudoPassword: a separate sudo password is stored (never returned).
	HasSudoPassword bool       `json:"has_sudo_password,omitempty"`
	SetAt           *time.Time `json:"set_at,omitempty"`
	SetBy           *int64     `json:"set_by,omitempty"`
}

// resolvedCredentials is the decrypted login the executor connects with.
type resolvedCredentials struct {
	Username   string
	Password   string
	PrivateKey string
	// SudoPassword is what sudo gets if it asks: the explicit sudo password,
	// else the login password for password logins, else nothing.
	SudoPassword    string
	HasSudoPassword bool
	Kind            string
	Source          string
	SetAt           *time.Time
	SetBy           *int64
}

// resolveVMCredentials picks the login for a VM: the explicit per-VM
// credential if one is set, else the deployment's initial credentials.
func resolveVMCredentials(database *db.DB, enc Encryptor, vm *models.ManagedVM) (*resolvedCredentials, error) {
	if enc == nil {
		return nil, fmt.Errorf("encryption not available")
	}
	if c, err := database.GetVMCredential(vm.ID); err != nil {
		return nil, fmt.Errorf("get VM credentials: %w", err)
	} else if c != nil {
		secret, err := enc.Decrypt(c.SecretEnc)
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve credentials")
		}
		r := &resolvedCredentials{Username: c.Username, Kind: c.Kind, Source: CredentialSourceVM, SetBy: c.SetBy}
		at := c.UpdatedAt
		r.SetAt = &at
		if c.Kind == models.CredentialKindPrivateKey {
			r.PrivateKey = secret
		} else {
			r.Password = secret
			r.SudoPassword = secret
		}
		if c.SudoPasswordEnc != "" {
			sp, err := enc.Decrypt(c.SudoPasswordEnc)
			if err != nil {
				return nil, fmt.Errorf("unable to retrieve credentials")
			}
			r.SudoPassword, r.HasSudoPassword = sp, true
		}
		return r, nil
	}
	if vm.DeploymentID == nil || *vm.DeploymentID == 0 {
		return nil, ErrNoCredentials
	}
	dep, err := database.GetDeployment(*vm.DeploymentID)
	if err != nil {
		return nil, fmt.Errorf("get deployment: %w", err)
	}
	if dep.InitialPwdEnc == "" {
		return nil, ErrNoCredentials
	}
	pwd, err := enc.Decrypt(dep.InitialPwdEnc)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve credentials")
	}
	return &resolvedCredentials{Username: dep.InitialUsername, Password: pwd, SudoPassword: pwd, Kind: models.CredentialKindPassword, Source: CredentialSourceDeployment}, nil
}

// GetCredentials returns the SSH login Forgemill would use for a VM.
func (s *VMService) GetCredentials(ctx context.Context, vmID int64) (*VMCredentials, error) {
	vm, err := s.db.GetManagedVM(vmID)
	if err != nil {
		return nil, fmt.Errorf("get VM: %w", err)
	}
	r, err := resolveVMCredentials(s.db, s.encryptor, vm)
	if err != nil {
		return nil, err
	}
	return &VMCredentials{
		Username:        r.Username,
		Password:        r.Password,
		Kind:            r.Kind,
		Source:          r.Source,
		HasPrivateKey:   r.PrivateKey != "",
		HasSudoPassword: r.HasSudoPassword,
		SetAt:           r.SetAt,
		SetBy:           r.SetBy,
	}, nil
}

// SetCredentialsRequest is the login to store for a VM: exactly one of
// Password or PrivateKey (PEM, unencrypted).
type SetCredentialsRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
	// SudoPassword: handed to sudo when it asks. Optional for password
	// logins (defaults to the login password); the only way a key login can
	// satisfy a password-prompting sudo.
	SudoPassword string `json:"sudo_password,omitempty"`
	// Force saves even when the live check fails or cannot run.
	Force bool `json:"force,omitempty"`
}

// CredentialCheck is the live result of trying the credentials on the VM.
type CredentialCheck struct {
	Skipped    bool      `json:"skipped"`               // VM off / no address: nothing was tried
	SSHOK      bool      `json:"ssh_ok"`                // login succeeded
	Sudo       SudoState `json:"sudo,omitempty"`        // see SudoState
	OK         bool      `json:"ok"`                    // SSH and sudo both usable for actions
	Message    string    `json:"message"`               // one sentence for the user
	Detail     string    `json:"detail,omitempty"`      // raw first line from ssh/sudo
	CheckedVia string    `json:"checked_via,omitempty"` // the address tried
}

// ErrCredentialCheckFailed wraps a Set that was refused because the live
// check failed; the handler returns the check with it.
type ErrCredentialCheckFailed struct {
	Check *CredentialCheck
}

func (e *ErrCredentialCheckFailed) Error() string { return e.Check.Message }

// validateCredentialsRequest normalises and checks a Set request without
// touching the network. Returns the kind and the secret to store.
func validateCredentialsRequest(req *SetCredentialsRequest) (kind, secret string, err error) {
	req.Username = strings.TrimSpace(req.Username)
	switch {
	case req.Username == "":
		return "", "", fmt.Errorf("%w: username is required", ErrInvalidCredentials)
	case len(req.Username) > 64 || strings.ContainsAny(req.Username, " \t\r\n"):
		return "", "", fmt.Errorf("%w: username must be a single word of at most 64 characters", ErrInvalidCredentials)
	case req.Password != "" && req.PrivateKey != "":
		return "", "", fmt.Errorf("%w: provide either a password or a private key, not both", ErrInvalidCredentials)
	case req.Password == "" && strings.TrimSpace(req.PrivateKey) == "":
		return "", "", fmt.Errorf("%w: a password or a private key is required", ErrInvalidCredentials)
	case len(req.SudoPassword) > 1024:
		return "", "", fmt.Errorf("%w: sudo password is too long", ErrInvalidCredentials)
	}
	kind, secret = models.CredentialKindPassword, req.Password
	if req.PrivateKey != "" {
		key := strings.TrimSpace(req.PrivateKey) + "\n"
		if len(key) > 16*1024 {
			return "", "", fmt.Errorf("%w: private key is too large", ErrInvalidCredentials)
		}
		if _, err := ssh.ParsePrivateKey([]byte(key)); err != nil {
			var pw *ssh.PassphraseMissingError
			if errors.As(err, &pw) {
				return "", "", fmt.Errorf("%w: the private key is passphrase-protected; Forgemill needs an unencrypted key", ErrInvalidCredentials)
			}
			return "", "", fmt.Errorf("%w: not a valid OpenSSH/PEM private key", ErrInvalidCredentials)
		}
		kind, secret = models.CredentialKindPrivateKey, key
	} else if len(secret) > 1024 {
		return "", "", fmt.Errorf("%w: password is too long", ErrInvalidCredentials)
	}
	return kind, secret, nil
}

// sshAuthFor builds the executor's auth from a request.
func sshAuthFor(req SetCredentialsRequest, kind, secret string) sshAuth {
	a := sshAuth{SudoPassword: req.SudoPassword}
	if kind == models.CredentialKindPrivateKey {
		a.PrivateKey = secret
	} else {
		a.Password = secret
		if a.SudoPassword == "" {
			a.SudoPassword = secret
		}
	}
	return a
}

// credentialCheckDialer is how TestCredentials reaches the VM; tests swap it.
var credentialCheckDialer = sshDial

// TestCredentials tries the credentials on the VM right now: SSH login, then
// sudo (without a password, then with one if needed and available). It never
// stores anything. A VM that is off or has no address yields Skipped.
func (s *VMService) TestCredentials(ctx context.Context, vmID int64, req SetCredentialsRequest) (*CredentialCheck, error) {
	vm, err := s.db.GetManagedVM(vmID)
	if err != nil {
		return nil, ErrVMNotFound
	}
	kind, secret, err := validateCredentialsRequest(&req)
	if err != nil {
		return nil, err
	}
	if vm.IPAddress == "" || (vm.PowerState != "poweredOn" && vm.PowerState != "running") {
		return &CredentialCheck{Skipped: true, Message: "The VM is powered off or has no address, so the credentials could not be tried. They will be checked the first time an action runs."}, nil
	}
	check := &CredentialCheck{CheckedVia: vm.IPAddress}
	auth := sshAuthFor(req, kind, secret)
	client, err := credentialCheckDialer(vm.IPAddress, 22, req.Username, auth, &dbHostKeyStore{s.db}, vm.ID)
	if err != nil {
		check.Detail = err.Error()
		check.Message = "SSH login failed: " + sshFailureHint(err)
		return check, nil
	}
	defer client.Close()
	check.SSHOK = true
	probe := probeSudo(client, auth.SudoPassword)
	check.Sudo, check.Detail = probe.State, probe.Detail
	if probe.OK() {
		check.OK = true
		if probe.State == SudoNoPasswd {
			check.Message = "SSH login and sudo both work (passwordless sudo)."
		} else {
			check.Message = "SSH login and sudo both work (Forgemill supplies the sudo password)."
		}
		return check, nil
	}
	check.Message = (&SudoError{Probe: probe}).Message()
	return check, nil
}

// sshFailureHint strips the Go plumbing from a dial error.
func sshFailureHint(err error) string {
	msg := err.Error()
	l := strings.ToLower(msg)
	switch {
	case strings.Contains(l, "unable to authenticate"), strings.Contains(l, "no supported methods"):
		return "the VM rejected the username, password or key."
	case strings.Contains(l, "host key mismatch"):
		return "the VM's SSH host key changed (reset it from the VM page if the VM was rebuilt)."
	case strings.Contains(l, "i/o timeout"), strings.Contains(l, "connection refused"), strings.Contains(l, "no route"):
		return "the VM did not answer on port 22 (" + msg + ")."
	}
	return msg
}

// SetCredentials validates, tries (unless Force) and stores an explicit SSH
// login for a VM. It replaces any previous explicit login and takes
// precedence over the deployment's credentials from now on. The returned
// check is what the live try found (Skipped when the VM was off).
func (s *VMService) SetCredentials(ctx context.Context, vmID int64, req SetCredentialsRequest, actorID *int64, actorName string) (*CredentialCheck, error) {
	vm, err := s.db.GetManagedVM(vmID)
	if err != nil {
		return nil, ErrVMNotFound
	}
	if s.encryptor == nil {
		return nil, fmt.Errorf("encryption not available")
	}
	kind, secret, err := validateCredentialsRequest(&req)
	if err != nil {
		return nil, err
	}
	check := &CredentialCheck{Skipped: true, Message: "Saved without checking."}
	if !req.Force {
		check, err = s.TestCredentials(ctx, vmID, req)
		if err != nil {
			return nil, err
		}
		if !check.Skipped && !check.OK {
			return check, &ErrCredentialCheckFailed{Check: check}
		}
	}
	enc, err := s.encryptor.Encrypt(secret)
	if err != nil {
		return nil, fmt.Errorf("encrypt credentials: %w", err)
	}
	cred := &models.VMCredential{VMID: vm.ID, Username: req.Username, Kind: kind, SecretEnc: enc, SetBy: actorID}
	if req.SudoPassword != "" {
		if cred.SudoPasswordEnc, err = s.encryptor.Encrypt(req.SudoPassword); err != nil {
			return nil, fmt.Errorf("encrypt sudo password: %w", err)
		}
	}
	if err := s.db.SetVMCredential(cred); err != nil {
		return nil, fmt.Errorf("store credentials: %w", err)
	}
	what := "password"
	if kind == models.CredentialKindPrivateKey {
		what = "private key"
	}
	if req.SudoPassword != "" {
		what += " + sudo password"
	}
	how := "checked: " + check.Message
	if check.Skipped {
		how = "not checked (VM off or no address)"
		if req.Force {
			how = "saved without checking"
		}
	}
	s.recordVMEvent(vm, "info", fmt.Sprintf("SSH credentials set (%s, %s) by %s — %s", req.Username, what, actorName, how))
	return check, nil
}

// ClearCredentials removes the explicit login; the VM falls back to its
// deployment credentials, if it has any.
func (s *VMService) ClearCredentials(ctx context.Context, vmID int64, actorName string) error {
	vm, err := s.db.GetManagedVM(vmID)
	if err != nil {
		return ErrVMNotFound
	}
	if err := s.db.DeleteVMCredential(vm.ID); err != nil {
		return fmt.Errorf("clear credentials: %w", err)
	}
	s.recordVMEvent(vm, "info", fmt.Sprintf("SSH credentials cleared by %s", actorName))
	return nil
}
