package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/forgemill/forgemill/internal/crypto"
	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/factory"
)

// FactoryService manages template build operations.
type FactoryService struct {
	db      *db.DB
	engine  *factory.Engine
	enc     *crypto.Encryptor
	checker *factory.ISOUpdateChecker
	hooksMu sync.RWMutex
	hooks   *WebhookService
	notifier *NotificationService
	syncTemplates func(ctx context.Context, targetID int64)
}

// SetNotificationService wires the in-app notification service. Optional.
func (s *FactoryService) SetNotificationService(n *NotificationService) {
	s.hooksMu.Lock()
	defer s.hooksMu.Unlock()
	s.notifier = n
}

func (s *FactoryService) getNotifier() *NotificationService {
	s.hooksMu.RLock()
	defer s.hooksMu.RUnlock()
	return s.notifier
}

// SetTemplateSyncCallback sets a function to call after a build completes to sync templates.
func (s *FactoryService) SetTemplateSyncCallback(fn func(ctx context.Context, targetID int64)) {
	s.syncTemplates = fn
}

// NewFactoryService creates a new FactoryService.
func NewFactoryService(database *db.DB, engine *factory.Engine, enc *crypto.Encryptor) *FactoryService {
	svc := &FactoryService{
		db:      database,
		engine:  engine,
		enc:     enc,
		checker: factory.NewISOUpdateChecker(database),
	}

	// Set up lineage callback for automatic linking after build completion
	engine.SetBuildCompleteCallback(svc.onBuildComplete)

	return svc
}

// SetWebhookService sets the webhook service for firing template events.
func (s *FactoryService) SetWebhookService(hooks *WebhookService) {
	s.hooksMu.Lock()
	defer s.hooksMu.Unlock()
	s.hooks = hooks
}

// getHooks returns the current webhook service, safe for concurrent access.
func (s *FactoryService) getHooks() *WebhookService {
	s.hooksMu.RLock()
	defer s.hooksMu.RUnlock()
	return s.hooks
}

// ListOSDefinitions returns all available OS definitions.
func (s *FactoryService) ListOSDefinitions() []factory.OSDefinition {
	return factory.ListDefinitions()
}

// GetOSDefinition returns a specific OS definition.
func (s *FactoryService) GetOSDefinition(id string) (*factory.OSDefinition, error) {
	def := factory.GetDefinition(id)
	if def == nil {
		return nil, fmt.Errorf("OS definition not found: %s", id)
	}
	return def, nil
}

// StartBuild creates a new template build and starts execution.
func (s *FactoryService) StartBuild(osDefID string, targetID int64, cfg factory.BuildConfig, userID int64) (*models.TemplateBuild, error) {
	osDef := factory.GetDefinition(osDefID)
	if osDef == nil {
		return nil, fmt.Errorf("unknown OS definition: %s", osDefID)
	}

	if cfg.CPU < osDef.MinCPU {
		cfg.CPU = osDef.MinCPU
	}
	if cfg.MemoryMB < osDef.MinMemoryMB {
		cfg.MemoryMB = osDef.MinMemoryMB
	}
	if cfg.DiskGB < osDef.MinDiskGB {
		cfg.DiskGB = osDef.MinDiskGB
	}

	target, err := s.db.GetTarget(targetID)
	if err != nil {
		return nil, fmt.Errorf("get target: %w", err)
	}

	// Populate target-specific fields from target config if not provided in request
	if cfg.StoragePool == "" && target.StoragePool != "" {
		cfg.StoragePool = target.StoragePool
	}
	if cfg.Bridge == "" && target.NetworkBridge != "" {
		cfg.Bridge = target.NetworkBridge
	}
	if cfg.Datacenter == "" && target.Datacenter != "" {
		cfg.Datacenter = target.Datacenter
	}
	if cfg.Datastore == "" && target.Datastore != "" {
		cfg.Datastore = target.Datastore
	}
	if cfg.Network == "" && target.Network != "" {
		cfg.Network = target.Network
	}

	password, err := s.enc.Decrypt(target.PasswordEncrypt)
	if err != nil {
		return nil, fmt.Errorf("decrypt target password: %w", err)
	}

	// Get or create family by OS definition + target (the canonical key).
	// Family identity is (os_definition_id, target_id), not template name.
	family, err := s.db.GetOrCreateFamilyByOS(osDefID, targetID)
	if err != nil {
		return nil, fmt.Errorf("get or create template family: %w", err)
	}

	// Derive next version from actual templates in the family, not the cached
	// latest_version counter. This stays correct even when templates are deleted.
	actualMax, err := s.db.GetMaxVersionInFamily(family.ID)
	if err != nil {
		return nil, fmt.Errorf("get max version in family: %w", err)
	}
	version := actualMax + 1

	// Auto-generate template name: fm-{os_id}-{target_name}-v{version}
	physicalTemplateName := fmt.Sprintf("fm-%s-%s-v%d", osDefID, target.Name, version)
	cfg.TemplateName = physicalTemplateName

	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}

	build := &models.TemplateBuild{
		OSDefinitionID: osDefID,
		TargetID:       targetID,
		Status:         "pending",
		TemplateName:   physicalTemplateName,
		ConfigJSON:     string(configJSON),
		CreatedBy:      userID,
		Version:        version,
	}

	if err := s.db.CreateTemplateBuild(build); err != nil {
		return nil, fmt.Errorf("create build record: %w", err)
	}

	if err := s.engine.RunBuild(build.ID, target.Type, target.Hostname, target.Port, target.Username, password, target.ValidateCerts, osDef, cfg); err != nil {
		s.db.UpdateBuildStatus(build.ID, "failed", err.Error())
		return nil, fmt.Errorf("start build: %w", err)
	}

	slog.Info("template build started", "build_id", build.ID, "os", osDefID, "target", target.Name, "version", version, "family_id", family.ID)
	return build, nil
}

// GetBuild returns a template build by ID.
func (s *FactoryService) GetBuild(id int64) (*models.TemplateBuild, error) {
	return s.db.GetTemplateBuild(id)
}

// ListBuilds returns all template builds.
func (s *FactoryService) ListBuilds() ([]models.TemplateBuild, error) {
	return s.db.ListTemplateBuilds()
}

// CancelBuild cancels a running build.
func (s *FactoryService) CancelBuild(id int64) error {
	build, err := s.db.GetTemplateBuild(id)
	if err != nil {
		return fmt.Errorf("get build: %w", err)
	}
	if build.Status != "building" && build.Status != "downloading" && build.Status != "pending" {
		return fmt.Errorf("build is not currently running (status: %s)", build.Status)
	}
	if s.engine.IsRunningBuild(id) {
		s.engine.CancelBuild()
	} else {
		// Orphaned build — just update DB directly
		s.db.UpdateBuildStatus(id, "cancelled", "cancelled by user (orphaned build)")
	}
	return nil
}

// DeleteBuild deletes a build record.
func (s *FactoryService) DeleteBuild(id int64) error {
	build, err := s.db.GetTemplateBuild(id)
	if err != nil {
		return fmt.Errorf("get build: %w", err)
	}

	if build.Status == "building" || build.Status == "downloading" {
		return fmt.Errorf("cannot delete a running build")
	}

	return s.db.DeleteTemplateBuild(id)
}

// CheckPrerequisites verifies that required tools are installed.
func (s *FactoryService) CheckPrerequisites() *factory.PrereqStatus {
	return factory.CheckPrerequisites()
}

// IsRunning reports whether a build is currently executing.
func (s *FactoryService) IsRunning() bool {
	return s.engine.IsRunning()
}

// --- Phase 5: Update checking ---

// CheckAllUpdates checks all managed templates for available ISO updates.
func (s *FactoryService) CheckAllUpdates() ([]models.UpdateAvailable, error) {
	return s.checker.CheckAllForUpdates()
}

// CheckTemplateUpdate checks a specific template for ISO updates.
func (s *FactoryService) CheckTemplateUpdate(templateID int64) (*models.UpdateAvailable, error) {
	return s.checker.CheckTemplateForUpdate(templateID)
}

// RebuildTemplate triggers a rebuild of a managed template with the latest ISO.
func (s *FactoryService) RebuildTemplate(templateID int64, userID int64) (*models.TemplateBuild, error) {
	tmpl, err := s.db.GetTemplate(templateID)
	if err != nil {
		return nil, fmt.Errorf("get template: %w", err)
	}

	if !tmpl.ManagedByForgemill || tmpl.BuildID == nil {
		return nil, fmt.Errorf("template is not managed by forgemill")
	}

	if tmpl.FamilyID == nil {
		return nil, fmt.Errorf("template has no family_id - cannot rebuild")
	}

	family, err := s.db.GetFamily(*tmpl.FamilyID)
	if err != nil {
		return nil, fmt.Errorf("get template family: %w", err)
	}

	lastBuild, err := s.db.GetTemplateBuild(*tmpl.BuildID)
	if err != nil {
		return nil, fmt.Errorf("get last build: %w", err)
	}

	var cfg factory.BuildConfig
	if err := json.Unmarshal([]byte(lastBuild.ConfigJSON), &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal build config: %w", err)
	}

	// Derive next version from actual templates, not the cached counter.
	actualMax, err := s.db.GetMaxVersionInFamily(family.ID)
	if err != nil {
		return nil, fmt.Errorf("get max version in family: %w", err)
	}
	nextVersion := actualMax + 1
	cfg.TemplateName = fmt.Sprintf("%s-v%d", family.BaseName, nextVersion)

	osDef := factory.GetDefinition(lastBuild.OSDefinitionID)
	if osDef == nil {
		return nil, fmt.Errorf("unknown OS definition: %s", lastBuild.OSDefinitionID)
	}

	target, err := s.db.GetTarget(tmpl.TargetID)
	if err != nil {
		return nil, fmt.Errorf("get target: %w", err)
	}

	password, err := s.enc.Decrypt(target.PasswordEncrypt)
	if err != nil {
		return nil, fmt.Errorf("decrypt target password: %w", err)
	}

	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}

	build := &models.TemplateBuild{
		OSDefinitionID:  lastBuild.OSDefinitionID,
		TargetID:        tmpl.TargetID,
		Status:          "pending",
		TemplateName:    cfg.TemplateName,
		ConfigJSON:      string(configJSON),
		CreatedBy:       userID,
		Version:         nextVersion,
		PreviousBuildID: &lastBuild.ID,
	}

	if err := s.db.CreateTemplateBuild(build); err != nil {
		return nil, fmt.Errorf("create build record: %w", err)
	}

	if err := s.engine.RunBuild(build.ID, target.Type, target.Hostname, target.Port, target.Username, password, target.ValidateCerts, osDef, cfg); err != nil {
		s.db.UpdateBuildStatus(build.ID, "failed", err.Error())
		return nil, fmt.Errorf("start build: %w", err)
	}

	if hooks := s.getHooks(); hooks != nil {
		hooks.FireTemplateEvent("template.rebuild_started", map[string]interface{}{
			"template_id":   tmpl.ID,
			"template_name": tmpl.Name,
			"build_id":      build.ID,
			"version":       nextVersion,
		})
	}

	slog.Info("template rebuild started", "build_id", build.ID, "template", tmpl.Name, "version", nextVersion)
	return build, nil
}

// --- Phase 5: Schedules ---

// ListSchedules returns all template schedules.
func (s *FactoryService) ListSchedules() ([]models.TemplateSchedule, error) {
	return s.db.ListTemplateSchedules()
}

// GetSchedule returns a schedule by ID.
func (s *FactoryService) GetSchedule(id int64) (*models.TemplateSchedule, error) {
	return s.db.GetTemplateSchedule(id)
}

// CreateSchedule creates a new template schedule.
func (s *FactoryService) CreateSchedule(sched *models.TemplateSchedule) error {
	return s.db.CreateTemplateSchedule(sched)
}

// UpdateSchedule updates an existing schedule.
func (s *FactoryService) UpdateSchedule(sched *models.TemplateSchedule) error {
	return s.db.UpdateTemplateSchedule(sched)
}

// DeleteSchedule deletes a schedule.
func (s *FactoryService) DeleteSchedule(id int64) error {
	return s.db.DeleteTemplateSchedule(id)
}

// --- Phase 5: Template History ---

// GetTemplateHistory returns version history for a template.
func (s *FactoryService) GetTemplateHistory(templateID int64) ([]models.TemplateHistory, error) {
	return s.db.GetTemplateHistory(templateID)
}

// CleanupSuperseded deletes superseded template versions for a template.
func (s *FactoryService) CleanupSuperseded(templateID int64) (int64, error) {
	return s.db.DeleteSupersededTemplates(templateID)
}

// --- Phase 5: Build completion callback ---

func (s *FactoryService) onBuildComplete(buildID int64, templateName string) {
	build, err := s.db.GetTemplateBuild(buildID)
	if err != nil {
		slog.Error("failed to get build for lineage linking", "build_id", buildID, "error", err)
		return
	}

	// Auto-sync templates from the target so the new template appears immediately.
	// Retry a few times because the hypervisor may not list the new template immediately.
	var tmpl *models.Template
	for attempt := 1; attempt <= 3; attempt++ {
		if s.syncTemplates != nil {
			slog.Info("auto-syncing templates after build completion", "build_id", buildID, "target_id", build.TargetID, "attempt", attempt)
			s.syncTemplates(context.Background(), build.TargetID)
		}

		tmpl, err = s.db.GetTemplateByName(templateName, build.TargetID)
		if err == nil {
			break
		}
		slog.Warn("template not found after sync, retrying", "template_name", templateName, "target_id", build.TargetID, "attempt", attempt, "error", err)
		if attempt < 3 {
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}
	if tmpl == nil {
		slog.Error("template not found after all sync attempts — lineage link skipped", "build_id", buildID, "template_name", templateName, "target_id", build.TargetID)
		return
	}

	version := build.Version
	if version == 0 {
		version = 1
	}

	isoChecksum := build.ISOChecksum

	if err := s.db.LinkBuildToTemplate(buildID, tmpl.ID, isoChecksum, version); err != nil {
		slog.Error("failed to link build to template", "build_id", buildID, "template_id", tmpl.ID, "error", err)
		return
	}

	// Update template family tracking (lookup by OS + target, not name)
	family, err := s.db.GetOrCreateFamilyByOS(build.OSDefinitionID, tmpl.TargetID)
	if err != nil {
		slog.Error("failed to get or create template family", "build_id", buildID, "template_id", tmpl.ID, "error", err)
	} else {
		// Use a transaction to update both template.family_id and family.latest_version
		tx, err := s.db.Begin()
		if err != nil {
			slog.Error("failed to begin transaction for family update", "error", err)
		} else {
			defer tx.Rollback()

			if _, err := tx.Exec(`UPDATE templates SET family_id = ? WHERE id = ?`, family.ID, tmpl.ID); err != nil {
				slog.Error("failed to set family_id on template", "template_id", tmpl.ID, "family_id", family.ID, "error", err)
			} else if _, err := tx.Exec(`UPDATE template_families SET latest_version = ? WHERE id = ?`, version, family.ID); err != nil {
				slog.Error("failed to update family latest version", "family_id", family.ID, "version", version, "error", err)
			} else if err := tx.Commit(); err != nil {
				slog.Error("failed to commit family update transaction", "template_id", tmpl.ID, "family_id", family.ID, "error", err)
			} else {
				slog.Info("updated template family", "template_id", tmpl.ID, "family_id", family.ID, "version", version)
			}
		}
	}

	// Supersede every other active template in this family. This runs for both
	// fresh StartBuild and RebuildTemplate — family membership is the source of
	// truth, not build lineage via PreviousBuildID. This prevents multiple
	// "active" templates accumulating in the same family.
	if family != nil {
		if err := s.db.SupersedeAllActiveInFamily(family.ID, tmpl.ID); err != nil {
			slog.Error("failed to supersede old templates in family", "family_id", family.ID, "new_template_id", tmpl.ID, "error", err)
		} else {
			if hooks := s.getHooks(); hooks != nil {
				hooks.FireTemplateEvent("template.version_superseded", map[string]interface{}{
					"new_template_id": tmpl.ID,
					"family_id":       family.ID,
					"version":         version,
				})
			}
		}
	}

	if hooks := s.getHooks(); hooks != nil {
		hooks.FireTemplateEvent("template.rebuild_completed", map[string]interface{}{
			"template_id":   tmpl.ID,
			"template_name": tmpl.Name,
			"build_id":      buildID,
			"version":       version,
		})
	}
	if notifier := s.getNotifier(); notifier != nil && build != nil {
		notifier.NotifyTemplateBuildCompleted(build.CreatedBy, tmpl.Name, buildID, true)
	}

	slog.Info("template lineage linked", "build_id", buildID, "template_id", tmpl.ID, "version", version)
}

// --- Template Families ---

// ListTemplateFamilies returns all template families.
func (s *FactoryService) ListTemplateFamilies() ([]models.TemplateFamily, error) {
	return s.db.ListTemplateFamilies()
}

// GetTemplatesByFamily returns all templates in a family, ordered by version descending.
func (s *FactoryService) GetTemplatesByFamily(familyID int64) ([]models.Template, error) {
	return s.db.GetTemplatesByFamily(familyID)
}

// GetUpdateChecker returns the ISO update checker (for use by the scheduler).
func (s *FactoryService) GetUpdateChecker() *factory.ISOUpdateChecker {
	return s.checker
}

// GetEngine returns the build engine (for use by the scheduler).
func (s *FactoryService) GetEngine() *factory.Engine {
	return s.engine
}
