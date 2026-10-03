package vmware

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	"github.com/forgemill/forgemill/internal/provider"
)

// diskProvisioningTypes are the modes AddDisk accepts; thin is the default
// (and what CreateDisk produces). Published via ProviderMetadata so the
// service and UI work from the same list.
var diskProvisioningTypes = []string{"thin", "thick"}

func normalizeProvisioning(p string) (string, error) {
	t := strings.ToLower(strings.TrimSpace(p))
	if t == "" {
		return diskProvisioningTypes[0], nil
	}
	if !slices.Contains(diskProvisioningTypes, t) {
		return "", fmt.Errorf("invalid provisioning %q (use %s)", p, strings.Join(diskProvisioningTypes, ", "))
	}
	return t, nil
}

// diskFromDevice shapes a VirtualDisk into the provider-neutral Disk. The
// datastore name is read off the backing file name ("[ds] vm/vm.vmdk"),
// which is always present and avoids a lookup per disk.
func diskFromDevice(disk *types.VirtualDisk) provider.Disk {
	d := provider.Disk{
		Key:    int(disk.Key),
		SizeGB: int(disk.CapacityInKB / 1024 / 1024),
	}
	if disk.DeviceInfo != nil {
		d.Label = disk.DeviceInfo.GetDescription().Label
	}
	if b, ok := disk.Backing.(*types.VirtualDiskFlatVer2BackingInfo); ok {
		d.Backing = b.FileName
		d.Datastore = parseDatastoreName(b.FileName)
		if b.ThinProvisioned != nil {
			if *b.ThinProvisioned {
				d.Provisioning = "thin"
			} else {
				d.Provisioning = "thick"
			}
		}
	}
	return d
}

// AddDisk hot-adds a virtual disk on the VM's existing SCSI controller. The
// reconfigure is an Add device change with a file-create operation — no
// power cycle, no existing device touched. Without a datastore in the spec
// the new disk goes where the VM's first disk lives.
func (p *Provider) AddDisk(ctx context.Context, vmID string, spec provider.DiskSpec) (*provider.Disk, error) {
	if spec.SizeGB <= 0 {
		return nil, fmt.Errorf("size_gb must be positive")
	}
	provisioning, err := normalizeProvisioning(spec.Provisioning)
	if err != nil {
		return nil, err
	}

	client, err := p.getClient(ctx)
	if err != nil {
		return nil, err
	}
	ref := types.ManagedObjectReference{Type: "VirtualMachine", Value: vmID}
	vm := object.NewVirtualMachine(client.Client, ref)

	before, err := vm.Device(ctx)
	if err != nil {
		return nil, fmt.Errorf("get VM devices: %w", err)
	}
	controller := before.PickController((*types.VirtualSCSIController)(nil))
	if controller == nil {
		return nil, fmt.Errorf("VM has no SCSI controller to attach a disk to")
	}

	var dsRef types.ManagedObjectReference
	if strings.TrimSpace(spec.Datastore) != "" {
		// Scope the finder to the VM's datacenter — the only one whose
		// datastores this VM can use — exactly as AddNIC does for networks.
		finder := find.NewFinder(client.Client, true)
		elem, err := finder.Element(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("locate VM %s: %w", vmID, err)
		}
		dcPath, err := datacenterPathOf(elem.Path)
		if err != nil {
			return nil, err
		}
		dc, err := finder.Datacenter(ctx, dcPath)
		if err != nil {
			return nil, fmt.Errorf("find datacenter for VM %s: %w", vmID, err)
		}
		finder.SetDatacenter(dc)
		ds, err := finder.Datastore(ctx, spec.Datastore)
		if err != nil {
			var notFound *find.NotFoundError
			if errors.As(err, &notFound) {
				return nil, fmt.Errorf("%w: %q", provider.ErrDatastoreNotFound, spec.Datastore)
			}
			return nil, fmt.Errorf("find datastore %q: %w", spec.Datastore, err)
		}
		dsRef = ds.Reference()
	} else {
		dsRef, err = defaultDatastoreFor(ctx, client.Client, vm, before)
		if err != nil {
			return nil, err
		}
	}

	existing := map[int32]bool{}
	for _, dev := range before.SelectByType((*types.VirtualDisk)(nil)) {
		existing[dev.GetVirtualDevice().Key] = true
	}

	disk := before.CreateDisk(controller, dsRef, "")
	disk.CapacityInKB = int64(spec.SizeGB) * 1024 * 1024
	if b, ok := disk.Backing.(*types.VirtualDiskFlatVer2BackingInfo); ok {
		b.ThinProvisioned = types.NewBool(provisioning == "thin")
		b.EagerlyScrub = types.NewBool(false)
	}

	if err := vm.AddDevice(ctx, disk); err != nil {
		return nil, fmt.Errorf("add disk: %w", err)
	}

	after, err := vm.Device(ctx)
	if err != nil {
		// The disk exists at this point; failing would invite a retry that
		// attaches a second one.
		slog.Warn("disk added but post-add device read failed", "vmID", vmID, "error", err)
		return &provider.Disk{SizeGB: spec.SizeGB, Provisioning: provisioning}, nil
	}
	for _, dev := range after.SelectByType((*types.VirtualDisk)(nil)) {
		if existing[dev.GetVirtualDevice().Key] {
			continue
		}
		d := diskFromDevice(dev.(*types.VirtualDisk))
		return &d, nil
	}
	slog.Warn("disk added but not found in post-add device list", "vmID", vmID)
	return &provider.Disk{SizeGB: spec.SizeGB, Provisioning: provisioning}, nil
}

// defaultDatastoreFor picks the datastore of the VM's first disk, falling
// back to the first datastore the VM is associated with (a diskless VM).
func defaultDatastoreFor(ctx context.Context, c *vim25.Client, vm *object.VirtualMachine, devices object.VirtualDeviceList) (types.ManagedObjectReference, error) {
	for _, dev := range devices.SelectByType((*types.VirtualDisk)(nil)) {
		if b, ok := dev.(*types.VirtualDisk).Backing.(*types.VirtualDiskFlatVer2BackingInfo); ok && b.Datastore != nil {
			return *b.Datastore, nil
		}
	}
	var props mo.VirtualMachine
	if err := property.DefaultCollector(c).RetrieveOne(ctx, vm.Reference(), []string{"datastore"}, &props); err != nil {
		return types.ManagedObjectReference{}, fmt.Errorf("get VM datastores: %w", err)
	}
	if len(props.Datastore) == 0 {
		return types.ManagedObjectReference{}, fmt.Errorf("VM has no datastore; specify one")
	}
	return props.Datastore[0], nil
}
