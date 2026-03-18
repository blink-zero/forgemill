package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/service"
)

var validParamName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
var validParamTypes = map[string]bool{"string": true, "number": true, "select": true, "boolean": true, "password": true}

func validateParameters(params []models.ActionParameter) error {
	seen := map[string]bool{}
	for i, p := range params {
		if p.Name == "" {
			return fmt.Errorf("parameter %d: name is required", i+1)
		}
		if !validParamName.MatchString(p.Name) {
			return fmt.Errorf("parameter %q: name must match ^[A-Z][A-Z0-9_]*$", p.Name)
		}
		if seen[p.Name] {
			return fmt.Errorf("parameter %q: duplicate name", p.Name)
		}
		seen[p.Name] = true
		if p.Label == "" {
			return fmt.Errorf("parameter %q: label is required", p.Name)
		}
		if !validParamTypes[p.Type] {
			return fmt.Errorf("parameter %q: invalid type %q", p.Name, p.Type)
		}
		if p.Type == "select" && len(p.Options) == 0 {
			return fmt.Errorf("parameter %q: select type requires options", p.Name)
		}
	}
	return nil
}

type ActionHandler struct {
	db    *db.DB
	audit *service.AuditService
}

func NewActionHandler(db *db.DB, audit *service.AuditService) *ActionHandler {
	return &ActionHandler{db: db, audit: audit}
}

func (h *ActionHandler) List(w http.ResponseWriter, r *http.Request) {
	actions, err := h.db.ListActions()
	if err != nil {
		writeErrorLog(w, "failed to list actions", http.StatusInternalServerError, err)
		return
	}
	if actions == nil {
		actions = []models.Action{}
	}
	writeJSON(w, http.StatusOK, actions)
}

type createActionRequest struct {
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Category    string                  `json:"category"`
	Script      string                  `json:"script"`
	Parameters  []models.ActionParameter `json:"parameters,omitempty"`
}

func (h *ActionHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.Script == "" {
		writeError(w, "name and script are required", http.StatusBadRequest)
		return
	}

	if err := service.ValidateActionScript(req.Script); err != nil {
		writeError(w, "invalid script: "+err.Error(), http.StatusBadRequest)
		return
	}

	validCategories := map[string]bool{"packages": true, "scripts": true, "security": true, "monitoring": true, "custom": true}
	if req.Category == "" {
		req.Category = "custom"
	}
	if !validCategories[req.Category] {
		writeError(w, "invalid category; must be one of: packages, scripts, security, monitoring, custom", http.StatusBadRequest)
		return
	}

	if len(req.Parameters) > 0 {
		if err := validateParameters(req.Parameters); err != nil {
			writeError(w, "invalid parameters: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	action := &models.Action{
		Name:        req.Name,
		Description: req.Description,
		Category:    req.Category,
		Script:      req.Script,
		Parameters:  req.Parameters,
	}
	if err := h.db.CreateAction(action); err != nil {
		writeErrorLog(w, "failed to create action", http.StatusInternalServerError, err)
		return
	}

	user := middleware.UserFromContext(r.Context())
	h.audit.Log(user.Username, &user.ID, "action.create", "action", fmt.Sprintf("%d", action.ID), service.IPFromRequest(r), map[string]interface{}{
		"action_name": action.Name, "category": action.Category,
	})

	writeJSON(w, http.StatusCreated, action)
}

func (h *ActionHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}

	existing, err := h.db.GetAction(id)
	if err != nil {
		writeError(w, "action not found", http.StatusNotFound)
		return
	}
	if existing.Builtin {
		writeError(w, "cannot modify builtin actions", http.StatusForbidden)
		return
	}

	var req createActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.Script == "" {
		writeError(w, "name and script are required", http.StatusBadRequest)
		return
	}

	if err := service.ValidateActionScript(req.Script); err != nil {
		writeError(w, "invalid script: "+err.Error(), http.StatusBadRequest)
		return
	}

	validCategories := map[string]bool{"packages": true, "scripts": true, "security": true, "monitoring": true, "custom": true}
	if req.Category == "" {
		req.Category = "custom"
	}
	if !validCategories[req.Category] {
		writeError(w, "invalid category", http.StatusBadRequest)
		return
	}

	if len(req.Parameters) > 0 {
		if err := validateParameters(req.Parameters); err != nil {
			writeError(w, "invalid parameters: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	existing.Name = req.Name
	existing.Description = req.Description
	existing.Category = req.Category
	existing.Script = req.Script
	existing.Parameters = req.Parameters

	if err := h.db.UpdateAction(existing); err != nil {
		writeErrorLog(w, "failed to update action", http.StatusInternalServerError, err)
		return
	}

	user := middleware.UserFromContext(r.Context())
	h.audit.Log(user.Username, &user.ID, "action.update", "action", fmt.Sprintf("%d", id), service.IPFromRequest(r), map[string]interface{}{
		"action_name": existing.Name,
	})

	writeJSON(w, http.StatusOK, existing)
}

func (h *ActionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}

	existing, err := h.db.GetAction(id)
	if err != nil {
		writeError(w, "action not found", http.StatusNotFound)
		return
	}
	if existing.Builtin {
		writeError(w, "cannot delete builtin actions", http.StatusForbidden)
		return
	}

	if err := h.db.DeleteAction(id); err != nil {
		writeErrorLog(w, "failed to delete action", http.StatusInternalServerError, err)
		return
	}

	user := middleware.UserFromContext(r.Context())
	h.audit.Log(user.Username, &user.ID, "action.delete", "action", fmt.Sprintf("%d", id), service.IPFromRequest(r), map[string]interface{}{
		"action_name": existing.Name,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *ActionHandler) GetDeploymentActions(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}

	actions, err := h.db.GetDeploymentActions(id)
	if err != nil {
		writeErrorLog(w, "failed to get deployment actions", http.StatusInternalServerError, err)
		return
	}
	if actions == nil {
		actions = []models.Action{}
	}
	writeJSON(w, http.StatusOK, actions)
}
