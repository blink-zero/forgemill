package vmware

import (
	"context"
	"fmt"
	"strings"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	"github.com/forgemill/forgemill/internal/provider"
)

// ListNICs reads config.hardware.device for the adapters and guest.net for
// what the guest reports on each (VMware Tools), joined by MAC — with a
// fallback on deviceConfigId for the odd guest that reports a MAC in a
// different case or not at all.
func (p *Provider) ListNICs(ctx context.Context, vmID string) ([]provider.NIC, error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return nil, err
	}
	ref := types.ManagedObjectReference{Type: "VirtualMachine", Value: vmID}
	var vm mo.VirtualMachine
	pc := property.DefaultCollector(client.Client)
	if err := pc.RetrieveOne(ctx, ref, []string{"config.hardware.device", "guest.net"}, &vm); err != nil {
		return nil, fmt.Errorf("get VM network devices: %w", err)
	}
	if vm.Config == nil {
		return nil, fmt.Errorf("VM configuration not available")
	}

	var guestNets []types.GuestNicInfo
	if vm.Guest != nil {
		guestNets = vm.Guest.Net
	}

	devices := object.VirtualDeviceList(vm.Config.Hardware.Device)
	nics := []provider.NIC{}
	// Portgroup names are resolved once per key — a VM with several NICs on
	// the same DVPG shouldn't cost several round-trips.
	pgNames := map[string]string{}
	for _, dev := range devices.SelectByType((*types.VirtualEthernetCard)(nil)) {
		eth := dev.(types.BaseVirtualEthernetCard).GetVirtualEthernetCard()
		nic := provider.NIC{
			Key:         int(eth.Key),
			Label:       devices.Name(dev),
			AdapterType: devices.Type(dev),
			MACAddress:  strings.ToUpper(eth.MacAddress),
			Network:     p.backingNetworkName(ctx, pc, eth.Backing, pgNames),
			Addresses:   provider.SortAddresses(guestAddressesFor(eth.MacAddress, eth.Key, guestNets)),
		}
		if eth.DeviceInfo != nil {
			if label := eth.DeviceInfo.GetDescription().Label; label != "" {
				nic.Label = label
			}
		}
		if eth.Connectable != nil {
			nic.Connected = eth.Connectable.Connected
		}
		nics = append(nics, nic)
	}
	return nics, nil
}

// backingNetworkName turns an adapter's backing into the name a user
// recognises: the portgroup name for standard and distributed switches
// (the latter resolved from its key), or the opaque network id for NSX.
func (p *Provider) backingNetworkName(ctx context.Context, pc *property.Collector, backing types.BaseVirtualDeviceBackingInfo, cache map[string]string) string {
	switch b := backing.(type) {
	case *types.VirtualEthernetCardNetworkBackingInfo:
		return b.DeviceName
	case *types.VirtualEthernetCardDistributedVirtualPortBackingInfo:
		key := b.Port.PortgroupKey
		if key == "" {
			return ""
		}
		if name, ok := cache[key]; ok {
			return name
		}
		name := key
		var pg mo.DistributedVirtualPortgroup
		if err := pc.RetrieveOne(ctx, types.ManagedObjectReference{Type: "DistributedVirtualPortgroup", Value: key}, []string{"name"}, &pg); err == nil && pg.Name != "" {
			name = pg.Name
		}
		cache[key] = name
		return name
	case *types.VirtualEthernetCardOpaqueNetworkBackingInfo:
		return b.OpaqueNetworkId
	default:
		return ""
	}
}

// guestAddressesFor returns the addresses guest tools report for the
// adapter with this MAC (case-insensitive); if no entry matches by MAC,
// falls back to the entry whose deviceConfigId is the adapter's key.
func guestAddressesFor(mac string, key int32, guestNets []types.GuestNicInfo) []string {
	var byKey []string
	for _, g := range guestNets {
		if mac != "" && strings.EqualFold(g.MacAddress, mac) {
			return g.IpAddress
		}
		if g.DeviceConfigId == key && key != 0 {
			byKey = g.IpAddress
		}
	}
	return byKey
}
