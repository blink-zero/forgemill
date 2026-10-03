package db

import (
	"testing"

	"github.com/forgemill/forgemill/internal/db/models"
)

// A custom action that a deployment ran must still be deletable: the
// deployment_actions link rows go with it, the deployment itself stays.
func TestDeleteActionSucceedsWhenADeploymentUsedIt(t *testing.T) {
	database := openTestDB(t)
	dep, _ := seedDeploymentAndVM(t, database)

	action := &models.Action{Name: "FM audit noop marker", Description: "noop", Script: "true", Category: "custom"}
	if err := database.CreateAction(action); err != nil {
		t.Fatal(err)
	}
	if err := database.SetDeploymentActions(dep.ID, []int64{action.ID}); err != nil {
		t.Fatal(err)
	}
	if acts, err := database.GetDeploymentActions(dep.ID); err != nil || len(acts) != 1 {
		t.Fatalf("precondition: deployment should list the action, got %v %v", acts, err)
	}

	if err := database.DeleteAction(action.ID); err != nil {
		t.Fatalf("DeleteAction with deployment history: %v", err)
	}
	if _, err := database.GetAction(action.ID); err == nil {
		t.Error("action should be gone")
	}
	if _, err := database.GetDeployment(dep.ID); err != nil {
		t.Errorf("deployment must survive: %v", err)
	}
	if acts, err := database.GetDeploymentActions(dep.ID); err != nil || len(acts) != 0 {
		t.Errorf("deployment's action list should no longer include the deleted action, got %v %v", acts, err)
	}
}
