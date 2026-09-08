package service

import (
	"testing"
	"time"
)

func timePtr(t time.Time) *time.Time { return &t }

func TestApplyPowerStateTransitionNoChangeWhenSameState(t *testing.T) {
	now := time.Now()
	onAt := timePtr(now.Add(-time.Hour))
	prev := vmLifecycleState{
		PowerState:          "poweredOn",
		StateChangedAt:      timePtr(now.Add(-2 * time.Hour)),
		LastPoweredOnAt:     onAt,
		TotalRuntimeSeconds: 100,
	}
	next, changed := applyPowerStateTransition(prev, "poweredOn", now)
	if changed {
		t.Fatal("expected changed=false when the observed state matches the stored state")
	}
	if next != prev {
		t.Errorf("expected fields unchanged, got %+v, want %+v", next, prev)
	}
}

func TestApplyPowerStateTransitionFirstObservation(t *testing.T) {
	now := time.Now()
	// unknown -> poweredOn: a freshly-created/registered VM's first observed state.
	next, changed := applyPowerStateTransition(vmLifecycleState{PowerState: "unknown"}, "poweredOn", now)
	if !changed {
		t.Fatal("expected changed=true for unknown -> poweredOn")
	}
	if next.LastPoweredOnAt == nil || !next.LastPoweredOnAt.Equal(now) {
		t.Errorf("expected LastPoweredOnAt = now, got %v", next.LastPoweredOnAt)
	}
	if next.StateChangedAt == nil || !next.StateChangedAt.Equal(now) {
		t.Errorf("expected StateChangedAt = now, got %v", next.StateChangedAt)
	}
	if next.TotalRuntimeSeconds != 0 {
		t.Errorf("expected TotalRuntimeSeconds = 0 on first observation, got %d", next.TotalRuntimeSeconds)
	}
	if next.LastPoweredOffAt != nil {
		t.Errorf("expected LastPoweredOffAt untouched (nil), got %v", next.LastPoweredOffAt)
	}
}

func TestApplyPowerStateTransitionPoweredOnToPoweredOffFlushesRuntime(t *testing.T) {
	onAt := time.Now().Add(-90 * time.Minute)
	now := onAt.Add(90 * time.Minute)
	prev := vmLifecycleState{PowerState: "poweredOn", LastPoweredOnAt: timePtr(onAt), TotalRuntimeSeconds: 500}

	next, changed := applyPowerStateTransition(prev, "poweredOff", now)
	if !changed {
		t.Fatal("expected changed=true for poweredOn -> poweredOff")
	}
	wantTotal := int64(500 + 90*60)
	if next.TotalRuntimeSeconds != wantTotal {
		t.Errorf("TotalRuntimeSeconds = %d, want %d (500 prior + 90m elapsed)", next.TotalRuntimeSeconds, wantTotal)
	}
	if next.LastPoweredOffAt == nil || !next.LastPoweredOffAt.Equal(now) {
		t.Errorf("expected LastPoweredOffAt = now, got %v", next.LastPoweredOffAt)
	}
	// LastPoweredOnAt is a historical marker of when the stretch that just
	// ended began — it's not cleared on power-off.
	if next.LastPoweredOnAt == nil || !next.LastPoweredOnAt.Equal(onAt) {
		t.Errorf("expected LastPoweredOnAt left as the stretch's start (%v), got %v", onAt, next.LastPoweredOnAt)
	}
}

func TestApplyPowerStateTransitionSuspendFreezesWithoutResetting(t *testing.T) {
	onAt := time.Now().Add(-4 * 24 * time.Hour) // running for 4 days
	suspendAt := onAt.Add(4 * 24 * time.Hour)
	prev := vmLifecycleState{PowerState: "poweredOn", LastPoweredOnAt: timePtr(onAt), TotalRuntimeSeconds: 0}

	suspended, changed := applyPowerStateTransition(prev, "suspended", suspendAt)
	if !changed {
		t.Fatal("expected changed=true for poweredOn -> suspended")
	}
	wantFrozenTotal := int64(4 * 24 * 60 * 60)
	if suspended.TotalRuntimeSeconds != wantFrozenTotal {
		t.Errorf("TotalRuntimeSeconds at suspend = %d, want %d (4 days)", suspended.TotalRuntimeSeconds, wantFrozenTotal)
	}
	if suspended.LastPoweredOffAt != nil {
		t.Error("suspend must not set LastPoweredOffAt — that's reserved for an actual power-off")
	}

	// The counter must not tick further while suspended, and re-observing
	// "suspended" on a later sync must not change anything (no-op).
	stillSuspended, changed2 := applyPowerStateTransition(suspended, "suspended", suspendAt.Add(2*time.Hour))
	if changed2 {
		t.Fatal("expected changed=false when re-observing the same suspended state")
	}
	if stillSuspended.TotalRuntimeSeconds != wantFrozenTotal {
		t.Errorf("TotalRuntimeSeconds must stay frozen while suspended: got %d, want %d", stillSuspended.TotalRuntimeSeconds, wantFrozenTotal)
	}

	// Resuming starts a fresh stretch — total is not reset to zero, and a
	// new LastPoweredOnAt begins from the resume point.
	resumeAt := suspendAt.Add(3 * time.Hour)
	resumed, changed3 := applyPowerStateTransition(stillSuspended, "poweredOn", resumeAt)
	if !changed3 {
		t.Fatal("expected changed=true for suspended -> poweredOn")
	}
	if resumed.TotalRuntimeSeconds != wantFrozenTotal {
		t.Errorf("expected total unaffected by resume (not reset to zero): got %d, want %d", resumed.TotalRuntimeSeconds, wantFrozenTotal)
	}
	if resumed.LastPoweredOnAt == nil || !resumed.LastPoweredOnAt.Equal(resumeAt) {
		t.Errorf("expected a fresh LastPoweredOnAt at resume (%v), got %v", resumeAt, resumed.LastPoweredOnAt)
	}
}

func TestApplyPowerStateTransitionRestartKeepsRunningWithoutDoubleCounting(t *testing.T) {
	// restart is modeled as poweredOn -> poweredOn is skipped by the caller
	// in practice (PowerAction always requests "poweredOn" for restart);
	// verify that observing poweredOn while already poweredOn is a no-op
	// so a restart doesn't reset the running stretch or lose accrued time.
	onAt := time.Now().Add(-time.Hour)
	prev := vmLifecycleState{PowerState: "poweredOn", LastPoweredOnAt: timePtr(onAt), TotalRuntimeSeconds: 42}
	next, changed := applyPowerStateTransition(prev, "poweredOn", time.Now())
	if changed {
		t.Fatal("expected no-op when already poweredOn")
	}
	if next.LastPoweredOnAt == nil || !next.LastPoweredOnAt.Equal(onAt) {
		t.Error("LastPoweredOnAt must not move on a no-op observation")
	}
}

func TestApplyPowerStateTransitionUnknownToPoweredOffDoesNotFlushRuntime(t *testing.T) {
	// A VM registered by ref (no prior LastPoweredOnAt) observed as
	// poweredOff for the first time must not synthesize runtime from thin air.
	next, changed := applyPowerStateTransition(vmLifecycleState{PowerState: "unknown"}, "poweredOff", time.Now())
	if !changed {
		t.Fatal("expected changed=true for unknown -> poweredOff")
	}
	if next.TotalRuntimeSeconds != 0 {
		t.Errorf("expected TotalRuntimeSeconds = 0, got %d", next.TotalRuntimeSeconds)
	}
	if next.LastPoweredOffAt == nil {
		t.Error("expected LastPoweredOffAt to be set")
	}
	if next.LastPoweredOnAt != nil {
		t.Error("expected LastPoweredOnAt to remain nil — it was never observed running")
	}
}
