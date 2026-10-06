package vmware

import (
	"context"
	"testing"

	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/types"
)

func TestIsFreeLicense(t *testing.T) {
	free := []types.LicenseManagerLicenseInfo{
		{EditionKey: "esx.hypervisor.free", Name: "VMware vSphere 8 Hypervisor"},
		{EditionKey: "esxBasic", Name: "VMware vSphere Hypervisor"},
		{EditionKey: "esx.free", Name: "Free"},
	}
	paid := []types.LicenseManagerLicenseInfo{
		{EditionKey: "eval", Name: "Evaluation Mode"},
		{EditionKey: "esx.enterprisePlus", Name: "VMware vSphere 8 Enterprise Plus"},
		{EditionKey: "esx.standard", Name: "VMware vSphere 8 Standard"},
		{EditionKey: "esx.essentials", Name: "VMware vSphere 8 Essentials Plus"},
		{EditionKey: "vc.standard", Name: "VMware vCenter Server 8 Standard"},
	}
	for _, l := range free {
		if !isFreeLicense(l) {
			t.Errorf("%+v should be free", l)
		}
	}
	for _, l := range paid {
		if isFreeLicense(l) {
			t.Errorf("%+v should not be free", l)
		}
	}
}

// vcsim runs in evaluation mode: an ESXi target reports writes allowed and
// the edition name; a vCenter target always allows writes.
func TestHostCapabilitiesEvaluationAllowsWrites(t *testing.T) {
	for _, esxi := range []bool{true, false} {
		model := simulator.VPX()
		if esxi {
			model = simulator.ESX()
		}
		p := newSimProvider(t, model, esxi)
		caps, err := p.HostCapabilities(context.Background())
		if err != nil {
			t.Fatalf("esxi=%v: %v", esxi, err)
		}
		if !caps.WritesAllowed || caps.Note != "" {
			t.Errorf("esxi=%v: evaluation mode must allow writes, got %+v", esxi, caps)
		}
		if caps.LicenseEdition == "" {
			t.Errorf("esxi=%v: edition name should be reported", esxi)
		}
	}
}
