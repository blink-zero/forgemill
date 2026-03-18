package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/service"
)

type BulkDeployHandler struct {
	svc *service.BulkDeployService
}

func NewBulkDeployHandler(svc *service.BulkDeployService) *BulkDeployHandler {
	return &BulkDeployHandler{svc: svc}
}

func (h *BulkDeployHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req service.BulkDeployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		writeError(w, "name is required", http.StatusBadRequest)
		return
	}
	if len(req.VMs) == 0 {
		writeError(w, "at least one VM instance is required", http.StatusBadRequest)
		return
	}
	// M3: Enforce maximum VM count per bulk deployment
	if len(req.VMs) > 50 {
		writeError(w, "bulk deployment limited to 50 VMs per request", http.StatusBadRequest)
		return
	}

	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	bulk, err := h.svc.Start(&req, user.ID)
	if err != nil {
		writeErrorLog(w, "failed to start bulk deployment", http.StatusInternalServerError, err)
		return
	}

	// M5: Audit log
	slog.Info("audit",
		"action", "bulk_deployment.start",
		"actor", user.Username,
		"bulk_id", bulk.ID,
		"vm_count", len(req.VMs),
	)

	writeJSON(w, http.StatusAccepted, bulk)
}

func (h *BulkDeployHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	bulk, err := h.svc.Get(id)
	if err != nil {
		writeError(w, "bulk deployment not found", http.StatusNotFound)
		return
	}
	// HIGH-06: Ownership check — only creator or admin can view bulk deployment details
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if bulk.CreatedBy != user.ID && user.Role != "admin" {
		writeError(w, "forbidden", http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, bulk)
}

func (h *BulkDeployHandler) List(w http.ResponseWriter, r *http.Request) {
	// SEC-2: Filter by ownership — admins see all, non-admins see only their own
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var bulks []models.BulkDeployment
	var err error
	if user.Role == "admin" {
		bulks, err = h.svc.List()
	} else {
		bulks, err = h.svc.ListByUser(user.ID)
	}
	if err != nil {
		writeErrorLog(w, "failed to list bulk deployments", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, bulks)
}
