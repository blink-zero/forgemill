package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/provider"
)

// ErrTargetNotFound is returned for discovery/adoption against an unknown target.
var ErrTargetNotFound = errors.New("target not found")

// DiscoveredVM is a VM the hypervisor reports that Forgemill doesn't manage.
type DiscoveredVM struct {
	Ref        string `json:"ref"`
	Name       string `json:"name"`
	PowerState string `json:"power_state"`
	IPAddress  string `json:"ip_address,omitempty"`
	CPU        int    `json:"cpu"`
	MemoryMB   int    `json:"memory_mb"`
	DiskGB     int    `json:"disk_gb"`
	GuestID    string `json:"guest_id,omitempty"`
	Host       string `json:"host,omitempty"`
	Ignored    bool   `json:"ignored"`
}

// DiscoverResult is one live look at a target.
type DiscoverResult struct {
	TargetID   int64          `json:"target_id"`
	TargetName string         `json:"target_name"`
	ComputedAt time.Time      `json:"computed_at"`
	Managed    int            `json:"managed"`   // VMs on this target Forgemill already tracks
	Unmanaged  int            `json:"unmanaged"` // listed below and not ignored
	Ignored    int            `json:"ignored"`   // on the ignore list (listed only with includeIgnored)
	VMs        []DiscoveredVM `json:"vms"`
}

// AdoptSkipped explains why a requested ref was not adopted.
type AdoptSkipped struct {
	Ref    string `json:"ref"`
	Reason string `json:"reason"`
}

// AdoptResult reports what Adopt did.
type AdoptResult struct {
	Adopted []models.ManagedVM `json:"adopted"`
	Skipped []AdoptSkipped     `json:"skipped"`
}

// Discover lists the VMs on a target that Forgemill does not manage. It is a
// live read (one ListVMs, the same call SyncAll makes), never a cached view;
// templates are excluded by the providers, managed VMs here, ignored VMs
// unless includeIgnored. As a side effect it refreshes the target's
// unmanaged count.
func (s *VMService) Discover(ctx context.Context, targetID int64, includeIgnored bool) (*DiscoverResult, error) {
	target, err := s.targets.Get(targetID)
	if err != nil {
		return nil, fmt.Errorf("%w: id %d", ErrTargetNotFound, targetID)
	}
	p, err := s.targets.GetProvider(targetID)
	if err != nil {
		return nil, fmt.Errorf("get provider: %w", err)
	}
	defer p.Disconnect()
	if err := p.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	listed, err := p.ListVMs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list VMs: %w", err)
	}

	managedRefs, err := s.managedRefs(targetID)
	if err != nil {
		return nil, err
	}
	ignored, err := s.ignoredRefs(targetID)
	if err != nil {
		return nil, err
	}

	res := &DiscoverResult{TargetID: target.ID, TargetName: target.Name, ComputedAt: time.Now().UTC(), VMs: []DiscoveredVM{}}
	for _, vm := range listed {
		if managedRefs[vm.ID] {
			res.Managed++
			continue
		}
		_, isIgnored := ignored[vm.ID]
		if isIgnored {
			res.Ignored++
			if !includeIgnored {
				continue
			}
		} else {
			res.Unmanaged++
		}
		res.VMs = append(res.VMs, DiscoveredVM{Ref: vm.ID, Name: vm.Name, PowerState: vm.PowerState, IPAddress: vm.IPAddress,
			CPU: vm.CPU, MemoryMB: vm.MemoryMB, DiskGB: vm.DiskGB, GuestID: vm.GuestID, Host: vm.Host, Ignored: isIgnored})
	}
	sort.SliceStable(res.VMs, func(i, j int) bool { return strings.ToLower(res.VMs[i].Name) < strings.ToLower(res.VMs[j].Name) })
	if err := s.db.UpdateTargetUnmanaged(targetID, res.Unmanaged); err != nil {
		slog.Warn("discover: could not record unmanaged count", "target_id", targetID, "error", err)
	}
	return res, nil
}

// Adopt takes the given VMs (by hypervisor ref) under management: one
// managed_vms row each with origin "adopted", seeded from the listing and
// synced immediately. Idempotent — an already-managed ref is reported as
// skipped, not as an error; a ref the target doesn't have is skipped too.
// Adopting removes the ref from the ignore list.
func (s *VMService) Adopt(ctx context.Context, targetID int64, refs []string, actorID int64, actorName string) (*AdoptResult, error) {
	target, err := s.targets.Get(targetID)
	if err != nil {
		return nil, fmt.Errorf("%w: id %d", ErrTargetNotFound, targetID)
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("%w: no VM refs given", ErrInvalidNICSpec)
	}
	p, err := s.targets.GetProvider(targetID)
	if err != nil {
		return nil, fmt.Errorf("get provider: %w", err)
	}
	defer p.Disconnect()
	if err := p.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	listed, err := p.ListVMs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list VMs: %w", err)
	}
	byRef := map[string]provider.VMInfo{}
	for _, vm := range listed {
		byRef[vm.ID] = vm
	}
	managedRefs, err := s.managedRefs(targetID)
	if err != nil {
		return nil, err
	}

	res := &AdoptResult{Adopted: []models.ManagedVM{}, Skipped: []AdoptSkipped{}}
	now := time.Now().UTC()
	var unignore []string
	seen := map[string]bool{}
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		info, onTarget := byRef[ref]
		switch {
		case managedRefs[ref]:
			res.Skipped = append(res.Skipped, AdoptSkipped{Ref: ref, Reason: "already managed"})
			continue
		case !onTarget:
			res.Skipped = append(res.Skipped, AdoptSkipped{Ref: ref, Reason: "not found on target (templates cannot be adopted)"})
			continue
		}
		vm := &models.ManagedVM{
			TargetID: targetID, VMName: info.Name, VMRef: ref,
			PowerState: info.PowerState, IPAddress: info.IPAddress,
			CPU: info.CPU, MemoryMB: info.MemoryMB, DiskGB: info.DiskGB, OSType: info.GuestID,
			Origin: models.VMOriginAdopted, AdoptedAt: &now, AdoptedBy: &actorID,
		}
		initial, _ := applyPowerStateTransition(vmLifecycleState{PowerState: "unknown"}, vm.PowerState, now)
		vm.StateChangedAt = initial.StateChangedAt
		vm.LastPoweredOnAt = initial.LastPoweredOnAt
		vm.LastPoweredOffAt = initial.LastPoweredOffAt
		if err := s.db.CreateManagedVM(vm); err != nil {
			if errors.Is(err, dbErrAlreadyRegistered()) {
				res.Skipped = append(res.Skipped, AdoptSkipped{Ref: ref, Reason: "already managed"})
				continue
			}
			return nil, fmt.Errorf("adopt %s: %w", ref, err)
		}
		managedRefs[ref] = true
		unignore = append(unignore, ref)
		s.recordVMEvent(vm, "info", fmt.Sprintf("Adopted from %s by %s", target.Name, actorName))
		// Fill in what the listing didn't carry (guest IP on Proxmox, etc.).
		if synced, err := s.SyncState(ctx, vm.ID); err == nil && synced != nil {
			vm = synced
		} else if err != nil {
			slog.Warn("adopt: initial sync failed", "vm_id", vm.ID, "error", err)
		}
		res.Adopted = append(res.Adopted, *vm)
	}
	if len(unignore) > 0 {
		if err := s.db.UnignoreVMs(targetID, unignore); err != nil {
			slog.Warn("adopt: could not clear ignore list entries", "target_id", targetID, "error", err)
		}
	}
	// Refresh the unmanaged count without another hypervisor call.
	ignored, _ := s.ignoredRefs(targetID)
	unmanaged := 0
	for _, vm := range listed {
		if _, ig := ignored[vm.ID]; !managedRefs[vm.ID] && !ig {
			unmanaged++
		}
	}
	if err := s.db.UpdateTargetUnmanaged(targetID, unmanaged); err != nil {
		slog.Warn("adopt: could not record unmanaged count", "target_id", targetID, "error", err)
	}
	return res, nil
}

// IgnoreDiscovered hides refs from Discover for this target. names is
// optional display metadata keyed by ref.
func (s *VMService) IgnoreDiscovered(targetID int64, refs []string, names map[string]string, actorID int64) error {
	if _, err := s.targets.Get(targetID); err != nil {
		return fmt.Errorf("%w: id %d", ErrTargetNotFound, targetID)
	}
	if len(refs) == 0 {
		return fmt.Errorf("%w: no VM refs given", ErrInvalidNICSpec)
	}
	m := map[string]string{}
	for _, r := range refs {
		if r = strings.TrimSpace(r); r != "" {
			m[r] = names[r]
		}
	}
	if err := s.db.IgnoreVMs(targetID, m, &actorID); err != nil {
		return err
	}
	s.adjustUnmanaged(targetID, -len(m))
	return nil
}

// UnignoreDiscovered brings refs back into Discover.
func (s *VMService) UnignoreDiscovered(targetID int64, refs []string) error {
	if _, err := s.targets.Get(targetID); err != nil {
		return fmt.Errorf("%w: id %d", ErrTargetNotFound, targetID)
	}
	before, _ := s.ignoredRefs(targetID)
	removed := 0
	for _, r := range refs {
		if _, ok := before[strings.TrimSpace(r)]; ok {
			removed++
		}
	}
	if err := s.db.UnignoreVMs(targetID, refs); err != nil {
		return err
	}
	s.adjustUnmanaged(targetID, removed)
	return nil
}

// ListIgnored returns the ignore list for a target.
func (s *VMService) ListIgnored(targetID int64) ([]models.IgnoredVM, error) {
	if _, err := s.targets.Get(targetID); err != nil {
		return nil, fmt.Errorf("%w: id %d", ErrTargetNotFound, targetID)
	}
	return s.db.ListIgnoredVMs(targetID)
}

func (s *VMService) managedRefs(targetID int64) (map[string]bool, error) {
	vms, err := s.db.ListManagedVMs()
	if err != nil {
		return nil, fmt.Errorf("list managed VMs: %w", err)
	}
	refs := map[string]bool{}
	for _, vm := range vms {
		if vm.TargetID == targetID {
			refs[vm.VMRef] = true
		}
	}
	return refs, nil
}

func (s *VMService) ignoredRefs(targetID int64) (map[string]models.IgnoredVM, error) {
	list, err := s.db.ListIgnoredVMs(targetID)
	if err != nil {
		return nil, fmt.Errorf("list ignored VMs: %w", err)
	}
	m := map[string]models.IgnoredVM{}
	for _, i := range list {
		m[i.VMRef] = i
	}
	return m, nil
}

// adjustUnmanaged nudges the stored count after an ignore/unignore so the
// Targets page is right without waiting for the next sync (which recomputes
// it exactly). Never below zero.
func (s *VMService) adjustUnmanaged(targetID int64, delta int) {
	t, err := s.targets.Get(targetID)
	if err != nil {
		return
	}
	n := t.UnmanagedVMs + delta
	if n < 0 {
		n = 0
	}
	if err := s.db.UpdateTargetUnmanaged(targetID, n); err != nil {
		slog.Warn("could not adjust unmanaged count", "target_id", targetID, "error", err)
	}
}
