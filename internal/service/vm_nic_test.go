package service

import (
	"context"
	"errors"
	"testing"

	"github.com/forgemill/forgemill/internal/crypto"
	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/provider"
	// Blank imports register the in-tree providers' metadata and factories
	// (init side effects), exactly as cmd/forgemill/main.go does.
	_ "github.com/forgemill/forgemill/internal/provider/proxmox"
	_ "github.com/forgemill/forgemill/internal/provider/vmware"
)

// nicTestKey satisfies crypto.NewEncryptor's 32-char minimum.
const nicTestKey = "0123456789abcdef0123456789abcdef"

// fakeNICProvider is a provider.Provider whose only real behaviour is AddNIC
// and GetVMStatus; everything else returns errTestList. It records what it
// was asked so tests can assert the service passed the request through
// unchanged. It stands in for the "esxi" target type for the duration of a
// test (the targets table has a CHECK constraint on type, so a made-up
// type can't be inserted) and the real factory is restored on cleanup.
type fakeNICProvider struct {
	addNICCalls []provider.NICSpec
	addNICErr   error
	statusCalls int
	nics        []provider.NIC
	listNICsErr error
	vms         []provider.VMInfo // ListVMs result; nil => errTestList (listing unavailable)
}

var fakeNIC *fakeNICProvider

// useFakeNICProvider swaps the registry's "esxi" factory for one returning
// fakeNIC and restores the real one when the test ends.
func useFakeNICProvider(t *testing.T) {
	t.Helper()
	orig := provider.GetProviderFactory("esxi")
	provider.RegisterProvider("esxi", func(string, int, string, string, bool) provider.Provider {
		return fakeNIC
	})
	t.Cleanup(func() { provider.RegisterProvider("esxi", orig) })
}

func (f *fakeNICProvider) Connect(context.Context) error { return nil }
func (f *fakeNICProvider) Disconnect() error             { return nil }
func (f *fakeNICProvider) TestConnection(context.Context) error {
	return nil
}
func (f *fakeNICProvider) ListTemplates(context.Context) ([]provider.Template, error) {
	return nil, errTestList
}
func (f *fakeNICProvider) GetTemplate(context.Context, string) (*provider.Template, error) {
	return nil, errTestList
}
func (f *fakeNICProvider) GetTemplateDetail(context.Context, string) (*provider.TemplateDetail, error) {
	return nil, errTestList
}
func (f *fakeNICProvider) DeployVM(context.Context, *provider.DeploySpec) (*provider.DeployResult, error) {
	return nil, errTestList
}
func (f *fakeNICProvider) GetDeployProgress(context.Context, string) (*provider.Progress, error) {
	return nil, errTestList
}
func (f *fakeNICProvider) PowerOn(context.Context, string) error  { return errTestList }
func (f *fakeNICProvider) PowerOff(context.Context, string) error { return errTestList }
func (f *fakeNICProvider) Restart(context.Context, string) error  { return errTestList }
func (f *fakeNICProvider) Suspend(context.Context, string) error  { return errTestList }
func (f *fakeNICProvider) DeleteVM(context.Context, string) error { return errTestList }
func (f *fakeNICProvider) GetVMStatus(context.Context, string) (*provider.VMStatus, error) {
	f.statusCalls++
	return &provider.VMStatus{PowerState: "poweredOn", IPAddress: "10.0.0.5", CPU: 2, MemoryMB: 2048, DiskGB: 40}, nil
}
func (f *fakeNICProvider) ListSnapshots(context.Context, string) ([]provider.Snapshot, error) {
	return nil, errTestList
}
func (f *fakeNICProvider) CreateSnapshot(context.Context, string, string, string, bool) error {
	return errTestList
}
func (f *fakeNICProvider) RevertSnapshot(context.Context, string, string) error { return errTestList }
func (f *fakeNICProvider) DeleteSnapshot(context.Context, string, string) error { return errTestList }
func (f *fakeNICProvider) ResizeVM(context.Context, string, int, int) error     { return errTestList }
func (f *fakeNICProvider) ListDisks(context.Context, string) ([]provider.Disk, error) {
	return nil, errTestList
}
func (f *fakeNICProvider) ExpandDisk(context.Context, string, int, int) error { return errTestList }
func (f *fakeNICProvider) AddNIC(_ context.Context, _ string, spec provider.NICSpec) (*provider.NIC, error) {
	f.addNICCalls = append(f.addNICCalls, spec)
	if f.addNICErr != nil {
		return nil, f.addNICErr
	}
	return &provider.NIC{Key: 4001, Label: "Network adapter 2", AdapterType: "vmxnet3", Network: spec.Network, MACAddress: "00:50:56:aa:bb:cc", Connected: spec.Connected}, nil
}
func (f *fakeNICProvider) ListNICs(context.Context, string) ([]provider.NIC, error) {
	if f.listNICsErr != nil {
		return nil, f.listNICsErr
	}
	return f.nics, nil
}
func (f *fakeNICProvider) GetConsoleURL(context.Context, string) (string, error) {
	return "", errTestList
}
func (f *fakeNICProvider) ListVMs(context.Context) ([]provider.VMInfo, error) {
	if f.vms == nil {
		return nil, errTestList
	}
	return f.vms, nil
}
func (f *fakeNICProvider) GetResources(context.Context) (*provider.Resources, error) {
	return nil, errTestList
}
func (f *fakeNICProvider) ValidateDeploySpec(context.Context, *provider.DeploySpec) []error {
	return nil
}

// newNICTestService wires a VMService whose target of type targetType has a
// password the real encryptor can decrypt, so TargetService.GetProvider
// goes through the registry exactly as it does in production.
func newNICTestService(t *testing.T, targetType string) (*VMService, *db.DB, int64) {
	t.Helper()
	database := newTestDB(t)
	enc, err := crypto.NewEncryptor(nicTestKey)
	if err != nil {
		t.Fatalf("new encryptor: %v", err)
	}
	encPW, err := enc.Encrypt("secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	target := &models.Target{Name: "t", Type: targetType, Hostname: "h.example.com", Port: 443, Username: "u", PasswordEncrypt: encPW}
	if err := database.CreateTarget(target); err != nil {
		t.Fatalf("create target: %v", err)
	}
	vm := &models.ManagedVM{TargetID: target.ID, VMName: "web-01", VMRef: "vm-100", PowerState: "poweredOff"}
	if err := database.CreateManagedVM(vm); err != nil {
		t.Fatalf("create managed vm: %v", err)
	}
	fakeNIC = &fakeNICProvider{}
	if targetType == "esxi" {
		useFakeNICProvider(t)
	}
	return NewVMService(database, NewTargetService(database, enc), enc), database, vm.ID
}

func TestAddNICSuccessPassesSpecThroughAndResyncs(t *testing.T) {
	svc, database, vmID := newNICTestService(t, "esxi")

	nic, err := svc.AddNIC(context.Background(), vmID, AddNICRequest{Network: "  VM Network ", AdapterType: "vmxnet3", Connected: true})
	if err != nil {
		t.Fatalf("AddNIC: %v", err)
	}
	if nic == nil || nic.Key != 4001 || nic.Network != "VM Network" || !nic.Connected {
		t.Errorf("unexpected NIC: %+v", nic)
	}
	if len(fakeNIC.addNICCalls) != 1 {
		t.Fatalf("expected exactly one provider AddNIC call, got %d", len(fakeNIC.addNICCalls))
	}
	got := fakeNIC.addNICCalls[0]
	if got.Network != "VM Network" || got.AdapterType != "vmxnet3" || !got.Connected {
		t.Errorf("provider received %+v, want trimmed network / vmxnet3 / connected", got)
	}

	// The post-attach sync must have run and landed in the DB.
	if fakeNIC.statusCalls != 1 {
		t.Errorf("expected one GetVMStatus call from the post-attach sync, got %d", fakeNIC.statusCalls)
	}
	stored, err := database.GetManagedVM(vmID)
	if err != nil {
		t.Fatalf("get vm: %v", err)
	}
	if stored.PowerState != "poweredOn" || stored.IPAddress != "10.0.0.5" {
		t.Errorf("expected the sync to refresh power state/IP, got state=%q ip=%q", stored.PowerState, stored.IPAddress)
	}
}

func TestAddNICDefaultsAdapterTypeToProviderDefault(t *testing.T) {
	svc, _, vmID := newNICTestService(t, "esxi")
	if _, err := svc.AddNIC(context.Background(), vmID, AddNICRequest{Network: "VM Network", Connected: true}); err != nil {
		t.Fatalf("AddNIC: %v", err)
	}
	// The service must not invent an adapter type — an empty value is the
	// provider's cue to apply its own default (vmxnet3 on vSphere).
	if got := fakeNIC.addNICCalls[0].AdapterType; got != "" {
		t.Errorf("expected empty adapter type passed through, got %q", got)
	}
}

func TestAddNICVMNotFound(t *testing.T) {
	svc, _, _ := newNICTestService(t, "esxi")
	_, err := svc.AddNIC(context.Background(), 99999, AddNICRequest{Network: "VM Network"})
	if !errors.Is(err, ErrVMNotFound) {
		t.Fatalf("expected ErrVMNotFound, got %v", err)
	}
	if len(fakeNIC.addNICCalls) != 0 {
		t.Error("provider must not be called for an unknown VM")
	}
}

func TestAddNICRejectsEmptyNetworkBeforeTouchingProvider(t *testing.T) {
	svc, _, vmID := newNICTestService(t, "esxi")
	_, err := svc.AddNIC(context.Background(), vmID, AddNICRequest{Network: "   "})
	if !errors.Is(err, ErrInvalidNICSpec) {
		t.Fatalf("expected ErrInvalidNICSpec, got %v", err)
	}
	if len(fakeNIC.addNICCalls) != 0 {
		t.Error("provider must not be called with an empty network")
	}
}

func TestAddNICInvalidNetworkSurfacesProviderSentinel(t *testing.T) {
	svc, database, vmID := newNICTestService(t, "esxi")
	fakeNIC.addNICErr = errors.Join(provider.ErrNetworkNotFound, errors.New("\"nope\""))

	_, err := svc.AddNIC(context.Background(), vmID, AddNICRequest{Network: "nope"})
	if !errors.Is(err, provider.ErrNetworkNotFound) {
		t.Fatalf("expected ErrNetworkNotFound, got %v", err)
	}
	// No sync on failure, and the record is untouched.
	if fakeNIC.statusCalls != 0 {
		t.Error("a failed attach must not trigger the post-attach sync")
	}
	stored, _ := database.GetManagedVM(vmID)
	if stored.PowerState != "poweredOff" {
		t.Errorf("record should be untouched after a failed attach, got state %q", stored.PowerState)
	}
}

func TestAddNICUnsupportedProviderRefusesWithoutConnecting(t *testing.T) {
	// Both in-tree providers implement AddNIC now, so simulate a provider
	// whose metadata declares NICAttach: false by temporarily swapping the
	// "esxi" metadata. The service must answer ErrNotSupported without
	// ever building a provider (the fake would record a call otherwise).
	svc, _, vmID := newNICTestService(t, "esxi")
	orig := provider.GetMetadata("esxi")
	noNIC := *orig
	noNIC.Features.NICAttach = false
	provider.RegisterMetadata("esxi", &noNIC)
	t.Cleanup(func() { provider.RegisterMetadata("esxi", orig) })

	_, err := svc.AddNIC(context.Background(), vmID, AddNICRequest{Network: "VM Network"})
	if !errors.Is(err, provider.ErrNotSupported) {
		t.Fatalf("expected ErrNotSupported, got %v", err)
	}
	if len(fakeNIC.addNICCalls) != 0 {
		t.Error("provider must not be called when metadata says NICAttach is unsupported")
	}
}

func TestAddNICVLANTagValidation(t *testing.T) {
	svc, _, vmID := newNICTestService(t, "esxi")

	// Out of range is rejected before anything else.
	for _, tag := range []int{-1, 4095, 70000} {
		_, err := svc.AddNIC(context.Background(), vmID, AddNICRequest{Network: "VM Network", VLANTag: tag})
		if !errors.Is(err, ErrInvalidNICSpec) {
			t.Errorf("VLANTag %d: expected ErrInvalidNICSpec, got %v", tag, err)
		}
	}
	// In range but on a provider without VLANTagging (esxi) is refused —
	// silently dropping it would misreport the NIC as isolated.
	_, err := svc.AddNIC(context.Background(), vmID, AddNICRequest{Network: "VM Network", VLANTag: 20})
	if !errors.Is(err, ErrInvalidNICSpec) {
		t.Fatalf("expected ErrInvalidNICSpec for a VLAN tag on esxi, got %v", err)
	}
	if len(fakeNIC.addNICCalls) != 0 {
		t.Error("provider must not be called for a rejected VLAN tag")
	}

	// With VLANTagging declared, the tag passes straight through.
	orig := provider.GetMetadata("esxi")
	vlan := *orig
	vlan.Features.VLANTagging = true
	provider.RegisterMetadata("esxi", &vlan)
	t.Cleanup(func() { provider.RegisterMetadata("esxi", orig) })
	if _, err := svc.AddNIC(context.Background(), vmID, AddNICRequest{Network: "vmbr0", VLANTag: 20}); err != nil {
		t.Fatalf("AddNIC with VLAN: %v", err)
	}
	if got := fakeNIC.addNICCalls[0].VLANTag; got != 20 {
		t.Errorf("provider received VLANTag %d, want 20", got)
	}
}

func TestAddNICInvalidAdapterTypeRejectedBeforeConnecting(t *testing.T) {
	// "esxi" metadata publishes NICAdapterTypes, so a model outside that
	// list must be refused by the service without a provider call.
	svc, _, vmID := newNICTestService(t, "esxi")
	_, err := svc.AddNIC(context.Background(), vmID, AddNICRequest{Network: "VM Network", AdapterType: "virtio"})
	if !errors.Is(err, provider.ErrInvalidAdapterType) {
		t.Fatalf("expected ErrInvalidAdapterType, got %v", err)
	}
	if len(fakeNIC.addNICCalls) != 0 {
		t.Error("provider must not be called for an invalid adapter type")
	}

	// Case/whitespace are normalised, not rejected.
	if _, err := svc.AddNIC(context.Background(), vmID, AddNICRequest{Network: "VM Network", AdapterType: " E1000E "}); err != nil {
		t.Fatalf("expected normalised adapter type to be accepted, got %v", err)
	}
	if got := fakeNIC.addNICCalls[0].AdapterType; got != "e1000e" {
		t.Errorf("provider received adapter %q, want e1000e", got)
	}
}

func TestListNICsReturnsProviderInventoryAndNeverNil(t *testing.T) {
	svc, _, vmID := newNICTestService(t, "esxi")
	fakeNIC.nics = []provider.NIC{
		{Key: 4000, Label: "Network adapter 1", AdapterType: "vmxnet3", Network: "VM Network", MACAddress: "00:50:56:00:00:01", Connected: true, Addresses: []string{"10.20.10.11", "fe80::1"}},
		{Key: 4001, Label: "Network adapter 2", AdapterType: "e1000e", Network: "dvPG-Backend", MACAddress: "00:50:56:00:00:02", Connected: false},
	}
	got, err := svc.ListNICs(context.Background(), vmID)
	if err != nil {
		t.Fatalf("ListNICs: %v", err)
	}
	if len(got) != 2 || got[1].Network != "dvPG-Backend" || got[0].Addresses[0] != "10.20.10.11" {
		t.Errorf("unexpected inventory: %+v", got)
	}

	// A VM with no adapters is an empty list, not null — the UI iterates it.
	fakeNIC.nics = nil
	got, err = svc.ListNICs(context.Background(), vmID)
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("expected empty non-nil slice, got %v, %v", got, err)
	}
}

func TestListNICsVMNotFound(t *testing.T) {
	svc, _, _ := newNICTestService(t, "esxi")
	_, err := svc.ListNICs(context.Background(), 424242)
	if !errors.Is(err, ErrVMNotFound) {
		t.Fatalf("expected ErrVMNotFound, got %v", err)
	}
}

func TestResizeSurfacesRequiresPowerOffSentinel(t *testing.T) {
	svc, database, vmID := newNICTestService(t, "esxi")
	// Powered-on VM: the service refuses before touching the provider, and
	// the error is the sentinel the handler maps to 409 — not matched text.
	if err := database.UpdateManagedVMState(vmID, "poweredOn", "", nil, nil, nil, 0); err != nil {
		t.Fatalf("set state: %v", err)
	}
	err := svc.Resize(context.Background(), vmID, 4, 0)
	if !errors.Is(err, ErrRequiresPowerOff) {
		t.Fatalf("expected ErrRequiresPowerOff, got %v", err)
	}
}

func TestSyncAllUsesListingAndSkipsPerVMStatusCalls(t *testing.T) {
	svc, database, vmID := newNICTestService(t, "esxi")
	fakeNIC.vms = []provider.VMInfo{{ID: "vm-100", Name: "web-01", PowerState: "poweredOn", IPAddress: "10.0.0.9", CPU: 4, MemoryMB: 8192, DiskGB: 80, GuestID: "ubuntu64Guest"}}

	res, err := svc.SyncAll(context.Background(), false)
	if err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	if res.Synced != 1 || res.Orphaned != 0 || len(res.Errors) != 0 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if fakeNIC.statusCalls != 0 {
		t.Errorf("listing covered everything; GetVMStatus should not be called, was called %d times", fakeNIC.statusCalls)
	}
	vm, _ := database.GetManagedVM(vmID)
	if vm.PowerState != "poweredOn" || vm.IPAddress != "10.0.0.9" || vm.CPU != 4 || vm.MemoryMB != 8192 || vm.DiskGB != 80 || vm.OSType != "ubuntu64Guest" {
		t.Errorf("sync from listing did not record the same fields a status call would: %+v", vm)
	}
}

func TestSyncAllFallsBackToStatusWhenListingLacksIPForRunningVM(t *testing.T) {
	svc, database, vmID := newNICTestService(t, "esxi")
	// Proxmox-style listing: running VM, no IP without the guest agent.
	fakeNIC.vms = []provider.VMInfo{{ID: "vm-100", Name: "web-01", PowerState: "poweredOn", CPU: 2, MemoryMB: 2048}}

	if _, err := svc.SyncAll(context.Background(), false); err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	if fakeNIC.statusCalls != 1 {
		t.Errorf("expected exactly one GetVMStatus fallback for the running VM without an IP, got %d", fakeNIC.statusCalls)
	}
	vm, _ := database.GetManagedVM(vmID)
	if vm.IPAddress != "10.0.0.5" { // from the fake's GetVMStatus
		t.Errorf("fallback status should have supplied the IP, got %q", vm.IPAddress)
	}
}

func TestSyncAllStillUsesStatusWhenListingUnavailable(t *testing.T) {
	svc, _, _ := newNICTestService(t, "esxi") // fakeNIC.vms nil => ListVMs errors
	if _, err := svc.SyncAll(context.Background(), false); err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	if fakeNIC.statusCalls != 1 {
		t.Errorf("with no listing every VM needs a status call; got %d", fakeNIC.statusCalls)
	}
}
