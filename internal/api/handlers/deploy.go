package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/service"
)

type DeployHandler struct {
	svc   *service.DeployService
	audit *service.AuditService
}

func NewDeployHandler(svc *service.DeployService, audit *service.AuditService) *DeployHandler {
	return &DeployHandler{svc: svc, audit: audit}
}

func (h *DeployHandler) Deploy(w http.ResponseWriter, r *http.Request) {
	var req service.DeployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.VMName == "" || req.TemplateID == 0 || req.TargetID == 0 {
		writeError(w, "vm_name, template_id, and target_id are required", http.StatusBadRequest)
		return
	}

	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	resp, err := h.svc.Start(&req, user.ID)
	if err != nil {
		writeErrorLog(w, "failed to start deployment", http.StatusInternalServerError, err)
		return
	}

	// M5: Audit log
	slog.Info("audit",
		"action", "deployment.start",
		"actor", user.Username,
		"deployment_id", resp.ID,
		"vm_name", req.VMName,
	)
	h.audit.Log(user.Username, &user.ID, "deployment.start", "deployment", strconv.FormatInt(resp.ID, 10), service.IPFromRequest(r), map[string]interface{}{"vm_name": req.VMName, "deployment_id": resp.ID})

	writeJSON(w, http.StatusAccepted, resp)
}

func (h *DeployHandler) Status(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	deployment, err := h.svc.Get(id)
	if err != nil {
		writeError(w, "deployment not found", http.StatusNotFound)
		return
	}
	// HIGH-05: Ownership check — only creator or admin can view deployment details
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if deployment.CreatedBy != user.ID && user.Role != "admin" {
		writeError(w, "forbidden", http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, deployment)
}

func (h *DeployHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}

	// H2: Verify deployment ownership before cancellation
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	deployment, err := h.svc.Get(id)
	if err != nil {
		writeError(w, "deployment not found", http.StatusNotFound)
		return
	}
	if deployment.CreatedBy != user.ID && user.Role != "admin" {
		writeError(w, "insufficient permissions", http.StatusForbidden)
		return
	}

	// B-7: Only allow cancellation of deployments in a cancellable state
	if deployment.Status != "pending" && deployment.Status != "running" {
		writeError(w, "deployment is already in terminal state: "+deployment.Status, http.StatusConflict)
		return
	}

	if err := h.svc.Cancel(id); err != nil {
		writeErrorLog(w, "failed to cancel deployment", http.StatusInternalServerError, err)
		return
	}

	// M5: Audit log
	slog.Info("audit",
		"action", "deployment.cancel",
		"actor", user.Username,
		"deployment_id", id,
	)
	h.audit.Log(user.Username, &user.ID, "deployment.cancel", "deployment", strconv.FormatInt(id, 10), service.IPFromRequest(r), map[string]interface{}{"deployment_id": id})

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}
