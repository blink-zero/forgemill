package vmware

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	"github.com/forgemill/forgemill/internal/provider"
)

// PV-V14: ListTemplates uses ContainerView instead of VirtualMachineList for efficiency.
// PV-V15: Only retrieves "config" property (removed unused "summary").
func (p *Provider) ListTemplates(ctx context.Context) ([]provider.Template, error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return nil, err
	}

	finder := find.NewFinder(client.Client, true)

	dcPattern := "*"
	if p.esxiMode {
		dcPattern = "ha-datacenter"
	}

	dcs, err := finder.DatacenterList(ctx, dcPattern)
	if err != nil {
		return nil, fmt.Errorf("list datacenters: %w", err)
	}

	templates := []provider.Template{}
	var dcErrors int
	mgr := view.NewManager(client.Client)

	for _, dc := range dcs {
		// PV-V14: Use ContainerView for efficient template enumeration
		containerView, err := mgr.CreateContainerView(ctx, dc.Reference(), []string{"VirtualMachine"}, true)
		if err != nil {
			slog.Warn("failed to create container view", "datacenter", dc.Name(), "error", err)
			dcErrors++
			continue
		}

		var moVMs []mo.VirtualMachine
		if err := containerView.Retrieve(ctx, []string{"VirtualMachine"}, []string{"config", "runtime"}, &moVMs); err != nil {
			slog.Warn("failed to retrieve VM properties", "datacenter", dc.Name(), "error", err)
			containerView.Destroy(ctx)
			dcErrors++
			continue
		}
		containerView.Destroy(ctx)

		for _, vm := range moVMs {
			if vm.Config == nil {
				continue
			}
			// In ESXi standalone mode, templates can't be marked as such
			// (MarkAsTemplate is a vCenter-only API). Treat powered-off VMs
			// as templates — this is the standard ESXi practice.
			if !vm.Config.Template && !p.esxiMode {
				continue
			}
			// In ESXi mode, skip powered-on VMs (they're not templates)
			if p.esxiMode && !vm.Config.Template {
				if vm.Runtime.PowerState != types.VirtualMachinePowerStatePoweredOff {
					continue
				}
			}
			t := provider.Template{
				ID:    vm.Reference().Value,
				Name:  vm.Config.Name,
				Moref: vm.Reference().Value,
			}
			if vm.Config.GuestId != "" {
				t.GuestID = vm.Config.GuestId
				t.OSType = guessOSType(vm.Config.GuestId)
			}
			if vm.Config.Hardware.NumCPU > 0 {
				t.CPU = int(vm.Config.Hardware.NumCPU)
			}
			if vm.Config.Hardware.MemoryMB > 0 {
				t.MemoryMB = int(vm.Config.Hardware.MemoryMB)
			}
			t.DiskGB = totalDiskGB(vm.Config.Hardware.Device)
			templates = append(templates, t)
		}
	}
	if dcErrors > 0 && dcErrors == len(dcs) {
		return nil, fmt.Errorf("failed to list templates from all %d datacenter(s)", len(dcs))
	}
	return templates, nil
}

func (p *Provider) GetTemplate(ctx context.Context, id string) (*provider.Template, error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return nil, err
	}

	ref := types.ManagedObjectReference{Type: "VirtualMachine", Value: id}
	var vm mo.VirtualMachine
	pc := property.DefaultCollector(client.Client)
	if err := pc.RetrieveOne(ctx, ref, []string{"config"}, &vm); err != nil {
		return nil, fmt.Errorf("get template: %w", err)
	}
	if vm.Config == nil {
		return nil, fmt.Errorf("template config not found")
	}
	t := &provider.Template{
		ID:       id,
		Name:     vm.Config.Name,
		Moref:    id,
		GuestID:  vm.Config.GuestId,
		OSType:   guessOSType(vm.Config.GuestId),
		CPU:      int(vm.Config.Hardware.NumCPU),
		MemoryMB: int(vm.Config.Hardware.MemoryMB),
		DiskGB:   totalDiskGB(vm.Config.Hardware.Device),
	}
	return t, nil
}

func (p *Provider) GetTemplateDetail(ctx context.Context, id string) (*provider.TemplateDetail, error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return nil, err
	}

	ref := types.ManagedObjectReference{Type: "VirtualMachine", Value: id}
	var vm mo.VirtualMachine
	pc := property.DefaultCollector(client.Client)
	if err := pc.RetrieveOne(ctx, ref, []string{"config", "summary", "guest", "datastore", "parent", "network"}, &vm); err != nil {
		return nil, fmt.Errorf("get template detail: %w", err)
	}
	if vm.Config == nil {
		return nil, fmt.Errorf("template config not found")
	}

	detail := &provider.TemplateDetail{
		Template: provider.Template{
			ID:       id,
			Name:     vm.Config.Name,
			Moref:    id,
			GuestID:  vm.Config.GuestId,
			OSType:   guessOSType(vm.Config.GuestId),
			CPU:      int(vm.Config.Hardware.NumCPU),
			MemoryMB: int(vm.Config.Hardware.MemoryMB),
			DiskGB:   totalDiskGB(vm.Config.Hardware.Device),
		},
		Annotation:  vm.Config.Annotation,
		HardwareVer: vm.Config.Version,
		Firmware:    vm.Config.Firmware,
		Platform:    "vmware",
	}

	if vm.Config.CreateDate != nil {
		detail.CreatedAt = vm.Config.CreateDate.Format("2006-01-02T15:04:05Z")
	}

	// Tools status from guest or summary
	if vm.Guest != nil && vm.Guest.ToolsStatus != "" {
		detail.ToolsStatus = string(vm.Guest.ToolsStatus)
	} else if vm.Summary.Guest != nil && vm.Summary.Guest.ToolsStatus != "" {
		detail.ToolsStatus = string(vm.Summary.Guest.ToolsStatus)
	}

	// Resolve datastore names
	if len(vm.Datastore) > 0 {
		var dsNames []string
		for _, dsRef := range vm.Datastore {
			var ds mo.Datastore
			if err := pc.RetrieveOne(ctx, dsRef, []string{"name"}, &ds); err == nil {
				dsNames = append(dsNames, ds.Name)
			}
		}
		if len(dsNames) > 0 {
			detail.Datastore = dsNames[0]
		}
	}

	// Resolve folder path
	if vm.Parent != nil {
		var folder mo.Folder
		if err := pc.RetrieveOne(ctx, *vm.Parent, []string{"name"}, &folder); err == nil {
			detail.Folder = folder.Name
		}
	}

	// Extract network names from virtual ethernet cards
	networks := []string{}
	for _, dev := range vm.Config.Hardware.Device {
		if eth, ok := dev.(types.BaseVirtualEthernetCard); ok {
			backing := eth.GetVirtualEthernetCard().Backing
			switch b := backing.(type) {
			case *types.VirtualEthernetCardNetworkBackingInfo:
				if b.DeviceName != "" {
					networks = append(networks, b.DeviceName)
				}
			case *types.VirtualEthernetCardDistributedVirtualPortBackingInfo:
				// Try to resolve the port group name
				if b.Port.PortgroupKey != "" {
					pgRef := types.ManagedObjectReference{Type: "DistributedVirtualPortgroup", Value: b.Port.PortgroupKey}
					var pg mo.DistributedVirtualPortgroup
					if err := pc.RetrieveOne(ctx, pgRef, []string{"name"}, &pg); err == nil {
						networks = append(networks, pg.Name)
					} else {
						networks = append(networks, b.Port.PortgroupKey)
					}
				}
			}
		}
	}
	detail.Networks = networks

	return detail, nil
}

func guessOSType(guestID string) string {
	switch {
	case len(guestID) > 3 && guestID[:3] == "win":
		return "windows"
	default:
		return "linux"
	}
}

func totalDiskGB(devices object.VirtualDeviceList) int {
	var total int64
	for _, d := range devices {
		if disk, ok := d.(*types.VirtualDisk); ok {
			total += disk.CapacityInKB
		}
	}
	return int(total / 1024 / 1024)
}
