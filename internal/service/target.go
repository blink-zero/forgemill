package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/forgemill/forgemill/internal/crypto"
	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/provider"
)

type TargetService struct {
	db  *db.DB
	enc *crypto.Encryptor
}

func NewTargetService(db *db.DB, enc *crypto.Encryptor) *TargetService {
	return &TargetService{db: db, enc: enc}
}

func (s *TargetService) Create(t *models.Target, rawPassword string) error {
	encrypted, err := s.enc.Encrypt(rawPassword)
	if err != nil {
		return fmt.Errorf("encrypt password: %w", err)
	}
	t.PasswordEncrypt = encrypted
	if t.Port == 0 {
		t.Port = 443
	}
	if t.Status == "" {
		t.Status = "unknown"
	}
	return s.db.CreateTarget(t)
}

func (s *TargetService) Get(id int64) (*models.Target, error) {
	return s.db.GetTarget(id)
}

func (s *TargetService) List() ([]models.Target, error) {
	return s.db.ListTargets()
}

func (s *TargetService) Update(t *models.Target, rawPassword string) error {
	if rawPassword != "" {
		encrypted, err := s.enc.Encrypt(rawPassword)
		if err != nil {
			return fmt.Errorf("encrypt password: %w", err)
		}
		t.PasswordEncrypt = encrypted
	}
	return s.db.UpdateTarget(t)
}

func (s *TargetService) DeletePreview(id int64) (*db.DeleteTargetPreviewResult, error) {
	return s.db.DeleteTargetPreview(id)
}

func (s *TargetService) Delete(id int64) error {
	// DeleteTarget handles full cascade (executions, VMs, deployments, templates, builds)
	return s.db.DeleteTarget(id)
}

func (s *TargetService) TestConnection(ctx context.Context, id int64) error {
	p, err := s.getProvider(id)
	if err != nil {
		s.db.UpdateTargetStatus(id, "error")
		return err
	}
	if err := p.TestConnection(ctx); err != nil {
		s.db.UpdateTargetStatus(id, "error")
		return err
	}
	s.db.UpdateTargetStatus(id, "connected")
	s.refreshCapabilities(ctx, id, p)
	return nil
}

// refreshCapabilities asks a provider that can report it whether the host
// accepts writes (free-licensed ESXi does not) and records the answer on the
// target. Best effort: a host that cannot be asked keeps its last answer.
func (s *TargetService) refreshCapabilities(ctx context.Context, id int64, p provider.Provider) {
	reporter, ok := p.(provider.CapabilityReporter)
	if !ok {
		return
	}
	caps, err := reporter.HostCapabilities(ctx)
	if err != nil {
		slog.Warn("could not read target capabilities", "target_id", id, "error", err)
		return
	}
	if err := s.db.UpdateTargetCapabilities(id, caps.LicenseEdition, caps.WritesAllowed, caps.Note); err != nil {
		slog.Warn("could not store target capabilities", "target_id", id, "error", err)
		return
	}
	if !caps.WritesAllowed {
		slog.Warn("target is inventory-only: hypervisor license prohibits API writes", "target_id", id, "license", caps.LicenseEdition)
	}
}

func (s *TargetService) SyncTemplates(ctx context.Context, id int64) (int, error) {
	p, err := s.getProvider(id)
	if err != nil {
		return 0, err
	}
	defer p.Disconnect()

	if err := p.Connect(ctx); err != nil {
		return 0, fmt.Errorf("connect: %w", err)
	}

	templates, err := p.ListTemplates(ctx)
	if err != nil {
		return 0, fmt.Errorf("list templates: %w", err)
	}

	// BUG-01: Use upsert logic to preserve Factory-managed template metadata
	// (build_id, version, lifecycle_status, superseded_by, iso_checksum, etc.)
	// instead of DELETE + re-INSERT which destroys lineage data and can cause
	// FK violations from deployments referencing template IDs.
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Only delete templates that are NOT managed by Forgemill Factory
	if _, err := tx.Exec(`DELETE FROM templates WHERE target_id = ? AND (managed_by_forgemill = FALSE OR managed_by_forgemill IS NULL)`, id); err != nil {
		return 0, fmt.Errorf("clear unmanaged templates: %w", err)
	}

	now := time.Now().UTC()
	for _, t := range templates {
		// Upsert: update existing templates (matched by target_id + moref) or insert new ones.
		// This preserves IDs and Factory metadata for managed templates.
		_, err := tx.Exec(
			`INSERT INTO templates (target_id, name, moref, os_type, guest_id, cpu, memory_mb, disk_gb, icon, last_synced_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(target_id, moref) DO UPDATE SET
				name = excluded.name,
				os_type = excluded.os_type,
				guest_id = excluded.guest_id,
				cpu = excluded.cpu,
				memory_mb = excluded.memory_mb,
				disk_gb = excluded.disk_gb,
				icon = excluded.icon,
				last_synced_at = excluded.last_synced_at`,
			id, t.Name, t.Moref, t.OSType, t.GuestID, t.CPU, t.MemoryMB, t.DiskGB, guessIcon(t.OSType), now,
		)
		if err != nil {
			return 0, fmt.Errorf("upsert template %s: %w", t.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit transaction: %w", err)
	}

	s.db.UpdateTargetStatus(id, "connected")
	s.refreshCapabilities(ctx, id, p)
	return len(templates), nil
}

func (s *TargetService) GetResources(ctx context.Context, id int64) (*provider.Resources, error) {
	p, err := s.getProvider(id)
	if err != nil {
		return nil, err
	}
	defer p.Disconnect()

	if err := p.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return p.GetResources(ctx)
}

func (s *TargetService) GetProvider(id int64) (provider.Provider, error) {
	return s.getProvider(id)
}

func (s *TargetService) getProvider(id int64) (provider.Provider, error) {
	target, err := s.db.GetTarget(id)
	if err != nil {
		return nil, fmt.Errorf("get target: %w", err)
	}

	password, err := s.enc.Decrypt(target.PasswordEncrypt)
	if err != nil {
		return nil, fmt.Errorf("decrypt password: %w", err)
	}

	// Try the provider registry first (modular approach for new hypervisors).
	// Providers register themselves via init() in their respective packages.
	// See: internal/provider/vmware/client.go, internal/provider/proxmox/client.go
	if factory := provider.GetProviderFactory(target.Type); factory != nil {
		p := factory(target.Hostname, target.Port, target.Username, password, target.ValidateCerts)
		// Providers that SSH to the hypervisor (Proxmox snippet uploads) get
		// trust-on-first-use host-key verification backed by the targets
		// table. Until this was wired here, only a dead fallback path ever
		// called SetTOFU and those connections accepted any host key (#186).
		if t, ok := p.(provider.HostKeyTrusting); ok {
			t.SetTOFU(target.ID, s.db)
		}
		return p, nil
	}

	// Both in-tree providers register themselves in init(); an unknown type
	// here means a row with a type this build doesn't ship a provider for.
	return nil, fmt.Errorf("unsupported target type: %s", target.Type)
}

func guessIcon(osType string) string {
	switch osType {
	case "windows":
		return "windows"
	default:
		return "linux"
	}
}
