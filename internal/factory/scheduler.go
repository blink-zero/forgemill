package factory

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/forgemill/forgemill/internal/db/models"
)

// ScheduleStore is the interface the scheduler uses to manage schedules and builds.
type ScheduleStore interface {
	UpdateStore
	ListDueSchedules() ([]models.TemplateSchedule, error)
	UpdateScheduleChecked(id int64, nextCheckAt time.Time) error
	UpdateScheduleRebuilt(id int64, nextCheckAt time.Time) error
	CreateTemplateBuild(b *models.TemplateBuild) error
	UpdateBuildAutoTriggered(id int64, previousBuildID *int64) error
}

// WebhookFirer is the interface for sending webhook events.
type WebhookFirer interface {
	FireTemplateEvent(event string, data map[string]interface{})
}

// NotificationEmitter allows the scheduler to post in-app notifications.
type NotificationEmitter interface {
	NotifyTemplateUpdateAvailable(templateID int64, templateName string)
}

// BuildScheduler manages periodic checks and auto-rebuilds.
// RebuildFunc is a callback that triggers an actual build execution for a template.
type RebuildFunc func(templateID int64, userID int64) error

type BuildScheduler struct {
	store       ScheduleStore
	engine      *Engine
	checker     *ISOUpdateChecker
	hooks       WebhookFirer
	notifier    NotificationEmitter
	rebuildFunc RebuildFunc
	ticker      *time.Ticker
	stopCh      chan struct{}
	mu          sync.Mutex
	running     bool
}

// NewBuildScheduler creates a new scheduler.
func NewBuildScheduler(store ScheduleStore, engine *Engine, checker *ISOUpdateChecker, hooks WebhookFirer) *BuildScheduler {
	return &BuildScheduler{
		store:   store,
		engine:  engine,
		checker: checker,
		hooks:   hooks,
		stopCh:  make(chan struct{}),
	}
}

// SetNotificationEmitter wires in-app notifications. Optional.
func (s *BuildScheduler) SetNotificationEmitter(n NotificationEmitter) {
	s.notifier = n
}

// SetRebuildFunc sets the callback used to trigger builds with full target credential resolution.
// B-5: Guarded by mutex to prevent data race with triggerRebuild reading rebuildFunc.
func (s *BuildScheduler) SetRebuildFunc(fn RebuildFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rebuildFunc = fn
}

// Start begins the scheduler loop, checking every hour.
func (s *BuildScheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		slog.Warn("scheduler already running, ignoring duplicate Start")
		return
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.ticker = time.NewTicker(1 * time.Hour)
	s.mu.Unlock()

	slog.Info("template build scheduler started")

	go func() {
		// Do an initial check after a short delay, but allow shutdown to interrupt
		initialDelay := time.NewTimer(30 * time.Second)
		select {
		case <-initialDelay.C:
			s.processSchedules()
		case <-s.stopCh:
			initialDelay.Stop()
			s.ticker.Stop()
			slog.Info("template build scheduler stopped")
			return
		}

		for {
			select {
			case <-s.ticker.C:
				s.processSchedules()
			case <-s.stopCh:
				s.ticker.Stop()
				slog.Info("template build scheduler stopped")
				return
			}
		}
	}()
}

// Stop gracefully stops the scheduler.
func (s *BuildScheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		slog.Warn("scheduler not running, ignoring duplicate Stop")
		return
	}
	s.running = false
	close(s.stopCh)
}

func (s *BuildScheduler) processSchedules() {
	schedules, err := s.store.ListDueSchedules()
	if err != nil {
		slog.Error("failed to list due schedules", "error", err)
		return
	}

	if len(schedules) == 0 {
		return
	}

	slog.Debug("processing template schedules", "count", len(schedules))

	for _, sched := range schedules {
		s.processOneSchedule(sched)
	}
}

func (s *BuildScheduler) processOneSchedule(sched models.TemplateSchedule) {
	nextCheck := time.Now().Add(time.Duration(sched.CheckIntervalHours) * time.Hour)
	needsRebuild := false

	switch sched.Strategy {
	case "interval":
		if sched.LastRebuiltAt == nil || time.Since(*sched.LastRebuiltAt) >= time.Duration(sched.IntervalDays)*24*time.Hour {
			needsRebuild = true
		}

	case "on_update":
		tmpl, err := s.store.GetTemplate(sched.TemplateID)
		if err != nil {
			slog.Warn("failed to get template for schedule", "schedule_id", sched.ID, "error", err)
			s.store.UpdateScheduleChecked(sched.ID, nextCheck)
			return
		}

		update, hasUpdate, err := s.checkTemplateUpdate(tmpl)
		if err != nil {
			slog.Warn("failed to check for update", "schedule_id", sched.ID, "error", err)
			s.store.UpdateScheduleChecked(sched.ID, nextCheck)
			return
		}
		if hasUpdate {
			needsRebuild = true
			if s.hooks != nil {
				s.hooks.FireTemplateEvent("template.update_available", map[string]interface{}{
					"template_id":   tmpl.ID,
					"template_name": tmpl.Name,
					"new_checksum":  update.LatestChecksum,
				})
			}
			if s.notifier != nil {
				s.notifier.NotifyTemplateUpdateAvailable(tmpl.ID, tmpl.Name)
			}
		}

	case "both":
		// Check interval first
		if sched.LastRebuiltAt == nil || time.Since(*sched.LastRebuiltAt) >= time.Duration(sched.IntervalDays)*24*time.Hour {
			needsRebuild = true
		}
		// Also check for updates
		if !needsRebuild {
			tmpl, err := s.store.GetTemplate(sched.TemplateID)
			if err == nil {
				update, hasUpdate, err := s.checkTemplateUpdate(tmpl)
				if err == nil && hasUpdate {
					needsRebuild = true
					if s.hooks != nil {
						s.hooks.FireTemplateEvent("template.update_available", map[string]interface{}{
							"template_id":   tmpl.ID,
							"template_name": tmpl.Name,
							"new_checksum":  update.LatestChecksum,
						})
					}
					if s.notifier != nil {
						s.notifier.NotifyTemplateUpdateAvailable(tmpl.ID, tmpl.Name)
					}
				}
			}
		}
	}

	if needsRebuild {
		if err := s.triggerRebuild(sched); err != nil {
			slog.Error("failed to trigger scheduled rebuild", "schedule_id", sched.ID, "error", err)
			s.store.UpdateScheduleChecked(sched.ID, nextCheck)
			return
		}
		s.store.UpdateScheduleRebuilt(sched.ID, nextCheck)
	} else {
		s.store.UpdateScheduleChecked(sched.ID, nextCheck)
	}
}

func (s *BuildScheduler) checkTemplateUpdate(tmpl *models.Template) (*models.UpdateAvailable, bool, error) {
	if tmpl.BuildID == nil || tmpl.ISOChecksum == "" {
		return nil, false, nil
	}

	build, err := s.store.GetTemplateBuild(*tmpl.BuildID)
	if err != nil {
		return nil, false, err
	}

	osDef := GetDefinition(build.OSDefinitionID)
	if osDef == nil {
		return nil, false, fmt.Errorf("unknown OS definition: %s", build.OSDefinitionID)
	}

	update, hasUpdate, err := s.checker.checkTemplate(*tmpl, build, osDef)
	if err != nil {
		return nil, false, err
	}
	return &update, hasUpdate, nil
}

func (s *BuildScheduler) triggerRebuild(sched models.TemplateSchedule) error {
	if s.engine.IsRunning() {
		return fmt.Errorf("a build is already running")
	}

	// B-5: Read rebuildFunc under mutex to prevent data race with SetRebuildFunc
	s.mu.Lock()
	fn := s.rebuildFunc
	s.mu.Unlock()

	if fn == nil {
		return fmt.Errorf("no rebuild function configured")
	}

	tmpl, err := s.store.GetTemplate(sched.TemplateID)
	if err != nil {
		return fmt.Errorf("get template: %w", err)
	}

	if tmpl.BuildID == nil {
		return fmt.Errorf("template has no linked build")
	}

	lastBuild, err := s.store.GetTemplateBuild(*tmpl.BuildID)
	if err != nil {
		return fmt.Errorf("get last build: %w", err)
	}

	// Use the rebuild function which has access to target credentials and runs the build
	if err := fn(sched.TemplateID, lastBuild.CreatedBy); err != nil {
		return fmt.Errorf("trigger rebuild: %w", err)
	}

	if s.hooks != nil {
		s.hooks.FireTemplateEvent("template.rebuild_started", map[string]interface{}{
			"template_id":   tmpl.ID,
			"template_name": tmpl.Name,
		})
	}

	slog.Info("scheduled rebuild triggered", "template", tmpl.Name)
	return nil
}

// BaseName strips version suffixes like "-v2" from a template name.
// INPUT NORMALISATION ONLY - do not use for version logic.
// Template families are now the source of truth for versioning.
func BaseName(name string) string {
	// Check if name ends with -vN pattern
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == 'v' && i > 0 && name[i-1] == '-' {
			// Verify the rest is digits
			allDigits := true
			for _, c := range name[i+1:] {
				if c < '0' || c > '9' {
					allDigits = false
					break
				}
			}
			if allDigits && len(name[i+1:]) > 0 {
				return name[:i-1]
			}
		}
	}
	return name
}
