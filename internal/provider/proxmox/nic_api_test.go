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
	hits     map[string]int           // request count per path (method-agnostic)
	server   *httptest.Server

	// Deploy flow (template 9000 → clone 101), see deploy_api_test.go.
	rejectKeys   []string     // a config PUT on 101 carrying any of these keys gets a 400 with a Proxmox-style error body
	lockFailures int          // the first N config PUTs on 101 fail with "VM is locked (clone)" (HTTP 500)
	newPuts      []url.Values // accepted config PUTs on 101, in order
	deleted      bool         // DELETE /qemu/101 was called (rollback)
	started      bool         // POST /qemu/101/status/start was called
	resizeFails  bool         // PUT /qemu/101/resize answers 500 "storage full"
	vlanAware    []string     // bridges reported with bridge_vlan_aware=1
	hotplugFails bool         // a config PUT on 100 adding a netN answers 400 "hotplug problem"
	deletes      []string     // keys removed via PUT config delete=<key> on 100
}

func newFakePVE(t *testing.T) *fakePVE {
	t.Helper()
	f := &fakePVE{
		config:  map[string]string{"net0": "virtio=BC:24:11:00:00:01,bridge=vmbr0", "scsi0": "local-zfs:vm-100-disk-0,size=40G", "hotplug": "network,disk,usb"},
		pending: map[string]string{},
		bridges: []string{"vmbr0", "vmbr1"},
		// vmbr0 is a flat bridge, vmbr1 is VLAN aware — the usual lab shape.
		vlanAware: []string{"vmbr1"},
	}
	f.hits = map[string]int{}
	mux := http.NewServeMux()
	counted := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			f.hits[r.URL.Path]++
			f.mu.Unlock()
			h.ServeHTTP(w, r)
		})
	}
	write := func(w http.ResponseWriter, v interface{}) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": v})
	}
	mux.HandleFunc("/api2/json/storage", func(w http.ResponseWriter, r *http.Request) {
		write(w, []map[string]interface{}{{"storage": "local-zfs", "type": "zfspool"}, {"storage": "local", "type": "dir"}})
	})
	mux.HandleFunc("/api2/json/nodes", func(w http.ResponseWriter, r *http.Request) {
		write(w, []map[string]string{{"node": "pve", "status": "online"}})
	})
	mux.HandleFunc("/api2/json/cluster/resources", func(w http.ResponseWriter, r *http.Request) {
		write(w, []map[string]interface{}{
			{"vmid": 100, "node": "pve", "name": "web-01", "status": "running", "maxcpu": 2, "maxmem": 4294967296, "maxdisk": 42949672960, "template": 0},
			{"vmid": 9000, "node": "pve", "name": "tmpl", "status": "stopped", "template": 1},
		})
	})
	mux.HandleFunc("/api2/json/nodes/pve/network", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var out []map[string]string
		var outAny []map[string]interface{}
		for _, b := range f.bridges {
			e := map[string]interface{}{"iface": b, "type": "bridge"}
			for _, v := range f.vlanAware {
				if v == b {
					e["bridge_vlan_aware"] = 1
				}
			}
			outAny = append(outAny, e)
		}
		outAny = append(outAny, map[string]interface{}{"iface": "eno1", "type": "eth"})
		_ = out
		write(w, outAny)
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
			if del := r.PostForm.Get("delete"); del != "" {
				f.deletes = append(f.deletes, del)
				delete(f.config, del)
				write(w, nil)
				return
			}
			if f.hotplugFails {
				for k := range r.PostForm {
					if strings.HasPrefix(k, "net") {
						// Proxmox saves the key as pending and then reports the hot-plug failure.
						f.config[k] = r.PostForm.Get(k)
						w.WriteHeader(400)
						_, _ = w.Write([]byte(`{"data":null,"errors":{"` + k + `":"hotplug problem - VM 100 qmp command 'netdev_add' failed - network script /var/lib/qemu-server/pve-bridge failed with status 6400"},"message":"Parameter verification failed.\n"}`))
						return
					}
				}
			}
			f.puts = append(f.puts, r.PostForm)
			for k, vals := range r.PostForm {
				switch {
				case strings.HasPrefix(k, "net"):
					// Proxmox assigns a MAC on write: "virtio,bridge=x" -> "virtio=MAC,bridge=x"
					model, rest, _ := strings.Cut(vals[0], ",")
					f.config[k] = model + "=BC:24:11:AA:BB:" + strconv.Itoa(len(f.puts)+10) + "," + rest
				case strings.HasPrefix(k, "scsi") && !strings.Contains(vals[0], "vm-"):
					// "<storage>:<size>" allocates a volume: "local-zfs:10" -> "local-zfs:vm-100-disk-1,size=10G"
					storage, size, _ := strings.Cut(vals[0], ":")
					f.config[k] = storage + ":vm-100-disk-" + strings.TrimPrefix(k, "scsi") + ",size=" + size + "G"
				default:
					f.config[k] = vals[0]
				}
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
	// ---- deploy flow: clone template 9000 into VM 101
	upid := func(kind string) map[string]string {
		return map[string]string{"data": "UPID:pve:00001234:0000ABCD:650F0000:" + kind + ":101:root@pam:"}
	}
	writeRaw := func(w http.ResponseWriter, v interface{}) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("/api2/json/cluster/nextid", func(w http.ResponseWriter, r *http.Request) { write(w, "101") })
	mux.HandleFunc("/api2/json/nodes/pve/qemu/9000/clone", func(w http.ResponseWriter, r *http.Request) { writeRaw(w, upid("qmclone")) })
	mux.HandleFunc("/api2/json/nodes/pve/tasks/", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]string{"status": "stopped", "exitstatus": "OK"})
	})
	mux.HandleFunc("/api2/json/nodes/pve/qemu/101/config", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			write(w, map[string]string{"scsi0": "local-zfs:vm-101-disk-0,size=20G", "cores": "2", "memory": "2048"})
		case http.MethodPut:
			_ = r.ParseForm()
			if f.lockFailures > 0 {
				f.lockFailures--
				w.WriteHeader(500)
				_, _ = w.Write([]byte(`{"data":null,"message":"VM is locked (clone)"}`))
				return
			}
			for _, k := range f.rejectKeys {
				if r.PostForm.Has(k) {
					w.WriteHeader(400)
					_, _ = w.Write([]byte(`{"data":null,"errors":{"` + k + `":"invalid format - value does not look like a valid ssh key"},"message":"Parameter verification failed.\n"}`))
					return
				}
			}
			f.newPuts = append(f.newPuts, r.PostForm)
			write(w, nil)
		default:
			w.WriteHeader(405)
		}
	})
	mux.HandleFunc("/api2/json/nodes/pve/qemu/101/resize", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		fail := f.resizeFails
		f.mu.Unlock()
		if fail {
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"data":null,"message":"storage 'local-zfs' is full"}`))
			return
		}
		write(w, nil)
	})
	mux.HandleFunc("/api2/json/nodes/pve/qemu/101/status/current", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]interface{}{"status": "stopped", "vmid": 101})
	})
	mux.HandleFunc("/api2/json/nodes/pve/qemu/101/status/start", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.started = true
		f.mu.Unlock()
		writeRaw(w, upid("qmstart"))
	})
	mux.HandleFunc("/api2/json/nodes/pve/qemu/100/status/stop", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.stopped = true // the stop takes effect
		f.mu.Unlock()
		writeRaw(w, upid("qmstop"))
	})
	mux.HandleFunc("/api2/json/nodes/pve/qemu/100/status/shutdown", func(w http.ResponseWriter, r *http.Request) { writeRaw(w, upid("qmshutdown")) })
	mux.HandleFunc("/api2/json/nodes/pve/qemu/100", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			w.WriteHeader(405)
			return
		}
		f.mu.Lock()
		f.deleted = true
		f.mu.Unlock()
		writeRaw(w, upid("qmdestroy"))
	})
	mux.HandleFunc("/api2/json/nodes/pve/qemu/101/status/shutdown", func(w http.ResponseWriter, r *http.Request) { writeRaw(w, upid("qmshutdown")) })
	mux.HandleFunc("/api2/json/nodes/pve/qemu/101/status/stop", func(w http.ResponseWriter, r *http.Request) { writeRaw(w, upid("qmstop")) })
	mux.HandleFunc("/api2/json/nodes/pve/qemu/101", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			w.WriteHeader(405)
			return
		}
		f.mu.Lock()
		f.deleted = true
		f.mu.Unlock()
		writeRaw(w, upid("qmdestroy"))
	})

	f.server = httptest.NewTLSServer(counted(mux))
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

func (f *fakePVE) hit(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits["/api2/json"+path]
}

func TestNodeResolutionIsCachedPerConnection(t *testing.T) {
	f := newFakePVE(t)
	p := f.provider(t) // Connect: /nodes only
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := p.GetVMStatus(ctx, "100"); err != nil {
			t.Fatalf("GetVMStatus #%d: %v", i, err)
		}
	}
	if _, err := p.ListNICs(ctx, "100"); err != nil {
		t.Fatalf("ListNICs: %v", err)
	}
	if got := f.hit("/cluster/resources"); got != 1 {
		t.Errorf("cluster listing fetched %d times across 4 operations, want 1 (cached per connection)", got)
	}
	// A new connection starts cold.
	if err := p.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if err := p.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.GetVMStatus(ctx, "100"); err != nil {
		t.Fatal(err)
	}
	if got := f.hit("/cluster/resources"); got != 2 {
		t.Errorf("after reconnect the listing should be fetched again (got %d total)", got)
	}
}

func TestListVMsPrimesNodeCacheAndCarriesSyncFields(t *testing.T) {
	f := newFakePVE(t)
	p := f.provider(t)
	ctx := context.Background()
	vms, err := p.ListVMs(ctx)
	if err != nil {
		t.Fatalf("ListVMs: %v", err)
	}
	if len(vms) != 1 {
		t.Fatalf("templates must be excluded; got %d entries", len(vms))
	}
	vm := vms[0]
	if vm.ID != "100" || vm.PowerState != "poweredOn" || vm.CPU != 2 || vm.MemoryMB != 4096 || vm.DiskGB != 40 || vm.GuestID != "linux" {
		t.Errorf("listing fields: %+v", vm)
	}
	// The listing told us where every VM lives: no second cluster fetch.
	if _, err := p.GetVMStatus(ctx, "100"); err != nil {
		t.Fatal(err)
	}
	if got := f.hit("/cluster/resources"); got != 1 {
		t.Errorf("expected the ListVMs call to be the only cluster fetch, got %d", got)
	}
}

func TestConfigReadersShareOneFetchPerOperation(t *testing.T) {
	f := newFakePVE(t)
	p := f.provider(t)
	ctx := context.Background()
	if _, err := p.ListDisks(ctx, "100"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.GetTemplate(ctx, "100"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.GetTemplateDetail(ctx, "100"); err != nil {
		t.Fatal(err)
	}
	if got := f.hit("/nodes/pve/qemu/100/config"); got != 3 {
		t.Errorf("three config readers should fetch the config exactly three times, got %d", got)
	}
}
