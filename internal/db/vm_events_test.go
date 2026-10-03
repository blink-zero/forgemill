package db

import (
	"testing"
	"time"
)

func TestVMEventsRoundTripAndPrune(t *testing.T) {
	database := openTestDB(t)
	for i, lvl := range []string{"info", "warn", "error"} {
		if err := database.AddVMEvent(7, 2, lvl, "event "+lvl); err != nil {
			t.Fatal(err)
		}
		_ = i
	}
	if err := database.AddVMEvent(8, 2, "info", "other vm"); err != nil {
		t.Fatal(err)
	}
	events, err := database.ListVMEvents(7, 10)
	if err != nil || len(events) != 3 {
		t.Fatalf("ListVMEvents: %v %v", events, err)
	}
	if events[0].Message != "event error" || events[0].TargetID != 2 {
		t.Errorf("newest first with target id: %+v", events[0])
	}
	problems, err := database.ListRecentVMEvents(10, true)
	if err != nil || len(problems) != 2 {
		t.Errorf("problems only: %v %v", problems, err)
	}
	all, _ := database.ListRecentVMEvents(10, false)
	if len(all) != 4 {
		t.Errorf("all events: %d", len(all))
	}
	// Nothing is old enough yet; a zero window prunes everything.
	if n, err := database.PruneVMEvents(24 * time.Hour); err != nil || n != 0 {
		t.Errorf("prune 24h: %d %v", n, err)
	}
	if n, err := database.PruneVMEvents(-time.Hour); err != nil || n != 4 {
		t.Errorf("prune everything: %d %v", n, err)
	}
}
