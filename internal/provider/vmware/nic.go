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
	"github.com/vmware/govmomi/vim25/types"

	"github.com/forgemill/forgemill/internal/provider"
)

// nicAdapterTypes are the adapter models AddNIC will create, in the names
// govmomi's CreateEthernetCard expects. The first entry is the default:
// vmxnet3 is the paravirtual adapter VMware recommends for every modern
// guest and is hot-add safe on all supported vSphere versions. Deliberately
// a short list — the legacy/exotic models (pcnet32, vmxnet2, sriov, vrdma)
// need guest drivers or host features the typical Forgemill deploy lacks.
// Also published through ProviderMetadata.NICAdapterTypes so the service
// and UI work from the same list.
var nicAdapterTypes = []string{"vmxnet3", "e1000e", "e1000"}

// normalizeNICAdapterType lowercases/trims the requested adapter model,
// applies the default for an empty value, and rejects anything outside
// nicAdapterTypes with provider.ErrInvalidAdapterType.
func normalizeNICAdapterType(adapterType string) (string, error) {
	t := strings.ToLower(strings.TrimSpace(adapterType))
	if t == "" {
		return nicAdapterTypes[0], nil
	}
	if !slices.Contains(nicAdapterTypes, t) {
		return "", fmt.Errorf("%w: %q (use %s)", provider.ErrInvalidAdapterType, adapterType, strings.Join(nicAdapterTypes, ", "))
	}
	return t, nil
}

// datacenterPathOf returns the datacenter segment of a VM inventory path
// ("/DC1/vm/web/web-01" -> "/DC1"). The network finder needs a datacenter
// context, and the VM's own datacenter is the only one a NIC can attach to.
func datacenterPathOf(inventoryPath string) (string, error) {
	trimmed := strings.TrimPrefix(inventoryPath, "/")
	dc, _, _ := strings.Cut(trimmed, "/")
	if dc == "" {
		return "", fmt.Errorf("inventory path %q has no datacenter segment", inventoryPath)
	}
	return "/" + dc, nil
}

// AddNIC hot-adds a virtual network adapter to the VM. The reconfigure is
// an Add device change — no power cycle, and no existing device is touched.
// When the VM is running the adapter is connected immediately (if asked);
// when it's off, StartConnected applies at the next power-on.
func (p *Provider) AddNIC(ctx context.Context, vmID string, spec provider.NICSpec) (*provider.NIC, error) {
	if strings.TrimSpace(spec.Network) == "" {
		return nil, fmt.Errorf("network is required")
	}
	adapter, err := normalizeNICAdapterType(spec.AdapterType)
	if err != nil {
		return nil, err
	}

	client, err := p.getClient(ctx)
	if err != nil {
		return nil, err
	}

	ref := types.ManagedObjectReference{Type: "VirtualMachine", Value: vmID}
	vm := object.NewVirtualMachine(client.Client, ref)
	finder := find.NewFinder(client.Client, true)

	// Scope the finder to the VM's datacenter, resolved from the VM itself
	// rather than a caller-supplied name: that's the only datacenter whose
	// networks are valid backings for this VM, and it also means a bare
	// network name resolves the same way DeployVM resolved it at deploy time.
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

	net, err := finder.Network(ctx, spec.Network)
	if err != nil {
		var notFound *find.NotFoundError
		if errors.As(err, &notFound) {
			return nil, fmt.Errorf("%w: %q", provider.ErrNetworkNotFound, spec.Network)
		}
		return nil, fmt.Errorf("find network %q: %w", spec.Network, err)
	}
	backing, err := net.EthernetCardBackingInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("get network backing for %q: %w", spec.Network, err)
	}

	// Snapshot the existing adapter keys so the new one can be picked out of
	// the post-reconfigure device list — vSphere assigns the real key
	// server-side, so the key on the device we submit is only a placeholder.
	before, err := vm.Device(ctx)
	if err != nil {
		return nil, fmt.Errorf("get VM devices: %w", err)
	}
	existing := map[int32]bool{}
	for _, dev := range before.SelectByType((*types.VirtualEthernetCard)(nil)) {
		existing[dev.GetVirtualDevice().Key] = true
	}

	card, err := object.VirtualDeviceList{}.CreateEthernetCard(adapter, backing)
	if err != nil {
		return nil, fmt.Errorf("create %s adapter: %w", adapter, err)
	}
	card.(types.BaseVirtualEthernetCard).GetVirtualEthernetCard().Connectable = &types.VirtualDeviceConnectInfo{
		StartConnected:    spec.Connected,
		Connected:         spec.Connected,
		AllowGuestControl: true,
	}

	if err := vm.AddDevice(ctx, card); err != nil {
		return nil, fmt.Errorf("add network adapter: %w", err)
	}

	after, err := vm.Device(ctx)
	if err != nil {
		// The adapter is attached at this point; failing the call would
		// mislead the caller into retrying and attaching a second one.
		slog.Warn("network adapter added but post-add device read failed", "vmID", vmID, "error", err)
		return &provider.NIC{AdapterType: adapter, Network: spec.Network, Connected: spec.Connected}, nil
	}
	for _, dev := range after.SelectByType((*types.VirtualEthernetCard)(nil)) {
		if existing[dev.GetVirtualDevice().Key] {
			continue
		}
		return nicFromDevice(after, dev, spec.Network), nil
	}
	slog.Warn("network adapter added but not found in post-add device list", "vmID", vmID)
	return &provider.NIC{AdapterType: adapter, Network: spec.Network, Connected: spec.Connected}, nil
}

// nicFromDevice shapes a virtual ethernet card into the provider-neutral
// NIC. network is the name the caller asked for; resolving the backing back
// to a display name would cost another round-trip for no new information.
func nicFromDevice(devices object.VirtualDeviceList, dev types.BaseVirtualDevice, network string) *provider.NIC {
	eth := dev.(types.BaseVirtualEthernetCard).GetVirtualEthernetCard()
	nic := &provider.NIC{
		Key:         int(eth.Key),
		Label:       devices.Name(dev),
		AdapterType: devices.Type(dev),
		Network:     network,
		MACAddress:  eth.MacAddress,
	}
	if eth.DeviceInfo != nil {
		if label := eth.DeviceInfo.GetDescription().Label; label != "" {
			nic.Label = label
		}
	}
	if eth.Connectable != nil {
		nic.Connected = eth.Connectable.Connected
	}
	return nic
}
