package service

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateDeployRequestRejectsMalformedSSHKeyBeforeAnyClone(t *testing.T) {
	good := DeployRequest{VMName: "web-01", TemplateID: 1, TargetID: 1, CPU: 2, MemoryMB: 2048,
		SSHPublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGQ0rVHe4K0h1uHUn9Ua0mWsgf9Yv3gNjGk1h1b2Jv8Z user@lab\n"}
	if err := validateDeployRequest(&good); err != nil {
		t.Fatalf("a valid authorized_keys line must pass: %v", err)
	}
	bad := good
	bad.SSHPublicKey = "ssh-ed25519 not-base64-at-all"
	err := validateDeployRequest(&bad)
	if err == nil || !errors.Is(err, ErrInvalidDeployRequest) || !strings.HasPrefix(err.Error(), "invalid SSH public key: ") {
		t.Fatalf("malformed key must be a validation (400) error, got %v", err)
	}
	empty := good
	empty.SSHPublicKey = ""
	if err := validateDeployRequest(&empty); err != nil {
		t.Errorf("no key is fine: %v", err)
	}
}
