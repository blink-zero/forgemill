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
	Username      string     `json:"username"`
	Password      string     `json:"password,omitempty"`
	Kind          string     `json:"kind"`   // password | private_key
	Source        string     `json:"source"` // vm | deployment
	HasPrivateKey bool       `json:"has_private_key,omitempty"`
	SetAt         *time.Time `json:"set_at,omitempty"`
	SetBy         *int64     `json:"set_by,omitempty"`
}

// resolvedCredentials is the decrypted login the executor connects with.
type resolvedCredentials struct {
	Username   string
	Password   string
	PrivateKey string
	Kind       string
	Source     string
	SetAt      *time.Time
	SetBy      *int64
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
	return &resolvedCredentials{Username: dep.InitialUsername, Password: pwd, Kind: models.CredentialKindPassword, Source: CredentialSourceDeployment}, nil
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
		Username:      r.Username,
		Password:      r.Password,
		Kind:          r.Kind,
		Source:        r.Source,
		HasPrivateKey: r.PrivateKey != "",
		SetAt:         r.SetAt,
		SetBy:         r.SetBy,
	}, nil
}

// SetCredentialsRequest is the login to store for a VM: exactly one of
// Password or PrivateKey (PEM, unencrypted).
type SetCredentialsRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
}

// SetCredentials validates, encrypts and stores an explicit SSH login for a
// VM. It replaces any previous explicit login and takes precedence over the
// deployment's credentials from now on.
func (s *VMService) SetCredentials(ctx context.Context, vmID int64, req SetCredentialsRequest, actorID *int64, actorName string) error {
	vm, err := s.db.GetManagedVM(vmID)
	if err != nil {
		return ErrVMNotFound
	}
	if s.encryptor == nil {
		return fmt.Errorf("encryption not available")
	}
	username := strings.TrimSpace(req.Username)
	switch {
	case username == "":
		return fmt.Errorf("%w: username is required", ErrInvalidCredentials)
	case len(username) > 64 || strings.ContainsAny(username, " \t\r\n"):
		return fmt.Errorf("%w: username must be a single word of at most 64 characters", ErrInvalidCredentials)
	case req.Password != "" && req.PrivateKey != "":
		return fmt.Errorf("%w: provide either a password or a private key, not both", ErrInvalidCredentials)
	case req.Password == "" && strings.TrimSpace(req.PrivateKey) == "":
		return fmt.Errorf("%w: a password or a private key is required", ErrInvalidCredentials)
	}
	kind, secret := models.CredentialKindPassword, req.Password
	if req.PrivateKey != "" {
		key := strings.TrimSpace(req.PrivateKey) + "\n"
		if len(key) > 16*1024 {
			return fmt.Errorf("%w: private key is too large", ErrInvalidCredentials)
		}
		if _, err := ssh.ParsePrivateKey([]byte(key)); err != nil {
			var pw *ssh.PassphraseMissingError
			if errors.As(err, &pw) {
				return fmt.Errorf("%w: the private key is passphrase-protected; Forgemill needs an unencrypted key", ErrInvalidCredentials)
			}
			return fmt.Errorf("%w: not a valid OpenSSH/PEM private key", ErrInvalidCredentials)
		}
		kind, secret = models.CredentialKindPrivateKey, key
	} else if len(secret) > 1024 {
		return fmt.Errorf("%w: password is too long", ErrInvalidCredentials)
	}
	enc, err := s.encryptor.Encrypt(secret)
	if err != nil {
		return fmt.Errorf("encrypt credentials: %w", err)
	}
	if err := s.db.SetVMCredential(&models.VMCredential{VMID: vm.ID, Username: username, Kind: kind, SecretEnc: enc, SetBy: actorID}); err != nil {
		return fmt.Errorf("store credentials: %w", err)
	}
	what := "password"
	if kind == models.CredentialKindPrivateKey {
		what = "private key"
	}
	s.recordVMEvent(vm, "info", fmt.Sprintf("SSH credentials set (%s, %s) by %s", username, what, actorName))
	return nil
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
