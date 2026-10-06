package vmware

import (
	"context"
	"testing"
	"time"

	"github.com/vmware/govmomi/find"
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

// ESXi keeps saying "Evaluation Mode" after the 60 days; the expiration
// properties are what tell the truth.
func TestEvalExpired(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name           string
		props          []types.KeyAnyValue
		expired, known bool
	}{
		{"no expiration info", nil, false, false},
		{"hours left", []types.KeyAnyValue{{Key: "expirationHours", Value: int32(1200)}}, false, true},
		{"hours exhausted", []types.KeyAnyValue{{Key: "expirationHours", Value: int32(0)}}, true, true},
		{"minutes exhausted as string", []types.KeyAnyValue{{Key: "expirationMinutes", Value: "0"}}, true, true},
		{"date in the past", []types.KeyAnyValue{{Key: "expirationDate", Value: now.Add(-24 * time.Hour)}}, true, true},
		{"date in the future", []types.KeyAnyValue{{Key: "expirationDate", Value: now.Add(24 * time.Hour)}}, false, true},
		{"date string past", []types.KeyAnyValue{{Key: "expirationDate", Value: "2026-09-01T00:00:00Z"}}, true, true},
		{"unrelated props", []types.KeyAnyValue{{Key: "feature", Value: "dvs"}}, false, false},
	}
	for _, tc := range cases {
		expired, known := evalExpired(tc.props, now)
		if expired != tc.expired || known != tc.known {
			t.Errorf("%s: expired=%v known=%v, want %v/%v", tc.name, expired, known, tc.expired, tc.known)
		}
	}
}

// The write probe succeeds on vcsim (evaluation, writes allowed) and leaves
// nothing behind on the datastore.
func TestProbeWriteSucceedsAndCleansUp(t *testing.T) {
	p := newSimProvider(t, simulator.ESX(), true)
	ctx := context.Background()
	refused, err := p.probeWrite(ctx)
	if err != nil || refused {
		t.Fatalf("refused=%v err=%v", refused, err)
	}
	c, _ := p.getClient(ctx)
	finder := find.NewFinder(c.Client, true)
	dc, _ := finder.DefaultDatacenter(ctx)
	finder.SetDatacenter(dc)
	dss, _ := finder.DatastoreList(ctx, "*")
	b, _ := dss[0].Browser(ctx)
	task, err := b.SearchDatastore(ctx, "["+dss[0].Name()+"]", &types.HostDatastoreBrowserSearchSpec{MatchPattern: []string{".forgemill-probe-*"}})
	if err != nil {
		t.Fatal(err)
	}
	info, err := task.WaitForResult(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res, ok := info.Result.(types.HostDatastoreBrowserSearchResults); ok && len(res.File) != 0 {
		t.Errorf("probe directory left behind: %+v", res.File)
	}
}
