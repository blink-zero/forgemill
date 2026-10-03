package vmware

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"strconv"
	"strings"
	"testing"

	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	"github.com/forgemill/forgemill/internal/provider"
)

// These tests run DeployVM against govmomi's vCenter/ESXi simulator (vcsim)
// over a real TLS session, so the whole path — Finder lookups, clone spec,
// device edits, guestinfo injection, task wait, post-clone reconfigure — is
// exercised with no mocks of our own. They pin the behaviour the deploy
// decomposition must preserve.

// newSimProvider starts the given simulator model with TLS (the provider
// hard-codes https) and returns a Provider pointed at it.
func newSimProvider(t *testing.T, model *simulator.Model, esxi bool) *Provider {
	t.Helper()
	if err := model.Create(); err != nil {
		t.Fatal(err)
	}
	model.Service.TLS = new(tls.Config)
	s := model.Service.NewServer()
	t.Cleanup(func() { s.Close(); model.Remove() })
	port, err := strconv.Atoi(s.URL.Port())
	if err != nil {
		t.Fatal(err)
	}
	pw, _ := s.URL.User.Password()
	p := New(s.URL.Hostname(), port, s.URL.User.Username(), pw, false)
	p.esxiMode = esxi
	t.Cleanup(func() { _ = p.Disconnect() })
	return p
}

func vmProps(t *testing.T, ctx context.Context, p *Provider, dc, name string, props []string) (mo.VirtualMachine, *object.VirtualMachine) {
	t.Helper()
	c, err := p.getClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	finder := find.NewFinder(c.Client, true)
	d, err := finder.Datacenter(ctx, dc)
	if err != nil {
		t.Fatal(err)
	}
	finder.SetDatacenter(d)
	vm, err := finder.VirtualMachine(ctx, name)
	if err != nil {
		t.Fatalf("VM %q not found after deploy: %v", name, err)
	}
	var m mo.VirtualMachine
	if err := property.DefaultCollector(c.Client).RetrieveOne(ctx, vm.Reference(), props, &m); err != nil {
		t.Fatal(err)
	}
	return m, vm
}

func extraConfigMap(m mo.VirtualMachine) map[string]string {
	out := map[string]string{}
	if m.Config == nil {
		return out
	}
	for _, ov := range m.Config.ExtraConfig {
		v := ov.GetOptionValue()
		if s, ok := v.Value.(string); ok {
			out[v.Key] = s
		}
	}
	return out
}

func TestDeployVMClonesTemplateApplyingSpecToTheNewVM(t *testing.T) {
	ctx := context.Background()
	p := newSimProvider(t, simulator.VPX(), false)

	// Template disk in vcsim's VPX model is small; ask for more so the resize
	// edit is exercised.
	spec := &provider.DeploySpec{
		Datacenter: "DC0", Cluster: "DC0_C0", Datastore: "LocalDS_0", Network: "VM Network",
		TemplateName: "DC0_H0_VM0", VMName: "fm-clone-01",
		CPU: 2, MemoryMB: 2048, DiskGB: 40, DiskProvisioning: "thin",
		Hostname: "fm-clone-01", DomainName: "lab.internal", DNS: []string{"10.0.0.53"},
		PasswordHash: "$6$salt$hash", PlainPassword: "pw", SSHPublicKey: "ssh-ed25519 AAAATEST e2e",
	}
	res, err := p.DeployVM(ctx, spec)
	if err != nil {
		t.Fatalf("DeployVM: %v", err)
	}
	if res.VMID == "" || res.TaskID == "" {
		t.Fatalf("result must carry the clone task and the new VM's moref, got %+v", res)
	}

	m, _ := vmProps(t, ctx, p, "DC0", "fm-clone-01", []string{"config", "runtime.powerState"})
	if m.Config.Hardware.NumCPU != 2 || m.Config.Hardware.MemoryMB != 2048 {
		t.Errorf("cpu/mem override not applied: %d vCPU, %d MB", m.Config.Hardware.NumCPU, m.Config.Hardware.MemoryMB)
	}
	if m.Runtime.PowerState != types.VirtualMachinePowerStatePoweredOn {
		t.Errorf("clone should power on, got %s", m.Runtime.PowerState)
	}
	if m.Reference().Value != res.VMID {
		t.Errorf("VMID %q is not the new VM's moref %q", res.VMID, m.Reference().Value)
	}

	ec := extraConfigMap(m)
	wantUser := base64.StdEncoding.EncodeToString([]byte(buildCloudInitUserdata(spec.PasswordHash, spec.PlainPassword, spec.SSHPublicKey)))
	wantMeta := base64.StdEncoding.EncodeToString(buildCloudInitMetadata(spec))
	if ec["guestinfo.userdata"] != wantUser || ec["guestinfo.userdata.encoding"] != "base64" {
		t.Errorf("guestinfo.userdata not injected as expected: %q", ec["guestinfo.userdata"])
	}
	if ec["guestinfo.metadata"] != wantMeta || ec["guestinfo.metadata.encoding"] != "base64" {
		t.Errorf("guestinfo.metadata not injected as expected: %q", ec["guestinfo.metadata"])
	}

	var nics, disks int
	for _, dev := range m.Config.Hardware.Device {
		switch d := dev.(type) {
		case types.BaseVirtualEthernetCard:
			nics++
			card := d.GetVirtualEthernetCard()
			if card.Connectable == nil || !card.Connectable.StartConnected || !card.Connectable.Connected {
				t.Errorf("NIC must be connected and start-connected after clone: %+v", card.Connectable)
			}
			if b, ok := card.Backing.(*types.VirtualEthernetCardNetworkBackingInfo); ok && b.DeviceName != "VM Network" {
				t.Errorf("NIC backing should be %q, got %q", "VM Network", b.DeviceName)
			}
		case *types.VirtualDisk:
			disks++
			if want := int64(40) * 1024 * 1024; d.CapacityInKB != want {
				t.Errorf("disk should be resized to %d KB, got %d", want, d.CapacityInKB)
			}
		}
	}
	if nics == 0 || disks == 0 {
		t.Errorf("clone lost devices: %d NICs, %d disks", nics, disks)
	}
}

func TestDeployVMWithoutOverridesLeavesTemplateShapeAlone(t *testing.T) {
	ctx := context.Background()
	p := newSimProvider(t, simulator.VPX(), false)
	tmpl, _ := vmProps(t, ctx, p, "DC0", "DC0_H0_VM0", []string{"config.hardware"})

	res, err := p.DeployVM(ctx, &provider.DeploySpec{Datacenter: "DC0", Cluster: "DC0_C0", TemplateName: "DC0_H0_VM0", VMName: "fm-clone-plain"})
	if err != nil {
		t.Fatalf("DeployVM: %v", err)
	}
	m, _ := vmProps(t, ctx, p, "DC0", "fm-clone-plain", []string{"config"})
	if m.Config.Hardware.NumCPU != tmpl.Config.Hardware.NumCPU || m.Config.Hardware.MemoryMB != tmpl.Config.Hardware.MemoryMB {
		t.Errorf("no CPU/mem in spec must mean template values: got %d/%d want %d/%d",
			m.Config.Hardware.NumCPU, m.Config.Hardware.MemoryMB, tmpl.Config.Hardware.NumCPU, tmpl.Config.Hardware.MemoryMB)
	}
	if ec := extraConfigMap(m); ec["guestinfo.userdata"] != "" || ec["guestinfo.metadata"] != "" {
		t.Errorf("no credentials in spec must mean no guestinfo injection, got %v", ec)
	}
	if res.VMID != m.Reference().Value {
		t.Errorf("VMID mismatch: %q vs %q", res.VMID, m.Reference().Value)
	}
}

func TestDeployVMErrorsNameTheMissingResourceAndCloneNothing(t *testing.T) {
	ctx := context.Background()
	p := newSimProvider(t, simulator.VPX(), false)
	base := provider.DeploySpec{Datacenter: "DC0", Cluster: "DC0_C0", TemplateName: "DC0_H0_VM0", VMName: "fm-should-not-exist"}

	cases := []struct {
		name string
		mut  func(*provider.DeploySpec)
		want string
	}{
		{"datacenter", func(s *provider.DeploySpec) { s.Datacenter = "nope" }, `find datacenter "nope"`},
		{"template", func(s *provider.DeploySpec) { s.TemplateName = "nope" }, `find template "nope"`},
		{"folder", func(s *provider.DeploySpec) { s.Folder = "nope" }, `find folder "nope"`},
		{"cluster", func(s *provider.DeploySpec) { s.Cluster = "nope" }, `find resource pool`},
		{"datastore", func(s *provider.DeploySpec) { s.Datastore = "nope" }, `find datastore "nope"`},
		{"host", func(s *provider.DeploySpec) { s.Host = "nope" }, `find host "nope"`},
		{"network", func(s *provider.DeploySpec) { s.Network = "nope" }, `find network "nope"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := base
			tc.mut(&spec)
			_, err := p.DeployVM(ctx, &spec)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}

	c, _ := p.getClient(ctx)
	finder := find.NewFinder(c.Client, true)
	dc, _ := finder.Datacenter(ctx, "DC0")
	finder.SetDatacenter(dc)
	if _, err := finder.VirtualMachine(ctx, "fm-should-not-exist"); err == nil {
		t.Error("a failed lookup must not leave a cloned VM behind")
	}
}

func TestESXiFallbackCopiesDiskRegistersAndPowersOn(t *testing.T) {
	ctx := context.Background()
	p := newSimProvider(t, simulator.ESX(), true)
	c, err := p.getClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	finder := find.NewFinder(c.Client, true)
	dc, err := finder.Datacenter(ctx, "ha-datacenter")
	if err != nil {
		t.Fatal(err)
	}
	finder.SetDatacenter(dc)
	vms, err := finder.VirtualMachineList(ctx, "*")
	if err != nil || len(vms) == 0 {
		t.Fatalf("ESX model should have a VM to use as template: %v", err)
	}
	tmpl := vms[0]
	folder, err := finder.DefaultFolder(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := p.findResourcePool(ctx, finder, "")
	if err != nil {
		t.Fatal(err)
	}
	var props mo.VirtualMachine
	if err := property.DefaultCollector(c.Client).RetrieveOne(ctx, tmpl.Reference(), []string{"config.hardware.device", "config.guestId", "config.hardware.numCPU", "config.hardware.memoryMB", "config.firmware"}, &props); err != nil {
		t.Fatal(err)
	}
	var origKB int64
	for _, dev := range props.Config.Hardware.Device {
		if d, ok := dev.(*types.VirtualDisk); ok {
			origKB = d.CapacityInKB
			break
		}
	}

	spec := &provider.DeploySpec{TemplateName: tmpl.Name(), VMName: "fm-esxi-01", CPU: 2, MemoryMB: 1024, DiskGB: int(origKB/1024/1024) + 8,
		Network: "VM Network", PasswordHash: "$6$salt$hash", Hostname: "fm-esxi-01"}
	res, err := p.esxiDeployFallback(ctx, spec, c.Client, dc, finder, folder, pool, &props, origKB, props.Config.Firmware)
	if err != nil {
		t.Fatalf("esxiDeployFallback: %v", err)
	}
	if res.VMID == "" || res.TaskID == "" {
		t.Fatalf("result must carry the create task and the new VM's moref, got %+v", res)
	}
	m, _ := vmProps(t, ctx, p, "ha-datacenter", "fm-esxi-01", []string{"config", "runtime.powerState"})
	if m.Config.Hardware.NumCPU != 2 || m.Config.Hardware.MemoryMB != 1024 {
		t.Errorf("cpu/mem not applied: %d vCPU, %d MB", m.Config.Hardware.NumCPU, m.Config.Hardware.MemoryMB)
	}
	if m.Runtime.PowerState != types.VirtualMachinePowerStatePoweredOn {
		t.Errorf("fallback should power on, got %s", m.Runtime.PowerState)
	}
	ec := extraConfigMap(m)
	if ec["guestinfo.userdata"] == "" || ec["guestinfo.metadata"] == "" {
		t.Errorf("fallback must inject the same guestinfo keys as the clone path, got %v", ec)
	}
	var nics, disks int
	for _, dev := range m.Config.Hardware.Device {
		switch dev.(type) {
		case types.BaseVirtualEthernetCard:
			nics++
		case *types.VirtualDisk:
			disks++
		}
	}
	if nics != 1 || disks != 1 {
		t.Errorf("fallback VM should have exactly one NIC and one disk, got %d/%d", nics, disks)
	}
}
