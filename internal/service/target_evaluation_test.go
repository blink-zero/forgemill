package service

import (
	"testing"
	"time"

	"github.com/forgemill/forgemill/internal/db/models"
)

func TestEvaluationStageAndDays(t *testing.T) {
	for days, want := range map[int]int{30: -1, 15: -1, 14: 14, 10: 14, 7: 7, 2: 7, 1: 1, 0: 0} {
		if got := evaluationStage(days); got != want {
			t.Errorf("stage(%d) = %d, want %d", days, got, want)
		}
	}
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	for d, want := range map[time.Duration]int{36 * time.Hour: 2, 24 * time.Hour: 1, time.Hour: 1, 0: 0, -time.Hour: 0} {
		if got := daysUntil(now.Add(d), now); got != want {
			t.Errorf("daysUntil(+%s) = %d, want %d", d, got, want)
		}
	}
}

// Each reminder goes out once, in order, and the ladder restarts when the
// expiry changes.
func TestSweepEvaluationsSendsEachStageOnce(t *testing.T) {
	svc, database, _ := newNICTestService(t, "esxi")
	ts := svc.targets
	ts.SetNotificationService(NewNotificationService(database))
	admin := &models.User{Username: "admin", Role: "admin", IsActive: true}
	if err := database.CreateUser(admin); err != nil {
		t.Fatal(err)
	}
	notes := func() int {
		n, err := database.ListNotificationsForUser(admin.ID, false, 50)
		if err != nil {
			t.Fatal(err)
		}
		return len(n)
	}
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	exp := now.Add(20 * 24 * time.Hour)
	if err := database.UpdateTargetCapabilities(1, "Evaluation Mode", true, "", &exp); err != nil {
		t.Fatal(err)
	}
	if n := ts.SweepEvaluations(now); n != 0 || notes() != 0 {
		t.Fatalf("20 days left: nothing due, sent %d", n)
	}
	if n := ts.SweepEvaluations(now.Add(8 * 24 * time.Hour)); n != 1 || notes() != 1 { // 12 days left → 14-day reminder
		t.Fatalf("12 days left: want one reminder, sent %d notes %d", n, notes())
	}
	if n := ts.SweepEvaluations(now.Add(9 * 24 * time.Hour)); n != 0 { // still the 14-day stage
		t.Fatalf("same stage must not repeat, sent %d", n)
	}
	if n := ts.SweepEvaluations(now.Add(19*24*time.Hour + 23*time.Hour)); n != 1 { // 1 hour left → 1-day stage (7 skipped, that's fine)
		t.Fatalf("1 day left: sent %d", n)
	}
	if n := ts.SweepEvaluations(now.Add(21 * 24 * time.Hour)); n != 1 || notes() != 3 { // expired
		t.Fatalf("expired: sent %d notes %d", n, notes())
	}
	got, _ := database.GetTarget(1)
	if got.EvaluationWarnedStage != 0 {
		t.Errorf("stage = %d, want 0", got.EvaluationWarnedStage)
	}
	// A key assigned: expiry cleared, nothing more; a new evaluation restarts.
	if err := database.UpdateTargetCapabilities(1, "VMware vSphere 8 Standard", true, "", nil); err != nil {
		t.Fatal(err)
	}
	if n := ts.SweepEvaluations(now.Add(30 * 24 * time.Hour)); n != 0 {
		t.Fatalf("keyed host: sent %d", n)
	}
	exp2 := now.Add(40 * 24 * time.Hour)
	if err := database.UpdateTargetCapabilities(1, "Evaluation Mode", true, "", &exp2); err != nil {
		t.Fatal(err)
	}
	if n := ts.SweepEvaluations(now.Add(30 * 24 * time.Hour)); n != 1 || notes() != 4 { // 10 days left again
		t.Fatalf("new evaluation: sent %d notes %d", n, notes())
	}
}
