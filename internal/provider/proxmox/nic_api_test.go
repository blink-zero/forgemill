package proxmox

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/forgemill/forgemill/internal/provider"
)

// fakePVE is a minimal Proxmox API double covering exactly the calls AddNIC
// makes: node discovery, VM→node resolution, node bridges, VM config
// read/write, and the pending-changes list.
type fakePVE struct {
	mu       sync.Mutex
	config   map[string]string // current VM 100 config
	pending  map[string]string // keys that would show as pending after a write
	bridges  []string
	puts     []url.Values
	agent    []map[string]interface{} // guest agent interfaces; nil = empty result
	agentErr bool                     // simulate "No QEMU guest agent configured"
	stopped  bool                     // status/current reports "stopped" instead of "running"
	server   *httptest.Server
}

func newFakePVE(t *testing.T) *fakePVE {
	t.Helper()
	f := &fakePVE{
		config:  map[string]string{"net0": "virtio=BC:24:11:00:00:01,bridge=vmbr0", "scsi0": "local-zfs:vm-100-disk-0,size=40G", "hotplug": "network,disk,usb"},
		pending: map[string]string{},
		bridges: []string{"vmbr0", "vmbr1"},
	}
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, v interface{}) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": v})
	}
	mux.HandleFunc("/api2/json/nodes", func(w http.ResponseWriter, r *http.Request) {
		write(w, []map[string]string{{"node": "pve", "status": "online"}})
	})
	mux.HandleFunc("/api2/json/cluster/resources", func(w http.ResponseWriter, r *http.Request) {
		write(w, []map[string]interface{}{{"vmid": 100, "node": "pve"}})
	})
	mux.HandleFunc("/api2/json/nodes/pve/network", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var out []map[string]string
		for _, b := range f.bridges {
			out = append(out, map[string]string{"iface": b, "type": "bridge"})
		}
		out = append(out, map[string]string{"iface": "eno1", "type": "eth"})
		write(w, out)
	})
	mux.HandleFunc("/api2/json/nodes/pve/qemu/100/config", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			write(w, f.config)
		case http.MethodPut:
			if err := r.ParseForm(); err != nil {
				w.WriteHeader(400)
				return
			}
			f.puts = append(f.puts, r.PostForm)
			for k, vals := range r.PostForm {
				// Proxmox assigns a MAC on write: "virtio,bridge=x" -> "virtio=MAC,bridge=x"
				model, rest, _ := strings.Cut(vals[0], ",")
				f.config[k] = model + "=BC:24:11:AA:BB:" + strconv.Itoa(len(f.puts)+10) + "," + rest
			}
			write(w, nil)
		default:
			w.WriteHeader(405)
		}
	})
	mux.HandleFunc("/api2/json/nodes/pve/qemu/100/pending", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var out []map[string]interface{}
		for k, v := range f.config {
			if pv, ok := f.pending[k]; ok {
				out = append(out, map[string]interface{}{"key": k, "pending": pv})
				continue
			}
			out = append(out, map[string]interface{}{"key": k, "value": v})
		}
		write(w, out)
	})
	mux.HandleFunc("/api2/json/nodes/pve/qemu/100/agent/network-get-interfaces", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.agentErr {
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"data":null,"message":"No QEMU guest agent configured"}`))
			return
		}
		write(w, map[string]interface{}{"result": f.agent})
	})
	mux.HandleFunc("/api2/json/nodes/pve/qemu/100/status/current", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		st := "running"
		if f.stopped {
			st = "stopped"
		}
		write(w, map[string]interface{}{"status": st, "vmid": 100})
	})
	f.server = httptest.NewTLSServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakePVE) provider(t *testing.T) *Provider {
	t.Helper()
	u, _ := url.Parse(f.server.URL)
	port, _ := strconv.Atoi(u.Port())
	// API-token auth skips the ticket login; validateCerts=false accepts
	// httptest's self-signed cert.
	p := New(u.Hostname(), port, "forgemill@pve!ci", "forgemill@pve!ci=00000000-0000-0000-0000-000000000000", false)
	if err := p.Connect(context.Background()); err != nil {
		t.Fatalf("connect to fake PVE: %v", err)
	}
	return p
}

func TestAddNICHotPlugsIntoNextFreeSlot(t *testing.T) {
	f := newFakePVE(t)
	p := f.provider(t)

	nic, err := p.AddNIC(context.Background(), "100", provider.NICSpec{Network: "vmbr1", AdapterType: "", Connected: true, VLANTag: 20})
	if err != nil {
		t.Fatalf("AddNIC: %v", err)
	}
	if len(f.puts) != 1 {
		t.Fatalf("expected exactly one config PUT, got %d", len(f.puts))
	}
	// Only the new key is written — existing devices are never resent.
	if got := f.puts[0]; len(got) != 1 || got.Get("net1") != "virtio,bridge=vmbr1,tag=20" {
		t.Errorf("PUT body = %v, want only net1=virtio,bridge=vmbr1,tag=20", got)
	}
	if nic.Key != 1 || nic.Label != "net1" || nic.AdapterType != "virtio" || nic.Network != "vmbr1" || nic.VLANTag != 20 || !nic.Connected || nic.Pending {
		t.Errorf("unexpected NIC: %+v", nic)
	}
	if !strings.HasPrefix(nic.MACAddress, "BC:24:11:") {
		t.Errorf("expected the MAC Proxmox assigned to be read back, got %q", nic.MACAddress)
	}
}

func TestAddNICDisconnectedUsesLinkDownAndExplicitModel(t *testing.T) {
	f := newFakePVE(t)
	p := f.provider(t)
	nic, err := p.AddNIC(context.Background(), "100", provider.NICSpec{Network: "vmbr0", AdapterType: "E1000", Connected: false})
	if err != nil {
		t.Fatalf("AddNIC: %v", err)
	}
	if got := f.puts[0].Get("net1"); got != "e1000,bridge=vmbr0,link_down=1" {
		t.Errorf("PUT net1 = %q", got)
	}
	if nic.Connected || nic.AdapterType != "e1000" {
		t.Errorf("unexpected NIC: %+v", nic)
	}
}

func TestAddNICReportsPendingWhenNotHotPlugged(t *testing.T) {
	f := newFakePVE(t)
	f.pending["net1"] = "virtio,bridge=vmbr0" // Proxmox parked the change
	p := f.provider(t)
	nic, err := p.AddNIC(context.Background(), "100", provider.NICSpec{Network: "vmbr0", Connected: true})
	if err != nil {
		t.Fatalf("AddNIC: %v", err)
	}
	if !nic.Pending {
		t.Error("expected Pending=true when the key sits in the pending list")
	}
}

func TestAddNICUnknownBridgeIsNetworkNotFoundWithoutWriting(t *testing.T) {
	f := newFakePVE(t)
	p := f.provider(t)
	_, err := p.AddNIC(context.Background(), "100", provider.NICSpec{Network: "vmbr9", Connected: true})
	if !errors.Is(err, provider.ErrNetworkNotFound) {
		t.Fatalf("expected ErrNetworkNotFound, got %v", err)
	}
	if len(f.puts) != 0 {
		t.Error("config must not be written for an unknown bridge")
	}
}

func TestAddNICRejectsBadInputBeforeAnyCall(t *testing.T) {
	f := newFakePVE(t)
	p := f.provider(t)
	if _, err := p.AddNIC(context.Background(), "100", provider.NICSpec{Network: "vmbr0", AdapterType: "sriov"}); !errors.Is(err, provider.ErrInvalidAdapterType) {
		t.Errorf("bad adapter: got %v", err)
	}
	if _, err := p.AddNIC(context.Background(), "100", provider.NICSpec{Network: "vmbr0", VLANTag: 5000}); err == nil {
		t.Error("expected an error for VLAN 5000")
	}
	if _, err := p.AddNIC(context.Background(), "100", provider.NICSpec{Network: "  "}); err == nil {
		t.Error("expected an error for an empty bridge")
	}
	if len(f.puts) != 0 {
		t.Error("nothing should have been written")
	}
}

func TestListNICsJoinsConfigWithGuestAgentByMAC(t *testing.T) {
	f := newFakePVE(t)
	f.config["net1"] = "e1000=BC:24:11:00:00:02,bridge=vmbr1,tag=20,link_down=1"
	f.agent = []map[string]interface{}{
		{"name": "lo", "hardware-address": "00:00:00:00:00:00", "ip-addresses": []map[string]string{{"ip-address": "127.0.0.1"}}},
		{"name": "ens18", "hardware-address": "bc:24:11:00:00:01", "ip-addresses": []map[string]string{{"ip-address": "fe80::be24:11ff:fe00:1"}, {"ip-address": "10.20.20.15"}}},
	}
	p := f.provider(t)

	nics, err := p.ListNICs(context.Background(), "100")
	if err != nil {
		t.Fatalf("ListNICs: %v", err)
	}
	if len(nics) != 2 {
		t.Fatalf("expected 2 adapters, got %d: %+v", len(nics), nics)
	}
	n0, n1 := nics[0], nics[1]
	if n0.Label != "net0" || n0.AdapterType != "virtio" || n0.Network != "vmbr0" || n0.MACAddress != "BC:24:11:00:00:01" || !n0.Connected {
		t.Errorf("net0 = %+v", n0)
	}
	// Guest agent addresses are joined by MAC (case-insensitive) and
	// sorted IPv4-first; loopback is dropped.
	if len(n0.Addresses) != 2 || n0.Addresses[0] != "10.20.20.15" {
		t.Errorf("net0 addresses = %v", n0.Addresses)
	}
	if n1.Label != "net1" || n1.AdapterType != "e1000" || n1.Network != "vmbr1" || n1.VLANTag != 20 || n1.Connected {
		t.Errorf("net1 = %+v", n1)
	}
	// An adapter the agent doesn't report has an empty (non-nil) list.
	if n1.Addresses == nil || len(n1.Addresses) != 0 {
		t.Errorf("net1 addresses should be empty, got %v", n1.Addresses)
	}
}

func TestListNICsWithoutGuestAgentStillListsAdapters(t *testing.T) {
	f := newFakePVE(t)
	f.agentErr = true // agent not running: Proxmox answers 500 "No QEMU guest agent configured"
	p := f.provider(t)
	nics, err := p.ListNICs(context.Background(), "100")
	if err != nil {
		t.Fatalf("ListNICs: %v", err)
	}
	if len(nics) != 1 || nics[0].MACAddress != "BC:24:11:00:00:01" || len(nics[0].Addresses) != 0 {
		t.Errorf("unexpected: %+v", nics)
	}
}

func TestListNICsConnectedIsRuntimeStartConnectedIsIntent(t *testing.T) {
	// Running VM: a normal adapter is both connected and start-connected;
	// link_down=1 is neither.
	f := newFakePVE(t)
	f.config["net1"] = "virtio=BC:24:11:00:00:02,bridge=vmbr0,link_down=1"
	p := f.provider(t)
	nics, err := p.ListNICs(context.Background(), "100")
	if err != nil {
		t.Fatalf("ListNICs: %v", err)
	}
	if !nics[0].Connected || !nics[0].StartConnected {
		t.Errorf("running + up: got connected=%v start=%v", nics[0].Connected, nics[0].StartConnected)
	}
	if nics[1].Connected || nics[1].StartConnected {
		t.Errorf("link_down: got connected=%v start=%v", nics[1].Connected, nics[1].StartConnected)
	}

	// Stopped VM: nothing is live, but the intent survives — this is what
	// lets the UI say "connects at power-on" instead of "disconnected".
	f2 := newFakePVE(t)
	f2.stopped = true
	p2 := f2.provider(t)
	nics, err = p2.ListNICs(context.Background(), "100")
	if err != nil {
		t.Fatalf("ListNICs (stopped): %v", err)
	}
	if nics[0].Connected || !nics[0].StartConnected {
		t.Errorf("stopped: got connected=%v start=%v, want false/true", nics[0].Connected, nics[0].StartConnected)
	}
}

func TestAddNICOnStoppedVMReportsStartConnectedOnly(t *testing.T) {
	f := newFakePVE(t)
	f.stopped = true
	p := f.provider(t)
	nic, err := p.AddNIC(context.Background(), "100", provider.NICSpec{Network: "vmbr0", Connected: true})
	if err != nil {
		t.Fatalf("AddNIC: %v", err)
	}
	if nic.Connected || !nic.StartConnected {
		t.Errorf("got connected=%v start=%v, want false/true on a stopped VM", nic.Connected, nic.StartConnected)
	}
}
