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
	var actions int
	if err := database.conn.QueryRow(`SELECT COUNT(*) FROM actions`).Scan(&actions); err != nil {
		t.Fatal(err)
	}

	s, err := database.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	want := Stats{TotalTargets: 1, TotalTemplates: 1, TotalDeployments: 2, DeploymentsToday: 2, RunningDeploys: 1,
		TotalVMs: 1, TotalActions: actions, ManagedTemplates: 1, ScheduledBuildsToday: 0}
	if *s != want {
		t.Errorf("GetStats = %+v, want %+v", *s, want)
	}
}
