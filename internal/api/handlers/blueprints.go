package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/service"
)

type BlueprintHandler struct {
	svc *service.BlueprintService
}

func NewBlueprintHandler(svc *service.BlueprintService) *BlueprintHandler {
	return &BlueprintHandler{svc: svc}
}

func (h *BlueprintHandler) List(w http.ResponseWriter, r *http.Request) {
	blueprints, err := h.svc.List()
	if err != nil {
		writeErrorLog(w, "failed to list blueprints", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, blueprints)
}

func (h *BlueprintHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	bp, err := h.svc.Get(id)
	if err != nil {
		writeError(w, "blueprint not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, bp)
}

type createBlueprintRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	TemplateID  *int64 `json:"template_id"`
	TargetID    *int64 `json:"target_id"`
	ConfigJSON  string `json:"config_json"`
}

func (h *BlueprintHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createBlueprintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		writeError(w, "name is required", http.StatusBadRequest)
		return
	}

	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if req.ConfigJSON != "" && !json.Valid([]byte(req.ConfigJSON)) {
		writeError(w, "config_json must be valid JSON", http.StatusBadRequest)
		return
	}
	bp := &models.Blueprint{
		Name:        req.Name,
		Description: req.Description,
		TemplateID:  req.TemplateID,
		TargetID:    req.TargetID,
		ConfigJSON:  req.ConfigJSON,
		CreatedBy:   user.ID,
	}

	if err := h.svc.Create(bp); err != nil {
		writeErrorLog(w, "failed to create blueprint", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, bp)
}

func (h *BlueprintHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}

	existing, err := h.svc.Get(id)
	if err != nil {
		writeError(w, "blueprint not found", http.StatusNotFound)
		return
	}

	// V3-H4: Verify blueprint ownership before allowing update
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if existing.CreatedBy != user.ID && user.Role != "admin" {
		writeError(w, "insufficient permissions", http.StatusForbidden)
		return
	}

	var req createBlueprintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Description != "" {
		existing.Description = req.Description
	}
	if req.TemplateID != nil {
		existing.TemplateID = req.TemplateID
	}
	if req.TargetID != nil {
		existing.TargetID = req.TargetID
	}
	if req.ConfigJSON != "" {
		if !json.Valid([]byte(req.ConfigJSON)) {
			writeError(w, "config_json must be valid JSON", http.StatusBadRequest)
			return
		}
		existing.ConfigJSON = req.ConfigJSON
	}

	if err := h.svc.Update(existing); err != nil {
		writeErrorLog(w, "failed to update blueprint", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (h *BlueprintHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}

	// V3-H4: Verify blueprint ownership before allowing delete
	existing, err := h.svc.Get(id)
	if err != nil {
		writeError(w, "blueprint not found", http.StatusNotFound)
		return
	}
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if existing.CreatedBy != user.ID && user.Role != "admin" {
		writeError(w, "insufficient permissions", http.StatusForbidden)
		return
	}

	if err := h.svc.Delete(id); err != nil {
		writeErrorLog(w, "failed to delete blueprint", http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type deployBlueprintRequest struct {
	VMName string `json:"vm_name"`
}

func (h *BlueprintHandler) Deploy(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid blueprint ID", http.StatusBadRequest)
		return
	}

	// V5-M3: Verify blueprint ownership before allowing deploy
	bp, err := h.svc.Get(id)
	if err != nil {
		writeError(w, "blueprint not found", http.StatusNotFound)
		return
	}
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if bp.CreatedBy != user.ID && user.Role != "admin" {
		writeError(w, "blueprint not found", http.StatusNotFound)
		return
	}

	var req deployBlueprintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.VMName == "" {
		writeError(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	deployment, err := h.svc.DeployFromBlueprint(id, req.VMName, user.ID)
	if err != nil {
		writeErrorLog(w, "failed to deploy from blueprint", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusAccepted, deployment)
}
