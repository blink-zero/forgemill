package db

import (
	"testing"

	"github.com/forgemill/forgemill/internal/db/models"
)

func TestIgnoreListAndUnmanagedCountRoundTrip(t *testing.T) {
	database := openTestDB(t)
	target := &models.Target{Name: "t", Type: "vcenter", Hostname: "h", Port: 443, Username: "u", PasswordEncrypt: "e"}
	if err := database.CreateTarget(target); err != nil {
		t.Fatal(err)
	}
	by := int64(7)
	if err := database.IgnoreVMs(target.ID, map[string]string{"vm-1": "a", "vm-2": "b", "": "skip"}, &by); err != nil {
		t.Fatal(err)
	}
	if err := database.IgnoreVMs(target.ID, map[string]string{"vm-1": "a-renamed"}, &by); err != nil {
		t.Fatal(err) // idempotent, name refreshed
	}
	list, _ := database.ListIgnoredVMs(target.ID)
	if len(list) != 2 || list[0].VMName != "a-renamed" || *list[0].IgnoredBy != 7 {
		t.Errorf("ignore list: %+v", list)
	}
	if err := database.UnignoreVMs(target.ID, []string{"vm-1", "nope"}); err != nil {
		t.Fatal(err)
	}
	if list, _ = database.ListIgnoredVMs(target.ID); len(list) != 1 || list[0].VMRef != "vm-2" {
		t.Errorf("after unignore: %+v", list)
	}
	if err := database.UpdateTargetUnmanaged(target.ID, 5); err != nil {
		t.Fatal(err)
	}
	got, _ := database.GetTarget(target.ID)
	if got.UnmanagedVMs != 5 || got.UnmanagedCheckedAt == nil {
		t.Errorf("unmanaged count: %+v", got)
	}
	all, _ := database.ListTargets()
	if all[0].UnmanagedVMs != 5 {
		t.Errorf("list carries the count too: %+v", all[0])
	}
	// Deleting the target removes its ignore list (FK cascade).
	if err := database.DeleteTarget(target.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ = database.ListIgnoredVMs(target.ID); len(list) != 0 {
		t.Errorf("ignore list should cascade on target delete: %+v", list)
	}
}
