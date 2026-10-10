package db

import (
	"testing"

	"github.com/forgemill/forgemill/internal/db/models"
)

// GetStats is one statement with nine scalar subqueries; this pins that each
// column lands in the right field and counts what it says it counts.
func TestGetStatsCountsEachTableIntoItsField(t *testing.T) {
	database := openTestDB(t)
	dep, _ := seedDeploymentAndVM(t, database) // 1 target, 1 template, 1 completed deployment (today), 1 VM

	// A second deployment left running, and a managed template.
	running := &models.Deployment{TemplateID: dep.TemplateID, TargetID: dep.TargetID, VMName: "web-02", Status: "pending", CreatedBy: 1}
	if err := database.CreateDeployment(running); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateDeploymentStatus(running.ID, "running", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := database.conn.Exec(`UPDATE templates SET managed_by_forgemill = TRUE, lifecycle_status = 'active' WHERE id = ?`, *dep.TemplateID); err != nil {
		t.Fatal(err)
	}
	var actions, active int
	if err := database.conn.QueryRow(`SELECT COUNT(*), SUM(status = 'active') FROM actions`).Scan(&actions, &active); err != nil {
		t.Fatal(err)
	}
	// A draft action, a failed deployment from an hour ago, a stopped VM and a target in error.
	if _, err := database.conn.Exec(`INSERT INTO actions (name, description, category, script, status, source) VALUES ('d', '', 'custom', 'echo', 'draft', 'ai')`); err != nil {
		t.Fatal(err)
	}
	failed := &models.Deployment{TemplateID: dep.TemplateID, TargetID: dep.TargetID, VMName: "web-03", Status: "pending", CreatedBy: 1}
	if err := database.CreateDeployment(failed); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateDeploymentStatus(failed.ID, "failed", "boom"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.conn.Exec(`UPDATE managed_vms SET power_state = 'poweredOff'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.conn.Exec(`UPDATE targets SET status = 'error'`); err != nil {
		t.Fatal(err)
	}

	s, err := database.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	want := Stats{TotalTargets: 1, TotalTemplates: 1, TotalDeployments: 3, DeploymentsToday: 3, RunningDeploys: 1,
		TotalVMs: 1, TotalActions: actions + 1, ManagedTemplates: 1, ScheduledBuildsToday: 0,
		VMsRunning: 0, VMsStopped: 1, ActiveActions: active, DraftActions: 1, TargetsConnected: 0, TargetsError: 1, FailedDeployments24h: 1}
	if *s != want {
		t.Errorf("GetStats = %+v, want %+v", *s, want)
	}
}

// The dashboard feed carries the VM's name and no script/output.
func TestListRecentExecutionsJoinsVMNameWithoutPayload(t *testing.T) {
	database := openTestDB(t)
	_, vm := seedDeploymentAndVM(t, database)
	e := &models.ActionExecution{VMID: vm.ID, ActionName: "Update", Script: "apt-get update", Status: "completed", CreatedBy: 1}
	if err := database.CreateExecution(e); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateExecutionStatus(e.ID, "completed", nil, "lots of output"); err != nil {
		t.Fatal(err)
	}
	list, err := database.ListRecentExecutions(5)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].VMName != vm.VMName || list[0].Script != "" || list[0].Output != "" || list[0].ActionName != "Update" {
		t.Errorf("feed: %+v", list)
	}
}
