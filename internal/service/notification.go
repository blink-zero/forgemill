package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
)

// NotificationService produces in-app notifications for end users.
// Intended to run alongside WebhookService — both consume the same event
// signals but deliver to different surfaces (webhooks out vs. bell drawer).
type NotificationService struct {
	db *db.DB
}

func NewNotificationService(database *db.DB) *NotificationService {
	return &NotificationService{db: database}
}

// StartRetentionCleanup runs a daily cleanup loop that deletes notifications
// that have been read for over 30 days or are older than 90 days. Call once
// at startup.
func (s *NotificationService) StartRetentionCleanup(ctx context.Context) {
	if s == nil || s.db == nil {
		return
	}
	go func() {
		tick := time.NewTicker(24 * time.Hour)
		defer tick.Stop()
		// Run once at startup so restarts don't skip a day.
		if n, err := s.db.DeleteOldReadNotifications(30); err == nil && n > 0 {
			slog.Info("notification retention: cleaned up", "deleted", n)
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if n, err := s.db.DeleteOldReadNotifications(30); err != nil {
					slog.Warn("notification retention cleanup failed", "error", err)
				} else if n > 0 {
					slog.Info("notification retention: cleaned up", "deleted", n)
				}
			}
		}
	}()
}

// Emit persists a notification. Non-fatal: errors are logged but never
// propagate — notifications are a side-effect of primary operations.
func (s *NotificationService) Emit(n *models.Notification) {
	if s == nil || s.db == nil {
		return
	}
	if n.Level == "" {
		n.Level = "info"
	}
	if err := s.db.CreateNotification(n); err != nil {
		slog.Warn("notification emit failed", "title", n.Title, "error", err)
	}
}

// EmitForUser is a convenience wrapper to send a targeted notification.
func (s *NotificationService) EmitForUser(userID int64, level, title, body, link, event string) {
	uid := userID
	s.Emit(&models.Notification{
		UserID: &uid,
		Level:  level,
		Title:  title,
		Body:   body,
		Link:   link,
		Event:  event,
	})
}

// EmitBroadcast sends a notification visible to every user.
func (s *NotificationService) EmitBroadcast(level, title, body, link, event string) {
	s.Emit(&models.Notification{
		Level: level,
		Title: title,
		Body:  body,
		Link:  link,
		Event: event,
	})
}

// EmitForAdmins sends a notification to every active admin.
func (s *NotificationService) EmitForAdmins(level, title, body, link, event string) {
	if s == nil || s.db == nil {
		return
	}
	ids, err := s.db.ListAdminUserIDs()
	if err != nil {
		slog.Warn("notification emit-for-admins: list admins failed", "error", err)
		return
	}
	for _, id := range ids {
		s.EmitForUser(id, level, title, body, link, event)
	}
}

// --- High-level helpers for specific events ---------------------------------

// NotifyDeployCompleted fires after a successful deployment.
func (s *NotificationService) NotifyDeployCompleted(d *models.Deployment) {
	if d == nil || s == nil {
		return
	}
	s.EmitForUser(d.CreatedBy, "success",
		fmt.Sprintf("Deployed %s", d.VMName),
		"",
		fmt.Sprintf("/history/%d", d.ID),
		"deploy.completed",
	)
}

// NotifyDeployFailed fires when a deployment errors out.
func (s *NotificationService) NotifyDeployFailed(d *models.Deployment) {
	if d == nil || s == nil {
		return
	}
	body := d.ErrorMessage
	if body == "" {
		body = "Deployment failed — check logs for details."
	}
	s.EmitForUser(d.CreatedBy, "error",
		fmt.Sprintf("Deploy failed: %s", d.VMName),
		body,
		fmt.Sprintf("/history/%d", d.ID),
		"deploy.failed",
	)
}

// NotifyExecutionCompleted fires when an action execution finishes.
// Skips cancelled executions — the user who cancelled already knows.
func (s *NotificationService) NotifyExecutionCompleted(exec *models.ActionExecution) {
	if exec == nil || s == nil {
		return
	}
	if exec.Status == "cancelled" {
		return
	}
	level := "success"
	title := fmt.Sprintf("Action completed: %s", exec.ActionName)
	if exec.Status == "failed" {
		level = "error"
		title = fmt.Sprintf("Action failed: %s", exec.ActionName)
	}
	s.EmitForUser(exec.CreatedBy, level, title, "",
		fmt.Sprintf("/vms/%d", exec.VMID),
		fmt.Sprintf("execution.%s", exec.Status),
	)
}

// NotifyTemplateBuildCompleted fires when a Template Factory build finishes.
func (s *NotificationService) NotifyTemplateBuildCompleted(userID int64, templateName string, buildID int64, success bool) {
	level := "success"
	title := fmt.Sprintf("Template built: %s", templateName)
	if !success {
		level = "error"
		title = fmt.Sprintf("Template build failed: %s", templateName)
	}
	s.EmitForUser(userID, level, title, "",
		fmt.Sprintf("/factory/build/%d", buildID),
		"template.build_completed",
	)
}

// NotifyTemplateUpdateAvailable is broadcast to admins when a template's
// upstream ISO checksum has changed.
func (s *NotificationService) NotifyTemplateUpdateAvailable(templateID int64, templateName string) {
	s.EmitForAdmins("info",
		fmt.Sprintf("Template update available: %s", templateName),
		"The upstream ISO checksum changed — rebuild to pick up the new version.",
		"/factory",
		"template.update_available",
	)
}
