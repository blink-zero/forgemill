package vmware

import (
	"context"
	"errors"
	"strings"
	"testing"

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
