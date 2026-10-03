package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/forgemill/forgemill/internal/provider"
)

func TestAddDiskPassesSpecThroughAndResyncs(t *testing.T) {
	svc, _, vmID := newNICTestService(t, "esxi")
	disk, err := svc.AddDisk(context.Background(), vmID, AddDiskRequest{SizeGB: 20, Datastore: " ds-nvme-01 ", Provisioning: "Thick"})
	if err != nil {
		t.Fatalf("AddDisk: %v", err)
	}
	if disk == nil || disk.Key != 2001 || disk.SizeGB != 20 {
		t.Errorf("unexpected disk: %+v", disk)
	}
	if len(fakeNIC.addDiskSpec) != 1 {
		t.Fatalf("expected one provider AddDisk call, got %d", len(fakeNIC.addDiskSpec))
	}
	if got := fakeNIC.addDiskSpec[0]; got.SizeGB != 20 || got.Datastore != "ds-nvme-01" || got.Provisioning != "thick" {
		t.Errorf("spec not normalised/passed through: %+v", got)
	}
	if fakeNIC.statusCalls == 0 {
		t.Error("a successful attach must re-sync the VM record")
	}
}

func TestAddDiskRejectsBadSizeAndUnknownProvisioningAsCallerErrors(t *testing.T) {
	svc, _, vmID := newNICTestService(t, "esxi")
	for _, req := range []AddDiskRequest{{SizeGB: 0}, {SizeGB: 70000}, {SizeGB: 10, Provisioning: "sparse"}} {
		_, err := svc.AddDisk(context.Background(), vmID, req)
		if !errors.Is(err, ErrInvalidDiskSpec) {
			t.Errorf("%+v: want ErrInvalidDiskSpec, got %v", req, err)
		}
	}
	if len(fakeNIC.addDiskSpec) != 0 {
		t.Errorf("rejected requests must not reach the provider: %v", fakeNIC.addDiskSpec)
	}
}

func TestAddDiskRefusesProvisioningWhereTheStorageDecides(t *testing.T) {
	svc, _, vmID := newNICTestService(t, "esxi")
	orig := provider.GetMetadata("esxi")
	noChoice := *orig
	noChoice.DiskProvisioningTypes = nil
	provider.RegisterMetadata("esxi", &noChoice)
	t.Cleanup(func() { provider.RegisterMetadata("esxi", orig) })

	_, err := svc.AddDisk(context.Background(), vmID, AddDiskRequest{SizeGB: 10, Provisioning: "thin"})
	if !errors.Is(err, ErrInvalidDiskSpec) || !strings.Contains(err.Error(), "storage decides") {
		t.Errorf("want a clear refusal, got %v", err)
	}
	if _, err := svc.AddDisk(context.Background(), vmID, AddDiskRequest{SizeGB: 10}); err != nil {
		t.Errorf("without provisioning the request must go through: %v", err)
	}
}

func TestAddDiskUnsupportedProviderRefusesWithoutConnecting(t *testing.T) {
	svc, _, vmID := newNICTestService(t, "esxi")
	orig := provider.GetMetadata("esxi")
	noDisk := *orig
	noDisk.Features.DiskAttach = false
	provider.RegisterMetadata("esxi", &noDisk)
	t.Cleanup(func() { provider.RegisterMetadata("esxi", orig) })

	_, err := svc.AddDisk(context.Background(), vmID, AddDiskRequest{SizeGB: 10})
	if !errors.Is(err, provider.ErrNotSupported) {
		t.Fatalf("expected ErrNotSupported, got %v", err)
	}
	if len(fakeNIC.addDiskSpec) != 0 {
		t.Error("provider must not be called when metadata says DiskAttach is unsupported")
	}
}

func TestAddDiskMissingVM(t *testing.T) {
	svc, _, _ := newNICTestService(t, "esxi")
	if _, err := svc.AddDisk(context.Background(), 9999, AddDiskRequest{SizeGB: 10}); !errors.Is(err, ErrVMNotFound) {
		t.Errorf("want ErrVMNotFound, got %v", err)
	}
}
