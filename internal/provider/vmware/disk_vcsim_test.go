package vmware

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/simulator"

	"github.com/forgemill/forgemill/internal/provider"
)

func TestAddDiskAttachesThinDiskOnTheVMsDatastoreAndListsIt(t *testing.T) {
	ctx := context.Background()
	p := newSimProvider(t, simulator.VPX(), false)
	m, _ := vmProps(t, ctx, p, "DC0", "DC0_H0_VM0", []string{"config.hardware"})
	vmRef := m.Reference().Value

	before, err := p.ListDisks(ctx, vmRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) == 0 || before[0].Datastore == "" || before[0].Backing == "" {
		t.Fatalf("ListDisks must now report datastore and backing: %+v", before)
	}

	disk, err := p.AddDisk(ctx, vmRef, provider.DiskSpec{SizeGB: 10})
	if err != nil {
		t.Fatalf("AddDisk: %v", err)
	}
	if disk.SizeGB != 10 || disk.Provisioning != "thin" || disk.Datastore != before[0].Datastore || disk.Key == 0 {
		t.Errorf("new disk should be 10 GB thin on the VM's datastore with a real key: %+v", disk)
	}
	after, err := p.ListDisks(ctx, vmRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before)+1 {
		t.Errorf("ListDisks should show one more disk: %d -> %d", len(before), len(after))
	}
	found := false
	for _, d := range after {
		if d.Key == disk.Key {
			found = true
			if d.SizeGB != 10 {
				t.Errorf("listed size %d", d.SizeGB)
			}
		}
	}
	if !found {
		t.Errorf("new disk key %d not in ListDisks: %+v", disk.Key, after)
	}
}

func TestAddDiskThickOnExplicitDatastoreAndUnknownDatastoreError(t *testing.T) {
	ctx := context.Background()
	// Two datastores, so "explicit datastore" is distinguishable from "the
	// VM's own": the live retest found an explicit choice silently landing
	// on the VM's datastore because only the backing file name places a disk.
	model := simulator.VPX()
	model.Datastore = 2
	p := newSimProvider(t, model, false)
	m, _ := vmProps(t, ctx, p, "DC0", "DC0_H0_VM0", []string{"config.hardware"})
	vmRef := m.Reference().Value

	before, err := p.ListDisks(ctx, vmRef)
	if err != nil || len(before) == 0 {
		t.Fatalf("ListDisks: %v %v", before, err)
	}
	other := "LocalDS_1"
	if before[0].Datastore == other {
		other = "LocalDS_0"
	}

	disk, err := p.AddDisk(ctx, vmRef, provider.DiskSpec{SizeGB: 4, Datastore: other, Provisioning: "thick"})
	if err != nil {
		t.Fatalf("AddDisk: %v", err)
	}
	if disk.Provisioning != "thick" || disk.Datastore != other {
		t.Errorf("want thick on %s (not the VM's %s), got %+v", other, before[0].Datastore, disk)
	}
	if !strings.HasPrefix(disk.Backing, "["+other+"]") {
		t.Errorf("backing file must live on the requested datastore: %q", disk.Backing)
	}
	if _, err := p.AddDisk(ctx, vmRef, provider.DiskSpec{SizeGB: 4, Datastore: "nope"}); !errors.Is(err, provider.ErrDatastoreNotFound) {
		t.Errorf("unknown datastore must be ErrDatastoreNotFound, got %v", err)
	}
	if _, err := p.AddDisk(ctx, vmRef, provider.DiskSpec{SizeGB: 4, Provisioning: "sparse"}); err == nil {
		t.Error("unknown provisioning must be rejected")
	}
}

func TestAddDiskRejectsDatastoreTheVMsHostCannotSee(t *testing.T) {
	ctx := context.Background()
	// Two hosts; a local datastore is created on the host the VM does NOT
	// run on, so it exists in the datacenter but is unreachable for the VM.
	model := simulator.VPX()
	model.Host = 2
	p := newSimProvider(t, model, false)
	m, _ := vmProps(t, ctx, p, "DC0", "DC0_H0_VM0", []string{"config.hardware", "runtime.host"})
	vmRef := m.Reference().Value

	c, _ := p.getClient(ctx)
	finder := find.NewFinder(c.Client, true)
	dc, _ := finder.Datacenter(ctx, "DC0")
	finder.SetDatacenter(dc)
	hosts, err := finder.HostSystemList(ctx, "*")
	if err != nil {
		t.Fatal(err)
	}
	var other *object.HostSystem
	for _, h := range hosts {
		if m.Runtime.Host == nil || h.Reference() != *m.Runtime.Host {
			other = h
			break
		}
	}
	if other == nil {
		t.Fatal("simulator has no second host")
	}
	dsSys, err := other.ConfigManager().DatastoreSystem(ctx)
	if err != nil {
		t.Fatal(err)
	}
	foreign := "OtherHostLocal"
	if _, err := dsSys.CreateLocalDatastore(ctx, foreign, t.TempDir()); err != nil {
		t.Fatal(err)
	}

	_, err = p.AddDisk(ctx, vmRef, provider.DiskSpec{SizeGB: 2, Datastore: foreign})
	if !errors.Is(err, provider.ErrDatastoreNotAccessible) {
		t.Fatalf("want ErrDatastoreNotAccessible for %s, got %v", foreign, err)
	}
	if !strings.Contains(err.Error(), "accessible:") || !strings.Contains(err.Error(), "LocalDS_0") {
		t.Errorf("error should list the datastores the host can see: %v", err)
	}
	// Sanity: the default placement still works on the same VM.
	if _, err := p.AddDisk(ctx, vmRef, provider.DiskSpec{SizeGB: 2}); err != nil {
		t.Errorf("default placement must still work: %v", err)
	}
}

func TestVMwareExtrasValidatorResolvesNetworkAndDatastoreInTheDatacenter(t *testing.T) {
	ctx := context.Background()
	p := newSimProvider(t, simulator.VPX(), false)
	if err := p.ValidateNICSpec(ctx, "DC0", provider.NICSpec{Network: "VM Network", VLANTag: 0}); err != nil {
		t.Errorf("known network: %v", err)
	}
	if err := p.ValidateNICSpec(ctx, "DC0", provider.NICSpec{Network: "nope"}); !errors.Is(err, provider.ErrNetworkNotFound) {
		t.Errorf("unknown network must be ErrNetworkNotFound: %v", err)
	}
	if err := p.ValidateNICSpec(ctx, "DC0", provider.NICSpec{Network: "VM Network", AdapterType: "pcnet32"}); err == nil {
		t.Error("unknown adapter model must be refused")
	}
	if err := p.ValidateDiskSpec(ctx, "DC0", provider.DiskSpec{SizeGB: 5, Datastore: "LocalDS_0", Provisioning: "thick"}); err != nil {
		t.Errorf("known datastore: %v", err)
	}
	if err := p.ValidateDiskSpec(ctx, "DC0", provider.DiskSpec{SizeGB: 5, Datastore: "nope"}); !errors.Is(err, provider.ErrDatastoreNotFound) {
		t.Errorf("unknown datastore: %v", err)
	}
	if err := p.ValidateDiskSpec(ctx, "DC0", provider.DiskSpec{SizeGB: 5, Provisioning: "sparse"}); err == nil {
		t.Error("unknown provisioning must be refused")
	}
}
