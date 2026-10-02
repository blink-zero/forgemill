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
	mu      sync.Mutex
	config  map[string]string // current VM 100 config
	pending map[string]string // keys that would show as pending after a write
	bridges []string
	puts    []url.Values
	server  *httptest.Server
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
