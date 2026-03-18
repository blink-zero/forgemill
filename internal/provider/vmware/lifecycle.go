package vmware

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	"github.com/forgemill/forgemill/internal/provider"
)

func (p *Provider) PowerOn(ctx context.Context, vmID string) error {
	vm, err := p.getVM(ctx, vmID)
	if err != nil {
		return err
	}
	task, err := vm.PowerOn(ctx)
	if err != nil {
		return fmt.Errorf("power on: %w", err)
	}
	return task.Wait(ctx)
}

// PowerOff tries a graceful guest shutdown first (ShutdownGuest via VMware Tools),
// waits up to 90s for the VM to power off, then falls back to a hard PowerOff.
func (p *Provider) PowerOff(ctx context.Context, vmID string) error {
	vm, err := p.getVM(ctx, vmID)
	if err != nil {
		return err
	}

	// Try graceful shutdown via VMware Tools. This is a fire-and-forget guest op —
	// govmomi returns immediately; we then poll for powered-off state.
	if err := vm.ShutdownGuest(ctx); err != nil {
		slog.Info("guest shutdown not available, falling back to hard power off", "vmID", vmID, "error", err)
		return p.hardPowerOff(ctx, vm)
	}

	// Poll up to 90s for the VM to reach powered-off state.
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
		status, err := p.GetVMStatus(ctx, vmID)
		if err == nil && status.PowerState == "poweredOff" {
			return nil
		}
	}

	// Guest didn't shut down in time — fall back to hard power off.
	slog.Info("guest shutdown timed out, falling back to hard power off", "vmID", vmID)
	return p.hardPowerOff(ctx, vm)
}

func (p *Provider) hardPowerOff(ctx context.Context, vm *object.VirtualMachine) error {
	task, err := vm.PowerOff(ctx)
	if err != nil {
		return fmt.Errorf("power off: %w", err)
	}
	return task.Wait(ctx)
}

// PV-V11: Restart tries graceful reboot first, falls back to hard reset.
func (p *Provider) Restart(ctx context.Context, vmID string) error {
	vm, err := p.getVM(ctx, vmID)
	if err != nil {
		return err
	}
	if err := vm.RebootGuest(ctx); err != nil {
		slog.Info("guest reboot failed, falling back to hard reset", "vmID", vmID, "error", err)
		task, err := vm.Reset(ctx)
		if err != nil {
			return fmt.Errorf("reset VM: %w", err)
		}
		return task.Wait(ctx)
	}
	return nil
}

func (p *Provider) Suspend(ctx context.Context, vmID string) error {
	vm, err := p.getVM(ctx, vmID)
	if err != nil {
		return err
	}
	task, err := vm.Suspend(ctx)
	if err != nil {
		return fmt.Errorf("suspend: %w", err)
	}
	return task.Wait(ctx)
}

// PV-V10: Use fault type assertion instead of fragile string matching.
func (p *Provider) DeleteVM(ctx context.Context, vmID string) error {
	vm, err := p.getVM(ctx, vmID)
	if err != nil {
		return err
	}
	task, err := vm.PowerOff(ctx)
	if err != nil {
		if !isInvalidPowerStateFault(err) {
			slog.Warn("failed to power off VM before deletion", "vmID", vmID, "error", err)
		}
	} else {
		if waitErr := task.Wait(ctx); waitErr != nil {
			if !isInvalidPowerStateFault(waitErr) {
				slog.Warn("power off task failed before deletion", "vmID", vmID, "error", waitErr)
			}
		}
	}
	task, err = vm.Destroy(ctx)
	if err != nil {
		return fmt.Errorf("destroy VM: %w", err)
	}
	return task.Wait(ctx)
}

func isInvalidPowerStateFault(err error) bool {
	if fault, ok := err.(types.HasFault); ok {
		if _, ok := fault.Fault().(*types.InvalidPowerState); ok {
			return true
		}
	}
	return false
}

func (p *Provider) GetVMStatus(ctx context.Context, vmID string) (*provider.VMStatus, error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return nil, err
	}

	ref := types.ManagedObjectReference{Type: "VirtualMachine", Value: vmID}
	var vm mo.VirtualMachine
	pc := property.DefaultCollector(client.Client)
	if err := pc.RetrieveOne(ctx, ref, []string{"guest", "runtime", "config"}, &vm); err != nil {
		return nil, fmt.Errorf("get VM status: %w", err)
	}

	status := &provider.VMStatus{
		PowerState: string(vm.Runtime.PowerState),
	}
	if vm.Guest != nil {
		status.IPAddress = vm.Guest.IpAddress
		status.HostName = vm.Guest.HostName
	}
	if vm.Config != nil {
		status.CPU = int(vm.Config.Hardware.NumCPU)
		status.MemoryMB = int(vm.Config.Hardware.MemoryMB)
		status.GuestID = vm.Config.GuestId
		// Calculate total disk size from virtual disks
		for _, dev := range vm.Config.Hardware.Device {
			if disk, ok := dev.(*types.VirtualDisk); ok {
				status.DiskGB += int(disk.CapacityInKB / 1024 / 1024)
			}
		}
	}
	return status, nil
}

func (p *Provider) ListSnapshots(ctx context.Context, vmID string) ([]provider.Snapshot, error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return nil, err
	}

	ref := types.ManagedObjectReference{Type: "VirtualMachine", Value: vmID}
	var vm mo.VirtualMachine
	pc := property.DefaultCollector(client.Client)
	if err := pc.RetrieveOne(ctx, ref, []string{"snapshot"}, &vm); err != nil {
		return nil, fmt.Errorf("get snapshots: %w", err)
	}

	snapshots := []provider.Snapshot{}
	if vm.Snapshot == nil {
		return snapshots, nil
	}

	var walk func(trees []types.VirtualMachineSnapshotTree)
	walk = func(trees []types.VirtualMachineSnapshotTree) {
		for _, tree := range trees {
			snapshots = append(snapshots, provider.Snapshot{
				Ref:         tree.Snapshot.Value,
				Name:        tree.Name,
				Description: tree.Description,
				Created:     tree.CreateTime.String(),
			})
			walk(tree.ChildSnapshotList)
		}
	}
	walk(vm.Snapshot.RootSnapshotList)
	return snapshots, nil
}

func (p *Provider) CreateSnapshot(ctx context.Context, vmID string, name string, description string, memory bool) error {
	vm, err := p.getVM(ctx, vmID)
	if err != nil {
		return err
	}
	task, err := vm.CreateSnapshot(ctx, name, description, memory, false)
	if err != nil {
		return fmt.Errorf("create snapshot: %w", err)
	}
	return task.Wait(ctx)
}

func (p *Provider) RevertSnapshot(ctx context.Context, vmID string, snapshotRef string) error {
	vm, err := p.getVM(ctx, vmID)
	if err != nil {
		return err
	}
	task, err := vm.RevertToSnapshot(ctx, snapshotRef, true)
	if err != nil {
		return fmt.Errorf("revert snapshot: %w", err)
	}
	return task.Wait(ctx)
}

func (p *Provider) DeleteSnapshot(ctx context.Context, vmID string, snapshotRef string) error {
	vm, err := p.getVM(ctx, vmID)
	if err != nil {
		return err
	}
	consolidate := true
	task, err := vm.RemoveSnapshot(ctx, snapshotRef, false, &consolidate)
	if err != nil {
		return fmt.Errorf("delete snapshot: %w", err)
	}
	return task.Wait(ctx)
}

// PV-V12: ResizeVM checks power state and hot-add capability before reconfigure.
func (p *Provider) ResizeVM(ctx context.Context, vmID string, cpu int, memoryMB int) error {
	client, err := p.getClient(ctx)
	if err != nil {
		return err
	}

	ref := types.ManagedObjectReference{Type: "VirtualMachine", Value: vmID}
	var vmProps mo.VirtualMachine
	pc := property.DefaultCollector(client.Client)
	if err := pc.RetrieveOne(ctx, ref, []string{"runtime.powerState", "config.cpuHotAddEnabled", "config.memoryHotAddEnabled"}, &vmProps); err != nil {
		return fmt.Errorf("get VM config: %w", err)
	}

	if string(vmProps.Runtime.PowerState) == "poweredOn" {
		if cpu > 0 && (vmProps.Config == nil || vmProps.Config.CpuHotAddEnabled == nil || !*vmProps.Config.CpuHotAddEnabled) {
			return fmt.Errorf("VM must be powered off to change CPU (hot-add not enabled)")
		}
		if memoryMB > 0 && (vmProps.Config == nil || vmProps.Config.MemoryHotAddEnabled == nil || !*vmProps.Config.MemoryHotAddEnabled) {
			return fmt.Errorf("VM must be powered off to change memory (hot-add not enabled)")
		}
	}

	vm := object.NewVirtualMachine(client.Client, ref)
	spec := types.VirtualMachineConfigSpec{}
	if cpu > 0 {
		spec.NumCPUs = int32(cpu)
	}
	if memoryMB > 0 {
		spec.MemoryMB = int64(memoryMB)
	}
	task, err := vm.Reconfigure(ctx, spec)
	if err != nil {
		return fmt.Errorf("resize VM: %w", err)
	}
	return task.Wait(ctx)
}

func (p *Provider) ListDisks(ctx context.Context, vmID string) ([]provider.Disk, error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return nil, err
	}
	ref := types.ManagedObjectReference{Type: "VirtualMachine", Value: vmID}
	var vmProps mo.VirtualMachine
	pc := property.DefaultCollector(client.Client)
	if err := pc.RetrieveOne(ctx, ref, []string{"config.hardware"}, &vmProps); err != nil {
		return nil, fmt.Errorf("get VM hardware: %w", err)
	}
	if vmProps.Config == nil {
		return nil, fmt.Errorf("VM configuration not available")
	}
	var disks []provider.Disk
	for _, dev := range vmProps.Config.Hardware.Device {
		disk, ok := dev.(*types.VirtualDisk)
		if !ok {
			continue
		}
		label := ""
		if disk.DeviceInfo != nil {
			label = disk.DeviceInfo.GetDescription().Label
		}
		disks = append(disks, provider.Disk{
			Key:    int(disk.Key),
			Label:  label,
			SizeGB: int(disk.CapacityInKB / 1024 / 1024),
		})
	}
	return disks, nil
}

// PV-V9: ExpandDisk uses getClient() consistently instead of p.client directly.
func (p *Provider) ExpandDisk(ctx context.Context, vmID string, diskKey int, newSizeGB int) error {
	client, err := p.getClient(ctx)
	if err != nil {
		return err
	}

	ref := types.ManagedObjectReference{Type: "VirtualMachine", Value: vmID}
	vm := object.NewVirtualMachine(client.Client, ref)

	var vmProps mo.VirtualMachine
	pc := property.DefaultCollector(client.Client)
	if err := pc.RetrieveOne(ctx, vm.Reference(), []string{"config.hardware"}, &vmProps); err != nil {
		return fmt.Errorf("get VM hardware: %w", err)
	}

	if vmProps.Config == nil {
		return fmt.Errorf("VM configuration not available")
	}
	for _, dev := range vmProps.Config.Hardware.Device {
		disk, ok := dev.(*types.VirtualDisk)
		if !ok || disk.Key != int32(diskKey) {
			continue
		}
		disk.CapacityInKB = int64(newSizeGB) * 1024 * 1024
		spec := types.VirtualMachineConfigSpec{
			DeviceChange: []types.BaseVirtualDeviceConfigSpec{
				&types.VirtualDeviceConfigSpec{
					Operation: types.VirtualDeviceConfigSpecOperationEdit,
					Device:    disk,
				},
			},
		}
		task, err := vm.Reconfigure(ctx, spec)
		if err != nil {
			return fmt.Errorf("expand disk: %w", err)
		}
		return task.Wait(ctx)
	}
	return fmt.Errorf("disk with key %d not found", diskKey)
}

func (p *Provider) GetConsoleURL(ctx context.Context, vmID string) (string, error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return "", err
	}

	// HIGH-02: Log console access for audit trail.
	slog.Warn("console access requested",
		"vm_id", vmID,
		"hostname", p.hostname,
		"esxi_mode", p.esxiMode,
	)

	if p.esxiMode {
		// ESXi HTML5 embedded host client — works in any browser, no VMRC needed
		// Acquire a session ticket so the user doesn't need to re-authenticate
		mgr := session.NewManager(client.Client)
		ticket, err := mgr.AcquireCloneTicket(ctx)
		if err != nil {
			// Fall back to basic URL without ticket
			return fmt.Sprintf("https://%s:%d/ui/#/host/vms/%s/console", p.hostname, p.port, vmID), nil
		}
		return fmt.Sprintf("https://%s:%d/ui/#/host/vms/%s/console?clonesession=%s", p.hostname, p.port, vmID, ticket), nil
	}

	// vCenter: use VMRC protocol
	mgr := session.NewManager(client.Client)
	ticket, err := mgr.AcquireCloneTicket(ctx)
	if err != nil {
		return "", fmt.Errorf("acquire clone ticket: %w", err)
	}

	return fmt.Sprintf("vmrc://clone:%s@%s:%d/?moid=%s", ticket, p.hostname, p.port, vmID), nil
}

func (p *Provider) ListVMs(ctx context.Context) ([]provider.VMInfo, error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return nil, err
	}

	mgr := view.NewManager(client.Client)
	containerView, err := mgr.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		return nil, fmt.Errorf("create container view: %w", err)
	}
	defer containerView.Destroy(ctx)

	var vms []mo.VirtualMachine
	if err := containerView.Retrieve(ctx, []string{"VirtualMachine"}, []string{"name", "config", "guest", "runtime"}, &vms); err != nil {
		return nil, fmt.Errorf("retrieve VMs: %w", err)
	}

	result := []provider.VMInfo{}
	for _, vm := range vms {
		if vm.Config != nil && vm.Config.Template {
			continue
		}
		info := provider.VMInfo{
			ID:         vm.Reference().Value,
			Name:       vm.Name,
			PowerState: string(vm.Runtime.PowerState),
		}
		if vm.Guest != nil {
			info.IPAddress = vm.Guest.IpAddress
		}
		if vm.Config != nil {
			info.CPU = int(vm.Config.Hardware.NumCPU)
			info.MemoryMB = int(vm.Config.Hardware.MemoryMB)
			info.GuestID = vm.Config.GuestId
		}
		result = append(result, info)
	}
	return result, nil
}

func (p *Provider) getVM(ctx context.Context, vmID string) (*object.VirtualMachine, error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return nil, err
	}
	ref := types.ManagedObjectReference{Type: "VirtualMachine", Value: vmID}
	return object.NewVirtualMachine(client.Client, ref), nil
}
