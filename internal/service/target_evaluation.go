package service

import (
	"fmt"
	"log/slog"
	"time"
)

// Evaluation reminders. A host on an evaluation license works fully until
// the day it doesn't; from then on it is inventory-only. The connection test
// records when the evaluation ends; this sweep turns that into a warning the
// operator sees in time: at 14, 7 and 1 days before, and when it expires.

// evaluationStages are the reminder thresholds in days left, descending.
var evaluationStages = []int{14, 7, 1, 0}

// evaluationStage maps days left to the reminder that applies: 14, 7, 1 or
// 0 (expired); -1 when nothing is due yet.
func evaluationStage(daysLeft int) int {
	stage := -1
	for _, s := range evaluationStages {
		if daysLeft <= s {
			stage = s
		}
	}
	return stage
}

// daysUntil rounds up: 1 hour left is "1 day", so the last reminder fires
// before, not after, the host flips.
func daysUntil(t, now time.Time) int {
	d := t.Sub(now)
	if d <= 0 {
		return 0
	}
	return int((d + 24*time.Hour - time.Nanosecond) / (24 * time.Hour))
}

// SweepEvaluations sends due reminders for every target with an evaluation
// expiry recorded. Each stage is sent once; a changed expiry (new key, new
// evaluation) restarts the ladder (see UpdateTargetCapabilities). Returns
// how many reminders went out.
func (s *TargetService) SweepEvaluations(now time.Time) int {
	targets, err := s.db.ListTargets()
	if err != nil {
		slog.Warn("evaluation sweep: list targets failed", "error", err)
		return 0
	}
	sent := 0
	for _, t := range targets {
		if t.EvaluationExpiresAt == nil {
			continue
		}
		days := daysUntil(*t.EvaluationExpiresAt, now)
		stage := evaluationStage(days)
		if stage < 0 || stage == t.EvaluationWarnedStage || (t.EvaluationWarnedStage >= 0 && stage > t.EvaluationWarnedStage) {
			continue
		}
		s.notifyEvaluation(t.ID, t.Name, *t.EvaluationExpiresAt, days, stage)
		if err := s.db.UpdateTargetEvaluationWarnedStage(t.ID, stage); err != nil {
			slog.Warn("evaluation sweep: could not record warning stage", "target_id", t.ID, "error", err)
		}
		sent++
	}
	return sent
}

func (s *TargetService) notifyEvaluation(targetID int64, name string, expiresAt time.Time, days, stage int) {
	date := expiresAt.Local().Format("2 Jan 2006")
	var level, title, body, event string
	if stage == 0 {
		level, event = "error", "target.evaluation_expired"
		title = fmt.Sprintf("Evaluation expired on %s", name)
		body = fmt.Sprintf("The evaluation license on %s ended on %s. The host is now inventory-only: Forgemill can read it, but cannot deploy, power, reconfigure, snapshot or destroy VMs there until a paid or VMUG key is assigned.", name, date)
	} else {
		level, event = "warning", "target.evaluation_expiring"
		if stage == 1 {
			level = "error"
		}
		title = fmt.Sprintf("Evaluation on %s expires in %d day%s", name, days, plural(days))
		body = fmt.Sprintf("The evaluation license on %s ends on %s. Assign a paid or VMUG key before then — after that date the host becomes inventory-only (no deploy, power, reconfigure, snapshot or destroy).", name, date)
	}
	slog.Warn("evaluation reminder", "target_id", targetID, "name", name, "days_left", days, "stage", stage)
	if s.notifier != nil {
		s.notifier.EmitForAdmins(level, title, body, "/targets", event)
	}
	if s.webhooks != nil {
		s.webhooks.FireTemplateEvent(event, map[string]interface{}{
			"target_id": targetID, "target_name": name, "expires_at": expiresAt.UTC().Format(time.RFC3339), "days_left": days, "message": body,
		})
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
