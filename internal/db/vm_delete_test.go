package db

import (
	"testing"

	"github.com/forgemill/forgemill/internal/db/models"
)

func TestDeleteManagedVMSucceedsWithExecutionHistory(t *testing.T) {
	database := newTestDB(t)

	target := &models.Target{Name: "t1", Type: "esxi", Hostname: "esxi.example.com", Port: 443, Username: "admin", PasswordEncrypt: "enc"}
	if err := database.CreateTarget(target); err != nil {
		t.Fatalf("create target: %v", err)
	}
	user := &models.User{Username: "alice", Role: "user", IsActive: true}
	if err := database.CreateUser(user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	vm := &models.ManagedVM{TargetID: target.ID, VMName: "web-01", VMRef: "vm-1"}
	if err := database.CreateManagedVM(vm); err != nil {
		t.Fatalf("create managed vm: %v", err)
	}
	exec := &models.ActionExecution{VMID: vm.ID, ActionName: "ad-hoc", Script: "true", Status: "completed", CreatedBy: user.ID}
	if err := database.CreateExecution(exec); err != nil {
		t.Fatalf("create execution: %v", err)
	}

	// Before the fix, this would fail with a foreign key constraint error
	// because action_executions.vm_id has no ON DELETE CASCADE.
	if err := database.DeleteManagedVM(vm.ID); err != nil {
		t.Fatalf("expected delete to succeed with execution history present, got: %v", err)
	}

	if _, err := database.GetManagedVM(vm.ID); err == nil {
		t.Error("expected VM record to be gone")
	}
	if _, err := database.GetExecution(exec.ID); err == nil {
		t.Error("expected the VM's execution history to be removed along with it")
	}
}

func TestDeleteActionSucceedsWithExecutionHistoryAndPreservesIt(t *testing.T) {
	database := newTestDB(t)

	target := &models.Target{Name: "t1", Type: "esxi", Hostname: "esxi.example.com", Port: 443, Username: "admin", PasswordEncrypt: "enc"}
	if err := database.CreateTarget(target); err != nil {
		t.Fatalf("create target: %v", err)
	}
	user := &models.User{Username: "bob", Role: "user", IsActive: true}
	if err := database.CreateUser(user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	vm := &models.ManagedVM{TargetID: target.ID, VMName: "web-02", VMRef: "vm-2"}
	if err := database.CreateManagedVM(vm); err != nil {
		t.Fatalf("create managed vm: %v", err)
	}
	action := &models.Action{Name: "install-nginx", Category: "packages", Script: "apt-get install -y nginx"}
	if err := database.CreateAction(action); err != nil {
		t.Fatalf("create action: %v", err)
	}
	actionID := action.ID
	exec := &models.ActionExecution{VMID: vm.ID, ActionID: &actionID, ActionName: action.Name, Script: action.Script, Status: "completed", CreatedBy: user.ID}
	if err := database.CreateExecution(exec); err != nil {
		t.Fatalf("create execution: %v", err)
	}

	// Before the fix, this would fail with a foreign key constraint error
	// because action_executions.action_id has no ON DELETE CASCADE.
	if err := database.DeleteAction(action.ID); err != nil {
		t.Fatalf("expected delete to succeed with execution history present, got: %v", err)
	}

	if _, err := database.GetAction(action.ID); err == nil {
		t.Error("expected action record to be gone")
	}

	got, err := database.GetExecution(exec.ID)
	if err != nil {
		t.Fatalf("expected the execution record to survive action deletion, got: %v", err)
	}
	if got.ActionID != nil {
		t.Errorf("expected action_id to be soft-unlinked to nil, got %v", *got.ActionID)
	}
	if got.ActionName != "install-nginx" || got.Script != "apt-get install -y nginx" {
		t.Errorf("expected the execution's denormalized action_name/script to remain intact, got %+v", got)
	}
}
