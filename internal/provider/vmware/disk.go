package vmware

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/vmware/govmomi/fault"
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
	// vSphere places a new disk by its backing *file name*, not by the
	// backing's datastore reference: an empty name means "next to the VM's
	// files", whatever datastore was referenced. So an explicit datastore
	// has to be expressed as a "[datastore]" path prefix, which makes
	// vSphere create the VMDK in a VM-named folder on that datastore.
	fileName := ""
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
		fileName = "[" + ds.Name() + "]"
		// A datastore the VM's host can't see would only fail inside the
		// reconfigure task ("Unable to access file [ds]") and surface as a
		// generic failure. Check the host's datastore list first and say
		// exactly which datastores would work.
		if err := p.checkDatastoreAccessible(ctx, client.Client, vm, dsRef, ds.Name()); err != nil {
			return nil, err
		}
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
		if fileName != "" {
			b.FileName = fileName
		}
	}

	if err := vm.AddDevice(ctx, disk); err != nil {
		if fault.Is(err, &types.CannotAccessFile{}) || fault.Is(err, &types.InvalidDatastore{}) || fault.Is(err, &types.FileNotFound{}) {
			return nil, fmt.Errorf("%w: %q (%v)", provider.ErrDatastoreNotAccessible, spec.Datastore, err)
		}
		return nil, fmt.Errorf("add disk: %w", err)
	}

	after, err := vm.Device(ctx)
	if err != nil {
		// The disk exists at this point; failing would invite a retry that
		// attaches a second one.
		provider.Warnf(ctx, "Disk added but the post-add device read failed", "vmID", vmID, "error", err)
		return &provider.Disk{SizeGB: spec.SizeGB, Provisioning: provisioning}, nil
	}
	for _, dev := range after.SelectByType((*types.VirtualDisk)(nil)) {
		if existing[dev.GetVirtualDevice().Key] {
			continue
		}
		d := diskFromDevice(dev.(*types.VirtualDisk))
		return &d, nil
	}
	provider.Warnf(ctx, "Disk added but not found in the post-add device list", "vmID", vmID)
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

// checkDatastoreAccessible verifies the datastore is mounted on the host
// the VM currently runs on. On failure the error lists the datastores that
// host can see, so the caller can pick one that works.
func (p *Provider) checkDatastoreAccessible(ctx context.Context, c *vim25.Client, vm *object.VirtualMachine, dsRef types.ManagedObjectReference, dsName string) error {
	var vmp mo.VirtualMachine
	pc := property.DefaultCollector(c)
	if err := pc.RetrieveOne(ctx, vm.Reference(), []string{"runtime.host"}, &vmp); err != nil || vmp.Runtime.Host == nil {
		return nil // no host (template, or not placed yet): let vSphere decide
	}
	var host mo.HostSystem
	if err := pc.RetrieveOne(ctx, *vmp.Runtime.Host, []string{"name", "datastore"}, &host); err != nil {
		return nil
	}
	var names []string
	for _, ref := range host.Datastore {
		if ref == dsRef {
			return nil
		}
		var ds mo.Datastore
		if pc.RetrieveOne(ctx, ref, []string{"name"}, &ds) == nil {
			names = append(names, ds.Name)
		}
	}
	return fmt.Errorf("%w: %q is not mounted on host %s (accessible: %s)", provider.ErrDatastoreNotAccessible, dsName, host.Name, strings.Join(names, ", "))
}

// deployFinder returns a Finder scoped to the deploy's datacenter (or the
// ESXi default), for validating extras before any VM exists.
func (p *Provider) deployFinder(ctx context.Context, datacenter string) (*find.Finder, error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return nil, err
	}
	finder := find.NewFinder(client.Client, true)
	dcName := datacenter
	if p.esxiMode && dcName == "" {
		dcName = "ha-datacenter"
	}
	dc, err := finder.Datacenter(ctx, dcName)
	if err != nil {
		return nil, fmt.Errorf("find datacenter %q: %w", dcName, err)
	}
	finder.SetDatacenter(dc)
	return finder, nil
}

// ValidateNICSpec implements provider.ExtrasValidator: the network must
// resolve in the deploy's datacenter (bare name or inventory path, exactly
// as AddNIC resolves it) and the adapter model must be one we create.
func (p *Provider) ValidateNICSpec(ctx context.Context, datacenter string, spec provider.NICSpec) error {
	if strings.TrimSpace(spec.Network) == "" {
		return fmt.Errorf("network is required")
	}
	if _, err := normalizeNICAdapterType(spec.AdapterType); err != nil {
		return err
	}
	finder, err := p.deployFinder(ctx, datacenter)
	if err != nil {
		return err
	}
	if _, err := finder.Network(ctx, spec.Network); err != nil {
		var notFound *find.NotFoundError
		if errors.As(err, &notFound) {
			return fmt.Errorf("%w: %q", provider.ErrNetworkNotFound, spec.Network)
		}
		return fmt.Errorf("find network %q: %w", spec.Network, err)
	}
	return nil
}

// ValidateDiskSpec implements provider.ExtrasValidator: a named datastore
// must resolve in the deploy's datacenter (host accessibility can only be
// checked once the VM is placed) and provisioning must be thin or thick.
func (p *Provider) ValidateDiskSpec(ctx context.Context, datacenter string, spec provider.DiskSpec) error {
	if spec.SizeGB <= 0 {
		return fmt.Errorf("size_gb must be positive")
	}
	if _, err := normalizeProvisioning(spec.Provisioning); err != nil {
		return err
	}
	if strings.TrimSpace(spec.Datastore) == "" {
		return nil
	}
	finder, err := p.deployFinder(ctx, datacenter)
	if err != nil {
		return err
	}
	if _, err := finder.Datastore(ctx, spec.Datastore); err != nil {
		var notFound *find.NotFoundError
		if errors.As(err, &notFound) {
			return fmt.Errorf("%w: %q", provider.ErrDatastoreNotFound, spec.Datastore)
		}
		return fmt.Errorf("find datastore %q: %w", spec.Datastore, err)
	}
	return nil
}
