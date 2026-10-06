package provider

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsLicenseRestricted(t *testing.T) {
	raw := errors.New("start VMDK copy: ServerFaultCode: Current license or ESXi version prohibits execution of the requested operation.")
	if !IsLicenseRestricted(raw) {
		t.Error("the vSphere RestrictedVersion fault text must be recognised")
	}
	if !IsLicenseRestricted(fmt.Errorf("power on: %w", raw)) {
		t.Error("wrapped fault text must be recognised")
	}
	if !IsLicenseRestricted(fmt.Errorf("clone: %w", ErrLicenseRestricted)) {
		t.Error("sentinel must be recognised")
	}
	if IsLicenseRestricted(errors.New("datastore not found")) || IsLicenseRestricted(nil) {
		t.Error("other errors are not the license gate")
	}
}
