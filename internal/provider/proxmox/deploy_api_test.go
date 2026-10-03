package proxmox

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/forgemill/forgemill/internal/provider"
)

// The post-clone configuration is where a Proxmox deploy can quietly produce
// the wrong VM: these tests pin that hardware and cloud-init are written
// separately, that a rejection fails the deployment with Proxmox's reason
// and removes the clone, and that the clone lock is retried.

func deploySpec() *provider.DeploySpec {
	return &provider.DeploySpec{
		TemplateName: "tmpl", VMName: "fm-audit-pve", CPU: 1, MemoryMB: 1024, DiskGB: 22,
		Network: "vmbr0", Hostname: "fm-audit-pve", PlainPassword: "pw", SSHPublicKey: "ssh-ed25519 AAAATEST e2e",
	}
}

func TestDeployVMWritesHardwareAndCloudInitInSeparatePuts(t *testing.T) {
	f := newFakePVE(t)
	p := f.provider(t)

	res, err := p.DeployVM(context.Background(), deploySpec())
	if err != nil {
		t.Fatalf("DeployVM: %v", err)
	}
	if res.VMID != "101" {
		t.Errorf("VMID = %q, want 101", res.VMID)
	}
	if len(f.newPuts) != 2 {
		t.Fatalf("want 2 config PUTs (hardware, cloud-init), got %d: %v", len(f.newPuts), f.newPuts)
	}
	hw, ci := f.newPuts[0], f.newPuts[1]
	if hw.Get("cores") != "1" || hw.Get("memory") != "1024" || hw.Get("net0") == "" || len(hw) != 3 {
		t.Errorf("hardware PUT must carry exactly cores/memory/net0: %v", hw)
	}
	if ci.Get("ciuser") != "forgemill" || ci.Get("cipassword") != "pw" || ci.Get("name") != "fm-audit-pve" || ci.Get("ipconfig0") != "ip=dhcp" || ci.Get("sshkeys") == "" {
		t.Errorf("cloud-init PUT missing keys: %v", ci)
	}
	if ci.Has("cores") || ci.Has("memory") {
		t.Errorf("hardware keys must not be repeated in the cloud-init PUT: %v", ci)
	}
	if !f.started || f.deleted {
		t.Errorf("successful deploy must start the VM and not delete it (started=%v deleted=%v)", f.started, f.deleted)
	}
}

func TestDeployVMFailsLoudlyAndRemovesCloneWhenCloudInitIsRejected(t *testing.T) {
	f := newFakePVE(t)
	f.rejectKeys = []string{"sshkeys"}
	p := f.provider(t)

	start := time.Now()
	_, err := p.DeployVM(context.Background(), deploySpec())
	if err == nil {
		t.Fatal("a rejected post-clone config must fail the deployment, not complete silently")
	}
	// The rollback deletes a VM that is already off; it must not sit in the
	// "wait for stopped" loop for its full 30 s (it compared the raw Proxmox
	// state with the canonical one and never matched).
	if time.Since(start) > 10*time.Second {
		t.Errorf("rollback took %s; deleting an already-stopped VM must be quick", time.Since(start))
	}
	msg := err.Error()
	for _, want := range []string{"configure VM 101 after clone", "cloud-init config", "HTTP 400", "sshkeys: invalid format", "clone 101 has been removed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should contain %q, got: %s", want, msg)
		}
	}
	var ae *apiError
	if !errors.As(err, &ae) || ae.Status != 400 {
		t.Errorf("Proxmox's rejection should be carried as an apiError, got %T", err)
	}
	// Hardware was still applied (its own PUT), the clone was rolled back, never started.
	if len(f.newPuts) != 1 || f.newPuts[0].Get("cores") != "1" {
		t.Errorf("hardware PUT should have been accepted first: %v", f.newPuts)
	}
	if !f.deleted || f.started {
		t.Errorf("failed deploy must delete the clone and not start it (deleted=%v started=%v)", f.deleted, f.started)
	}
	if f.hit("/nodes/pve/qemu/101/config") > 3 {
		t.Errorf("a 400 must not be retried, got %d config requests", f.hit("/nodes/pve/qemu/101/config"))
	}
}

func TestDeployVMRetriesWhileProxmoxHoldsTheCloneLock(t *testing.T) {
	f := newFakePVE(t)
	f.lockFailures = 2
	p := f.provider(t)

	if _, err := p.DeployVM(context.Background(), deploySpec()); err != nil {
		t.Fatalf("lock should be retried, got: %v", err)
	}
	if len(f.newPuts) != 2 || f.deleted {
		t.Errorf("after the lock clears both PUTs must land and nothing is rolled back: puts=%d deleted=%v", len(f.newPuts), f.deleted)
	}
}

func TestAPIErrorCarriesProxmoxMessageAndRetryabilityIsByCause(t *testing.T) {
	rejected := newAPIError(400, []byte(`{"data":null,"errors":{"sshkeys":"invalid format","memory":"too big"},"message":"Parameter verification failed.\n"}`))
	if rejected.Error() != "proxmox request failed (HTTP 400): Parameter verification failed. — memory: too big; sshkeys: invalid format" {
		t.Errorf("unexpected message: %s", rejected.Error())
	}
	if isRetryableConfigError(rejected) {
		t.Error("a parameter rejection must not be retried")
	}
	locked := newAPIError(500, []byte(`{"data":null,"message":"VM is locked (clone)"}`))
	if !isRetryableConfigError(locked) {
		t.Error("a lock must be retried")
	}
	bare := newAPIError(502, []byte("<html>bad gateway</html>"))
	if bare.Error() != "proxmox request failed (HTTP 502)" || !isRetryableConfigError(bare) {
		t.Errorf("non-JSON bodies keep the old wording and 5xx is retryable: %s", bare.Error())
	}
}

func TestDeployVMReportsFailedResizeAsPartialDeployKeepingTheVM(t *testing.T) {
	f := newFakePVE(t)
	f.resizeFails = true
	p := f.provider(t)

	_, err := p.DeployVM(context.Background(), deploySpec())
	var partial *provider.PartialDeployError
	if !errors.As(err, &partial) {
		t.Fatalf("a failed post-clone resize must be a PartialDeployError, got %v", err)
	}
	if partial.VMID != "101" || !strings.Contains(err.Error(), "resize disk") || !strings.Contains(err.Error(), "storage 'local-zfs' is full") {
		t.Errorf("partial error should name the VM and carry Proxmox's reason: vmid=%q err=%v", partial.VMID, err)
	}
	if f.deleted {
		t.Error("the clone must not be rolled back on a resize failure — it is kept for the operator")
	}
	if f.started {
		t.Error("a VM whose resize failed must not be started as if nothing happened")
	}
}
