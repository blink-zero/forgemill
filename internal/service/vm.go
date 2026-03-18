package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/provider"
)

type VMService struct {
	db        *db.DB
	targets   *TargetService
	encryptor Encryptor
	// syncDebounce coalesces rapid mutations into a single background SyncAll
	syncTimer *time.Timer
	syncMu    sync.Mutex
}

func NewVMService(db *db.DB, targets *TargetService, enc Encryptor) *VMService {
	return &VMService{db: db, targets: targets, encryptor: enc}
}

// scheduleSyncAll triggers a background SyncAll after a short delay.
// Multiple calls within the delay window are coalesced into one sync.
func (s *VMService) scheduleSyncAll(delay time.Duration) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if s.syncTimer != nil {
		s.syncTimer.Stop()
	}
	s.syncTimer = time.AfterFunc(delay, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if _, err := s.SyncAll(ctx); err != nil {
			slog.Warn("auto-sync after mutation failed", "error", err)
		} else {
			slog.Info("auto-sync completed after VM mutation")
		}
	})
}

// VMCredentials holds the decrypted SSH credentials for a deployed VM.
type VMCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// GetCredentials returns the deploy credentials for a managed VM.
func (s *VMService) GetCredentials(ctx context.Context, vmID int64) (*VMCredentials, error) {
	vm, err := s.db.GetManagedVM(vmID)
	if err != nil {
		return nil, fmt.Errorf("get VM: %w", err)
	}
	if vm.DeploymentID == nil || *vm.DeploymentID == 0 {
		return nil, fmt.Errorf("VM was not deployed by Forgemill")
	}
	dep, err := s.db.GetDeployment(*vm.DeploymentID)
	if err != nil {
		return nil, fmt.Errorf("get deployment: %w", err)
	}
	if dep.InitialPwdEnc == "" {
		return nil, fmt.Errorf("no credentials stored for this deployment")
	}
	if s.encryptor == nil {
		return nil, fmt.Errorf("encryption not available")
	}
	pwd, err := s.encryptor.Decrypt(dep.InitialPwdEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt credentials: %w", err)
	}
	return &VMCredentials{
		Username: dep.InitialUsername,
		Password: pwd,
	}, nil
}

func (s *VMService) List() ([]models.ManagedVM, error) {
	return s.db.ListManagedVMs()
}

// Get retrieves a VM, auto-syncing from hypervisor if stale (>60s) or never synced.
func (s *VMService) Get(ctx context.Context, id int64) (*models.ManagedVM, error) {
	vm, err := s.db.GetManagedVM(id)
	if err != nil {
		return nil, err
	}

	// Auto-sync if never synced or older than 60 seconds
	if vm.LastSyncedAt == nil || time.Since(*vm.LastSyncedAt) > 60*time.Second {
		synced, syncErr := s.SyncState(ctx, id)
		if syncErr != nil {
			slog.Warn("auto-sync failed for VM", "vm_id", id, "error", syncErr)
			return vm, nil // return stale data rather than error
		}
		return synced, nil
	}

	return vm, nil
}

func (s *VMService) Create(vm *models.ManagedVM) error {
	if vm.PowerState == "" {
		vm.PowerState = "unknown"
	}
	return s.db.CreateManagedVM(vm)
}

func (s *VMService) Delete(ctx context.Context, id int64, force bool) error {
	vm, err := s.db.GetManagedVM(id)
	if err != nil {
		return fmt.Errorf("VM not found: %w", err)
	}

	if force {
		// Force delete: skip hypervisor, just remove DB record
		return s.db.DeleteManagedVM(id)
	}

	p, err := s.targets.GetProvider(vm.TargetID)
	if err != nil {
		return fmt.Errorf("get provider: %w", err)
	}
	defer p.Disconnect()

	// Use a dedicated timeout for VM deletion (stop + delete can take >60s)
	deleteCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	if err := p.Connect(deleteCtx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	// V3-L6: Return error on hypervisor failure instead of silently deleting DB record
	if err := p.DeleteVM(deleteCtx, vm.VMRef); err != nil {
		return fmt.Errorf("failed to delete VM from hypervisor: %w", err)
	}

	return s.db.DeleteManagedVM(id)
}

func (s *VMService) PowerAction(ctx context.Context, id int64, action string) error {
	vm, err := s.db.GetManagedVM(id)
	if err != nil {
		return fmt.Errorf("VM not found: %w", err)
	}

	p, err := s.targets.GetProvider(vm.TargetID)
	if err != nil {
		return fmt.Errorf("get provider: %w", err)
	}
	defer p.Disconnect()

	if err := p.Connect(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	switch action {
	case "start":
		if err := p.PowerOn(ctx, vm.VMRef); err != nil {
			return fmt.Errorf("power on: %w", err)
		}
		if err := s.db.UpdateManagedVMState(id, "poweredOn", vm.IPAddress); err != nil {
			slog.Error("failed to update VM state", "vm_id", id, "state", "poweredOn", "error", err)
		}
	case "stop":
		if err := p.PowerOff(ctx, vm.VMRef); err != nil {
			return fmt.Errorf("power off: %w", err)
		}
		if err := s.db.UpdateManagedVMState(id, "poweredOff", vm.IPAddress); err != nil {
			slog.Error("failed to update VM state", "vm_id", id, "state", "poweredOff", "error", err)
		}
	case "restart":
		if err := p.Restart(ctx, vm.VMRef); err != nil {
			return fmt.Errorf("restart: %w", err)
		}
		if err := s.db.UpdateManagedVMState(id, "poweredOn", vm.IPAddress); err != nil {
			slog.Error("failed to update VM state", "vm_id", id, "state", "poweredOn", "error", err)
		}
	case "suspend":
		if err := p.Suspend(ctx, vm.VMRef); err != nil {
			return fmt.Errorf("suspend: %w", err)
		}
		if err := s.db.UpdateManagedVMState(id, "suspended", vm.IPAddress); err != nil {
			slog.Error("failed to update VM state", "vm_id", id, "state", "suspended", "error", err)
		}
	default:
		return fmt.Errorf("unknown power action: %s", action)
	}

	// Auto-sync after power state change (delay lets hypervisor settle)
	s.scheduleSyncAll(5 * time.Second)
	return nil
}

func (s *VMService) SyncState(ctx context.Context, id int64) (*models.ManagedVM, error) {
	vm, err := s.db.GetManagedVM(id)
	if err != nil {
		return nil, fmt.Errorf("VM not found: %w", err)
	}

	p, err := s.targets.GetProvider(vm.TargetID)
	if err != nil {
		return nil, fmt.Errorf("get provider: %w", err)
	}
	defer p.Disconnect()

	if err := p.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	status, err := p.GetVMStatus(ctx, vm.VMRef)
	if err != nil {
		return nil, fmt.Errorf("get VM status: %w", err)
	}

	if err := s.db.UpdateManagedVMState(id, status.PowerState, status.IPAddress); err != nil {
		slog.Error("failed to update VM state", "vm_id", id, "state", status.PowerState, "error", err)
	}
	// Update resource info if available from provider
	if status.CPU > 0 || status.MemoryMB > 0 || status.DiskGB > 0 {
		if err := s.db.UpdateManagedVMResources(id, status.CPU, status.MemoryMB, status.DiskGB); err != nil {
			slog.Error("failed to update VM resources", "vm_id", id, "error", err)
		}
		vm.CPU = status.CPU
		vm.MemoryMB = status.MemoryMB
		vm.DiskGB = status.DiskGB
	}
	if status.GuestID != "" && vm.OSType == "" {
		if err := s.db.UpdateManagedVMOSType(id, status.GuestID); err != nil {
			slog.Error("failed to update VM OS type", "vm_id", id, "error", err)
		}
		vm.OSType = status.GuestID
	}
	vm.PowerState = status.PowerState
	vm.IPAddress = status.IPAddress
	now := time.Now()
	vm.LastSyncedAt = &now
	return vm, nil
}

// SyncAll syncs all managed VMs with their hypervisors and cleans up orphans.
func (s *VMService) SyncAll(ctx context.Context) (*SyncAllResult, error) {
	allVMs, err := s.db.ListManagedVMs()
	if err != nil {
		return nil, fmt.Errorf("list VMs: %w", err)
	}

	result := &SyncAllResult{}

	// Group VMs by target to reuse provider connections
	byTarget := map[int64][]models.ManagedVM{}
	for _, vm := range allVMs {
		byTarget[vm.TargetID] = append(byTarget[vm.TargetID], vm)
	}

	for targetID, targetVMs := range byTarget {
		p, err := s.targets.GetProvider(targetID)
		if err != nil {
			slog.Error("sync-all: failed to get provider", "target_id", targetID, "error", err)
			result.Errors = append(result.Errors, fmt.Sprintf("target %d: %v", targetID, err))
			continue
		}

		if err := p.Connect(ctx); err != nil {
			p.Disconnect()
			slog.Error("sync-all: failed to connect", "target_id", targetID, "error", err)
			result.Errors = append(result.Errors, fmt.Sprintf("target %d connect: %v", targetID, err))
			continue
		}

		// Get all VMs from hypervisor for orphan detection
		hypervisorVMs, listErr := p.ListVMs(ctx)
		hypervisorRefs := map[string]provider.VMInfo{}
		if listErr == nil {
			for _, hvm := range hypervisorVMs {
				hypervisorRefs[hvm.ID] = hvm
			}
		}

		for _, vm := range targetVMs {
			// Check if VM still exists on hypervisor
			if listErr == nil {
				if _, exists := hypervisorRefs[vm.VMRef]; !exists {
					slog.Info("sync-all: removing orphan VM", "vm_id", vm.ID, "vm_ref", vm.VMRef)
					if err := s.db.DeleteManagedVM(vm.ID); err != nil {
						slog.Error("sync-all: failed to delete orphan", "vm_id", vm.ID, "error", err)
					}
					result.Orphaned++
					continue
				}
			}

			// Update vm_name from hypervisor if it differs from DB
			if listErr == nil {
				if hvm, ok := hypervisorRefs[vm.VMRef]; ok && hvm.Name != "" && hvm.Name != vm.VMName {
					if err := s.db.UpdateManagedVMName(vm.ID, hvm.Name); err != nil {
						slog.Error("sync-all: failed to update VM name", "vm_id", vm.ID, "error", err)
					}
				}
			}

			status, err := p.GetVMStatus(ctx, vm.VMRef)
			if err != nil {
				slog.Warn("sync-all: failed to get status", "vm_id", vm.ID, "vm_ref", vm.VMRef, "error", err)
				result.Errors = append(result.Errors, fmt.Sprintf("vm %d: %v", vm.ID, err))
				continue
			}

			if err := s.db.UpdateManagedVMState(vm.ID, status.PowerState, status.IPAddress); err != nil {
				slog.Error("sync-all: failed to update state", "vm_id", vm.ID, "error", err)
			}
			// Update resource info if available
			if status.CPU > 0 || status.MemoryMB > 0 || status.DiskGB > 0 {
				if err := s.db.UpdateManagedVMResources(vm.ID, status.CPU, status.MemoryMB, status.DiskGB); err != nil {
					slog.Error("sync-all: failed to update resources", "vm_id", vm.ID, "error", err)
				}
			}
			if status.GuestID != "" {
				if err := s.db.UpdateManagedVMOSType(vm.ID, status.GuestID); err != nil {
					slog.Error("sync-all: failed to update OS type", "vm_id", vm.ID, "error", err)
				}
			}
			result.Synced++
		}

		p.Disconnect()
	}

	return result, nil
}

type SyncAllResult struct {
	Synced   int      `json:"synced"`
	Orphaned int      `json:"orphaned"`
	Errors   []string `json:"errors,omitempty"`
}

func (s *VMService) ListSnapshots(vmID int64) ([]models.VMSnapshot, error) {
	return s.db.ListVMSnapshots(vmID)
}

func (s *VMService) CreateSnapshot(ctx context.Context, vmID int64, name, description string, memory bool) error {
	vm, err := s.db.GetManagedVM(vmID)
	if err != nil {
		return fmt.Errorf("VM not found: %w", err)
	}

	p, err := s.targets.GetProvider(vm.TargetID)
	if err != nil {
		return fmt.Errorf("get provider: %w", err)
	}
	defer p.Disconnect()

	if err := p.Connect(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	if err := p.CreateSnapshot(ctx, vm.VMRef, name, description, memory); err != nil {
		return fmt.Errorf("create snapshot: %w", err)
	}

	snap := &models.VMSnapshot{
		VMID:        vmID,
		SnapshotRef: name,
		Name:        name,
		Description: description,
	}
	if err := s.db.CreateVMSnapshot(snap); err != nil {
		return err
	}
	s.scheduleSyncAll(5 * time.Second)
	return nil
}

func (s *VMService) RevertSnapshot(ctx context.Context, vmID, snapID int64) error {
	vm, err := s.db.GetManagedVM(vmID)
	if err != nil {
		return fmt.Errorf("VM not found: %w", err)
	}

	snap, err := s.db.GetVMSnapshot(snapID)
	if err != nil {
		return fmt.Errorf("snapshot not found: %w", err)
	}

	// V3-C2: Verify snapshot belongs to this VM to prevent cross-VM manipulation
	if snap.VMID != vmID {
		return fmt.Errorf("snapshot does not belong to this VM")
	}

	p, err := s.targets.GetProvider(vm.TargetID)
	if err != nil {
		return fmt.Errorf("get provider: %w", err)
	}
	defer p.Disconnect()

	if err := p.Connect(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	if err := p.RevertSnapshot(ctx, vm.VMRef, snap.SnapshotRef); err != nil {
		return err
	}
	s.scheduleSyncAll(5 * time.Second)
	return nil
}

func (s *VMService) DeleteSnapshot(ctx context.Context, vmID, snapID int64) error {
	vm, err := s.db.GetManagedVM(vmID)
	if err != nil {
		return fmt.Errorf("VM not found: %w", err)
	}

	snap, err := s.db.GetVMSnapshot(snapID)
	if err != nil {
		return fmt.Errorf("snapshot not found: %w", err)
	}

	// V3-C2: Verify snapshot belongs to this VM to prevent cross-VM manipulation
	if snap.VMID != vmID {
		return fmt.Errorf("snapshot does not belong to this VM")
	}

	p, err := s.targets.GetProvider(vm.TargetID)
	if err != nil {
		return fmt.Errorf("get provider: %w", err)
	}
	defer p.Disconnect()

	if err := p.Connect(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	// V3-L6: Return error on hypervisor failure instead of silently proceeding
	if err := p.DeleteSnapshot(ctx, vm.VMRef, snap.SnapshotRef); err != nil {
		return fmt.Errorf("failed to delete snapshot from hypervisor: %w", err)
	}

	if err := s.db.DeleteVMSnapshot(snapID); err != nil {
		return err
	}
	s.scheduleSyncAll(5 * time.Second)
	return nil
}

func (s *VMService) Resize(ctx context.Context, id int64, cpu, memoryMB int) error {
	// MED-19: Validate resource bounds (same as deploy validation)
	if cpu > 0 && (cpu < 1 || cpu > 128) {
		return fmt.Errorf("cpu must be between 1 and 128")
	}
	if memoryMB > 0 && (memoryMB < 256 || memoryMB > 1048576) {
		return fmt.Errorf("memory_mb must be between 256 and 1048576 (1TB)")
	}

	vm, err := s.db.GetManagedVM(id)
	if err != nil {
		return fmt.Errorf("VM not found: %w", err)
	}

	if vm.PowerState != "poweredOff" && vm.PowerState != "stopped" {
		return fmt.Errorf("VM must be powered off to resize (current state: %s)", vm.PowerState)
	}

	p, err := s.targets.GetProvider(vm.TargetID)
	if err != nil {
		return fmt.Errorf("get provider: %w", err)
	}
	defer p.Disconnect()

	if err := p.Connect(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	if err := p.ResizeVM(ctx, vm.VMRef, cpu, memoryMB); err != nil {
		return err
	}

	// Update DB with new values (keep old value if not changed)
	newCPU := vm.CPU
	if cpu > 0 {
		newCPU = cpu
	}
	newMem := vm.MemoryMB
	if memoryMB > 0 {
		newMem = memoryMB
	}
	if err := s.db.UpdateManagedVMResources(id, newCPU, newMem, vm.DiskGB); err != nil {
		slog.Error("failed to update VM resources after resize", "vm_id", id, "error", err)
	}
	s.scheduleSyncAll(5 * time.Second)
	return nil
}

func (s *VMService) ListDisks(ctx context.Context, id int64) ([]provider.Disk, error) {
	vm, err := s.db.GetManagedVM(id)
	if err != nil {
		return nil, fmt.Errorf("VM not found: %w", err)
	}
	p, err := s.targets.GetProvider(vm.TargetID)
	if err != nil {
		return nil, fmt.Errorf("get provider: %w", err)
	}
	defer p.Disconnect()
	if err := p.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return p.ListDisks(ctx, vm.VMRef)
}

func (s *VMService) ExpandDisk(ctx context.Context, id int64, diskKey, newSizeGB int) error {
	// MED-20: Validate disk size bounds
	if newSizeGB <= 0 || newSizeGB > 65536 {
		return fmt.Errorf("new_size_gb must be between 1 and 65536")
	}

	vm, err := s.db.GetManagedVM(id)
	if err != nil {
		return fmt.Errorf("VM not found: %w", err)
	}

	if newSizeGB <= vm.DiskGB {
		return fmt.Errorf("new disk size must be larger than current size (%dGB)", vm.DiskGB)
	}

	p, err := s.targets.GetProvider(vm.TargetID)
	if err != nil {
		return fmt.Errorf("get provider: %w", err)
	}
	defer p.Disconnect()

	if err := p.Connect(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	if err := p.ExpandDisk(ctx, vm.VMRef, diskKey, newSizeGB); err != nil {
		return err
	}

	// Update DB with new disk size
	if err := s.db.UpdateManagedVMResources(id, vm.CPU, vm.MemoryMB, newSizeGB); err != nil {
		slog.Error("failed to update VM disk_gb after expand", "vm_id", id, "error", err)
	}
	s.scheduleSyncAll(5 * time.Second)
	return nil
}

func (s *VMService) GetConsoleURL(ctx context.Context, id int64) (string, error) {
	vm, err := s.db.GetManagedVM(id)
	if err != nil {
		return "", fmt.Errorf("VM not found: %w", err)
	}

	p, err := s.targets.GetProvider(vm.TargetID)
	if err != nil {
		return "", fmt.Errorf("get provider: %w", err)
	}
	defer p.Disconnect()

	if err := p.Connect(ctx); err != nil {
		return "", fmt.Errorf("connect: %w", err)
	}

	return p.GetConsoleURL(ctx, vm.VMRef)
}

// ResetHostKey clears the stored SSH host key fingerprint for a VM.
// Next SSH connection will trust-on-first-use again.
func (s *VMService) ResetHostKey(id int64) error {
	return s.db.UpdateManagedVMHostKeyFP(id, "")
}
