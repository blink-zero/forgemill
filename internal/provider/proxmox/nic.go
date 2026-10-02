package proxmox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/forgemill/forgemill/internal/provider"
)

// nicAdapterTypes are the NIC models AddNIC will create, in the names the
// qemu config API takes for netN. First entry is the default: virtio is
// the paravirtual model Proxmox itself defaults to and what every
// cloud-init image ships drivers for. Published through
// ProviderMetadata.NICAdapterTypes so the service and UI use the same list.
var nicAdapterTypes = []string{"virtio", "e1000", "e1000e", "vmxnet3", "rtl8139"}

// maxNetSlots is the number of netN devices qemu-server allows (net0..net31).
const maxNetSlots = 32

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

// nextFreeNetSlot returns the lowest netN index not present in the VM
// config. Proxmox doesn't allocate slots itself — the caller names the
// key — so picking the lowest free one mirrors what the Proxmox UI does.
func nextFreeNetSlot(config map[string]interface{}) (int, error) {
	for i := 0; i < maxNetSlots; i++ {
		if _, taken := config[fmt.Sprintf("net%d", i)]; !taken {
			return i, nil
		}
	}
	return 0, fmt.Errorf("no free network device slot (net0-net%d all in use)", maxNetSlots-1)
}

// buildNetConfig builds a netN value: "<model>,bridge=<bridge>[,tag=<vlan>][,link_down=1]".
// vlanTag <= 0 means untagged. linkDown=true creates the adapter
// disconnected (the Proxmox equivalent of vSphere's connected=false).
func buildNetConfig(model, bridge string, vlanTag int, linkDown bool) string {
	s := fmt.Sprintf("%s,bridge=%s", model, bridge)
	if vlanTag > 0 {
		s += fmt.Sprintf(",tag=%d", vlanTag)
	}
	if linkDown {
		s += ",link_down=1"
	}
	return s
}

// netConfig is a parsed netN value as Proxmox reports it back, e.g.
// "virtio=BC:24:11:AB:CD:EF,bridge=vmbr0,tag=20,firewall=1,link_down=1".
type netConfig struct {
	Model    string
	MAC      string
	Bridge   string
	VLANTag  int
	LinkDown bool
}

func parseNetConfig(val string) netConfig {
	var nc netConfig
	for i, part := range strings.Split(val, ",") {
		k, v, hasVal := strings.Cut(part, "=")
		switch {
		case i == 0:
			// First segment is "<model>=<mac>" once Proxmox has assigned a
			// MAC, or a bare "<model>" on a value we built ourselves.
			nc.Model = k
			if hasVal {
				nc.MAC = strings.ToUpper(v)
			}
		case k == "bridge":
			nc.Bridge = v
		case k == "tag":
			nc.VLANTag, _ = strconv.Atoi(v)
		case k == "link_down":
			nc.LinkDown = v == "1"
		case k == "macaddr":
			nc.MAC = strings.ToUpper(v)
		}
	}
	return nc
}

// getVMConfig fetches /config as a flat map (netN, scsiN, hotplug, ...).
func (p *Provider) getVMConfig(ctx context.Context, node, vmID string) (map[string]interface{}, error) {
	body, err := p.doGet(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/config", url.PathEscape(node), url.PathEscape(vmID)))
	if err != nil {
		return nil, fmt.Errorf("get VM config: %w", err)
	}
	var result struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse VM config: %w", err)
	}
	if result.Data == nil {
		return nil, fmt.Errorf("VM %s not found on node %s", vmID, node)
	}
	return result.Data, nil
}

// bridgeExists reports whether the node lists an interface of type bridge
// with that name. A failed read returns an error so the caller can skip
// the check rather than block a valid request on a transient fault.
func (p *Provider) bridgeExists(ctx context.Context, node, bridge string) (bool, error) {
	body, err := p.doGet(ctx, fmt.Sprintf("/nodes/%s/network", url.PathEscape(node)))
	if err != nil {
		return false, err
	}
	var result struct {
		Data []struct {
			Iface string `json:"iface"`
			Type  string `json:"type"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return false, err
	}
	for _, n := range result.Data {
		if n.Type == "bridge" && n.Iface == bridge {
			return true, nil
		}
	}
	return false, nil
}

// netChangePending reports whether the netN key is sitting in the VM's
// pending-changes list — which is what happens on a running VM whose
// "hotplug" setting doesn't include "network": the config is saved but
// only takes effect at the next power cycle. A read failure is reported
// as not-pending with a log line; the attach itself already succeeded.
func (p *Provider) netChangePending(ctx context.Context, node, vmID, key string) bool {
	body, err := p.doGet(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/pending", url.PathEscape(node), url.PathEscape(vmID)))
	if err != nil {
		slog.Warn("could not read pending changes after NIC add", "vmID", vmID, "error", err)
		return false
	}
	var result struct {
		Data []struct {
			Key     string      `json:"key"`
			Value   interface{} `json:"value"`
			Pending interface{} `json:"pending"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return false
	}
	for _, e := range result.Data {
		if e.Key == key {
			// A pending entry carries the new value under "pending" and no
			// (or the old) "value"; an applied one has only "value".
			return e.Pending != nil && e.Value == nil
		}
	}
	return false
}

// AddNIC adds a netN device to the VM via the qemu config API. Proxmox
// hot-plugs it when the VM is running and its hotplug setting includes
// "network" (the default); otherwise the change is saved as pending and
// applied at the next power cycle — reported via NIC.Pending, never forced
// with a reboot. Existing devices are untouched: the PUT carries only the
// new key.
func (p *Provider) AddNIC(ctx context.Context, vmID string, spec provider.NICSpec) (*provider.NIC, error) {
	bridge := strings.TrimSpace(spec.Network)
	if bridge == "" {
		return nil, fmt.Errorf("network (bridge) is required")
	}
	model, err := normalizeNICAdapterType(spec.AdapterType)
	if err != nil {
		return nil, err
	}
	if spec.VLANTag < 0 || spec.VLANTag > 4094 {
		return nil, fmt.Errorf("VLAN tag must be between 1 and 4094")
	}

	node, err := p.resolveVMNode(ctx, vmID)
	if err != nil {
		node = p.node
	}

	config, err := p.getVMConfig(ctx, node, vmID)
	if err != nil {
		return nil, err
	}
	slot, err := nextFreeNetSlot(config)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("net%d", slot)

	// Check the bridge exists on the VM's node before writing. Proxmox would
	// reject an unknown bridge itself, but doPut folds the response body
	// into a generic "HTTP 400" — checking the node's interface list first
	// (the same list GetResources offers) is what lets the API answer with
	// a specific "network not found" instead of a bare failure.
	if known, err := p.bridgeExists(ctx, node, bridge); err == nil && !known {
		return nil, fmt.Errorf("%w: bridge %q on node %s", provider.ErrNetworkNotFound, bridge, node)
	}

	data := url.Values{key: {buildNetConfig(model, bridge, spec.VLANTag, !spec.Connected)}}
	configPath := fmt.Sprintf("/nodes/%s/qemu/%s/config", url.PathEscape(node), url.PathEscape(vmID))
	if err := p.doPut(ctx, configPath, data); err != nil {
		return nil, fmt.Errorf("add network device %s: %w", key, err)
	}

	pending := p.netChangePending(ctx, node, vmID, key)
	nic := &provider.NIC{
		Key:            slot,
		Label:          key,
		AdapterType:    model,
		Network:        bridge,
		VLANTag:        spec.VLANTag,
		Connected:      spec.Connected && !pending && p.isRunning(ctx, node, vmID),
		StartConnected: spec.Connected,
		Pending:        pending,
	}
	// Re-read to pick up the MAC Proxmox generated. Best effort — the
	// device is attached at this point regardless.
	if after, err := p.getVMConfig(ctx, node, vmID); err == nil {
		if raw, ok := after[key].(string); ok {
			parsed := parseNetConfig(raw)
			nic.MACAddress = parsed.MAC
			if parsed.Bridge != "" {
				nic.Network = parsed.Bridge
			}
		}
	} else {
		slog.Warn("network device added but post-add config read failed", "vmID", vmID, "error", err)
	}
	return nic, nil
}

// guestInterface is one entry from the QEMU guest agent's
// network-get-interfaces, reduced to what ListNICs joins on.
type guestInterface struct {
	Name      string
	MAC       string
	Addresses []string
}

// getGuestAgentInterfaces returns the guest's interfaces with MAC and
// addresses, or nil when the agent isn't running/installed — that's the
// normal case for a stopped VM or a guest without qemu-guest-agent, not
// an error worth surfacing.
func (p *Provider) getGuestAgentInterfaces(ctx context.Context, node, vmID string) []guestInterface {
	body, err := p.doGet(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/agent/network-get-interfaces", url.PathEscape(node), url.PathEscape(vmID)))
	if err != nil {
		return nil
	}
	var result struct {
		Data struct {
			Result []struct {
				Name        string `json:"name"`
				HardwareMAC string `json:"hardware-address"`
				IPAddresses []struct {
					IPAddress string `json:"ip-address"`
				} `json:"ip-addresses"`
			} `json:"result"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &result) != nil {
		return nil
	}
	var out []guestInterface
	for _, iface := range result.Data.Result {
		if iface.Name == "lo" || iface.Name == "lo0" {
			continue
		}
		gi := guestInterface{Name: iface.Name, MAC: strings.ToUpper(iface.HardwareMAC)}
		for _, a := range iface.IPAddresses {
			if a.IPAddress != "" && a.IPAddress != "127.0.0.1" && a.IPAddress != "::1" {
				gi.Addresses = append(gi.Addresses, a.IPAddress)
			}
		}
		out = append(out, gi)
	}
	return out
}

// isRunning reports whether the VM is currently running. Unknown (status
// read failed) is treated as not running so Connected is never over-claimed.
func (p *Provider) isRunning(ctx context.Context, node, vmID string) bool {
	body, err := p.doGet(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/status/current", url.PathEscape(node), url.PathEscape(vmID)))
	if err != nil {
		return false
	}
	var result struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &result) != nil {
		return false
	}
	return result.Data.Status == "running"
}

// ListNICs reads net0..netN from the VM config and joins the guest agent's
// interface list by MAC for addresses. Connected is the live link state
// (VM running and link not down); StartConnected is the configured intent
// (link not down), so a stopped VM's adapters read as "connects at
// power-on" rather than "disconnected".
func (p *Provider) ListNICs(ctx context.Context, vmID string) ([]provider.NIC, error) {
	node, err := p.resolveVMNode(ctx, vmID)
	if err != nil {
		node = p.node
	}
	config, err := p.getVMConfig(ctx, node, vmID)
	if err != nil {
		return nil, err
	}
	running := p.isRunning(ctx, node, vmID)
	guest := p.getGuestAgentInterfaces(ctx, node, vmID)

	nics := []provider.NIC{}
	for i := 0; i < maxNetSlots; i++ {
		key := fmt.Sprintf("net%d", i)
		raw, ok := config[key].(string)
		if !ok {
			continue
		}
		nc := parseNetConfig(raw)
		nic := provider.NIC{
			Key:            i,
			Label:          key,
			AdapterType:    nc.Model,
			Network:        nc.Bridge,
			MACAddress:     nc.MAC,
			VLANTag:        nc.VLANTag,
			Connected:      running && !nc.LinkDown,
			StartConnected: !nc.LinkDown,
			Addresses:      []string{},
		}
		for _, g := range guest {
			if g.MAC != "" && g.MAC == nc.MAC {
				nic.Addresses = provider.SortAddresses(g.Addresses)
				break
			}
		}
		nics = append(nics, nic)
	}
	return nics, nil
}
