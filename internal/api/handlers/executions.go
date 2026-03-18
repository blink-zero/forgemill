package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/service"
)

type ExecutionHandler struct {
	executor *service.ExecutorService
	audit    *service.AuditService
}

func NewExecutionHandler(executor *service.ExecutorService, audit *service.AuditService) *ExecutionHandler {
	return &ExecutionHandler{executor: executor, audit: audit}
}

// Execute starts an action execution on a VM.
// POST /api/vms/{id}/execute
func (h *ExecutionHandler) Execute(w http.ResponseWriter, r *http.Request) {
	vmID, err := parseID(r)
	if err != nil {
		writeError(w, "invalid VM ID", http.StatusBadRequest)
		return
	}

	var req service.ExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	user := middleware.UserFromContext(r.Context())

	exec, err := h.executor.Execute(r.Context(), vmID, req, user.ID)
	if err != nil {
		writeErrorLog(w, err.Error(), http.StatusBadRequest, err)
		return
	}

	h.audit.Log(user.Username, &user.ID, "execution.start", "vm", fmt.Sprintf("%d", vmID), service.IPFromRequest(r), map[string]interface{}{
		"execution_id": exec.ID,
		"action_name":  exec.ActionName,
		"vm_id":        vmID,
	})

	writeJSON(w, http.StatusCreated, exec)
}

// Cancel cancels a running execution.
// POST /api/executions/{id}/cancel
func (h *ExecutionHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	execID, err := parseID(r)
	if err != nil {
		writeError(w, "invalid execution ID", http.StatusBadRequest)
		return
	}

	user := middleware.UserFromContext(r.Context())

	// Verify execution exists and check permissions
	exec, err := h.executor.GetExecution(execID)
	if err != nil {
		writeError(w, "execution not found", http.StatusNotFound)
		return
	}

	// Cancel requires creator or admin
	if exec.CreatedBy != user.ID && user.Role != "admin" {
		writeError(w, "only the creator or an admin can cancel this execution", http.StatusForbidden)
		return
	}

	if err := h.executor.Cancel(execID); err != nil {
		writeErrorLog(w, err.Error(), http.StatusBadRequest, err)
		return
	}

	h.audit.Log(user.Username, &user.ID, "execution.cancel", "execution", fmt.Sprintf("%d", execID), service.IPFromRequest(r), map[string]interface{}{
		"execution_id": execID,
		"action_name":  exec.ActionName,
		"vm_id":        exec.VMID,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// ListVMExecutions lists execution history for a VM.
// GET /api/vms/{id}/executions
func (h *ExecutionHandler) ListVMExecutions(w http.ResponseWriter, r *http.Request) {
	vmID, err := parseID(r)
	if err != nil {
		writeError(w, "invalid VM ID", http.StatusBadRequest)
		return
	}

	execs, err := h.executor.ListVMExecutions(vmID)
	if err != nil {
		writeErrorLog(w, "failed to list executions", http.StatusInternalServerError, err)
		return
	}
	if execs == nil {
		execs = []models.ActionExecution{}
	}
	writeJSON(w, http.StatusOK, execs)
}

// GetExecution returns a single execution with full output.
// GET /api/executions/{id}
func (h *ExecutionHandler) GetExecution(w http.ResponseWriter, r *http.Request) {
	execID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, "invalid execution ID", http.StatusBadRequest)
		return
	}

	exec, err := h.executor.GetExecution(execID)
	if err != nil {
		writeError(w, "execution not found", http.StatusNotFound)
		return
	}

	// Security: Verify ownership - user can only view their own executions (admins bypass)
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if exec.CreatedBy != user.ID && user.Role != "admin" {
		writeError(w, "insufficient permissions", http.StatusForbidden)
		return
	}

	writeJSON(w, http.StatusOK, exec)
}
