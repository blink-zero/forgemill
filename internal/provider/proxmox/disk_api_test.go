package proxmox

import (
	"context"
	"errors"
	"testing"

	"github.com/forgemill/forgemill/internal/provider"
)

func TestAddDiskAllocatesOnTheVMsStorageInTheNextFreeSlot(t *testing.T) {
	f := newFakePVE(t)
	p := f.provider(t)

	disk, err := p.AddDisk(context.Background(), "100", provider.DiskSpec{SizeGB: 10})
	if err != nil {
		t.Fatalf("AddDisk: %v", err)
	}
	if len(f.puts) != 1 || f.puts[0].Get("scsi1") != "local-zfs:10" || len(f.puts[0]) != 1 {
		t.Errorf("want exactly one PUT scsi1=local-zfs:10 (storage inferred from scsi0), got %v", f.puts)
	}
	if disk.Key != 1 || disk.Label != "scsi1" || disk.SizeGB != 10 || disk.Datastore != "local-zfs" || disk.Backing != "local-zfs:vm-100-disk-1" {
		t.Errorf("returned disk should reflect the allocated volume: %+v", disk)
	}
	disks, err := p.ListDisks(context.Background(), "100")
	if err != nil || len(disks) != 2 || disks[1].Label != "scsi1" || disks[1].SizeGB != 10 || disks[0].Datastore != "local-zfs" {
		t.Errorf("ListDisks after add: %v %v", disks, err)
	}
}

func TestAddDiskHonoursExplicitStorageAndRejectsUnknownOne(t *testing.T) {
	f := newFakePVE(t)
	p := f.provider(t)

	if _, err := p.AddDisk(context.Background(), "100", provider.DiskSpec{SizeGB: 5, Datastore: "local"}); err != nil {
		t.Fatalf("AddDisk on known storage: %v", err)
	}
	if f.puts[0].Get("scsi1") != "local:5" {
		t.Errorf("explicit storage must be used: %v", f.puts)
	}
	_, err := p.AddDisk(context.Background(), "100", provider.DiskSpec{SizeGB: 5, Datastore: "nope"})
	if !errors.Is(err, provider.ErrDatastoreNotFound) {
		t.Errorf("unknown storage must be ErrDatastoreNotFound, got %v", err)
	}
	if len(f.puts) != 1 {
		t.Errorf("rejected storage must not write config: %v", f.puts)
	}
}

func TestEnumerateDisksParsesVolumesAndSkipsCloudInitSize(t *testing.T) {
	cfg := qemuConfig{
		"scsi0": "local-lvm:vm-100-disk-0,size=32G,format=raw",
		"scsi2": "local:vm-100-cloudinit,media=cdrom",
		"ide0":  "nfs:100/vm-100-disk-1.qcow2,size=8G",
		"net0":  "virtio=..,bridge=vmbr0",
	}
	disks := enumerateDisks(cfg)
	if len(disks) != 3 {
		t.Fatalf("want 3 disk slots, got %+v", disks)
	}
	if disks[0].Key != 0 || disks[0].Label != "scsi0" || disks[0].SizeGB != 32 || disks[0].Datastore != "local-lvm" || disks[0].Provisioning != "raw" {
		t.Errorf("scsi0: %+v", disks[0])
	}
	if disks[1].Key != 1 || disks[1].Label != "scsi2" || disks[1].SizeGB != 0 || disks[1].Datastore != "local" {
		t.Errorf("cloud-init slot: %+v", disks[1])
	}
	if disks[2].Key != 2 || disks[2].Label != "ide0" || disks[2].SizeGB != 8 || disks[2].Datastore != "nfs" {
		t.Errorf("ide0: %+v", disks[2])
	}
}
