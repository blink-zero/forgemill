package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/service"
)

var validParamName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
var validParamTypes = map[string]bool{"string": true, "number": true, "select": true, "boolean": true, "password": true}
var validActionCategories = map[string]bool{"packages": true, "scripts": true, "security": true, "monitoring": true, "custom": true}

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

// normalizeTags trims, lowercases, dedupes, and caps the tag list so search
// stays consistent regardless of how a tag was typed ("Docker" and "docker"
// collapse to one). Capped at 10 tags / 30 chars each — plenty for
// discoverability without turning tags into a second description field.
func normalizeTags(tags []string) ([]string, error) {
	if len(tags) > 10 {
		return nil, fmt.Errorf("at most 10 tags allowed, got %d", len(tags))
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if len(t) > 30 {
			return nil, fmt.Errorf("tag %q exceeds 30 characters", t)
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out, nil
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
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Category    string                   `json:"category"`
	Script      string                   `json:"script"`
	Parameters  []models.ActionParameter `json:"parameters,omitempty"`
	Tags        []string                 `json:"tags,omitempty"`
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

	if req.Category == "" {
		req.Category = "custom"
	}
	if !validActionCategories[req.Category] {
		writeError(w, "invalid category; must be one of: packages, scripts, security, monitoring, custom", http.StatusBadRequest)
		return
	}

	if len(req.Parameters) > 0 {
		if err := validateParameters(req.Parameters); err != nil {
			writeError(w, "invalid parameters: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	tags, err := normalizeTags(req.Tags)
	if err != nil {
		writeError(w, "invalid tags: "+err.Error(), http.StatusBadRequest)
		return
	}

	action := &models.Action{
		Name:        req.Name,
		Description: req.Description,
		Category:    req.Category,
		Script:      req.Script,
		Parameters:  req.Parameters,
		Tags:        tags,
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

	if req.Category == "" {
		req.Category = "custom"
	}
	if !validActionCategories[req.Category] {
		writeError(w, "invalid category", http.StatusBadRequest)
		return
	}

	if len(req.Parameters) > 0 {
		if err := validateParameters(req.Parameters); err != nil {
			writeError(w, "invalid parameters: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	tags, err := normalizeTags(req.Tags)
	if err != nil {
		writeError(w, "invalid tags: "+err.Error(), http.StatusBadRequest)
		return
	}

	existing.Name = req.Name
	existing.Description = req.Description
	existing.Category = req.Category
	existing.Script = req.Script
	existing.Parameters = req.Parameters
	existing.Tags = tags

	user := middleware.UserFromContext(r.Context())
	if err := h.db.UpdateAction(existing, &user.ID); err != nil {
		writeErrorLog(w, "failed to update action", http.StatusInternalServerError, err)
		return
	}

	h.audit.Log(user.Username, &user.ID, "action.update", "action", fmt.Sprintf("%d", id), service.IPFromRequest(r), map[string]interface{}{
		"action_name": existing.Name, "new_version": existing.Version,
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

// maxImportActions bounds the number of actions accepted in a single import
// request, independent of the global request body size limit — this keeps
// worst-case work (validation + DB writes) per request bounded even for a
// payload built from many small entries.
const maxImportActions = 100

// actionImportItem mirrors createActionRequest deliberately: importing an
// action must not be able to do anything a manual "Create" through the UI
// couldn't already do (e.g. it can't set script_type/platform or mark an
// action builtin).
type actionImportItem struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Category    string                   `json:"category"`
	Script      string                   `json:"script"`
	Parameters  []models.ActionParameter `json:"parameters,omitempty"`
	Tags        []string                 `json:"tags,omitempty"`
}

type actionImportRequest struct {
	Actions []actionImportItem `json:"actions"`
}

type actionImportResult struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Status string `json:"status"` // "created" or "failed"
	ID     int64  `json:"id,omitempty"`
	Error  string `json:"error,omitempty"`
}

type actionImportResponse struct {
	Created int                  `json:"created"`
	Failed  int                  `json:"failed"`
	Results []actionImportResult `json:"results"`
}

// buildImportedAction validates a single import entry and turns it into an
// Action ready to persist. It applies exactly the same rules as Create, so
// an import can't create anything a manual "Create" through the UI couldn't.
func buildImportedAction(item actionImportItem) (*models.Action, error) {
	if item.Name == "" || item.Script == "" {
		return nil, fmt.Errorf("name and script are required")
	}
	if err := service.ValidateActionScript(item.Script); err != nil {
		return nil, fmt.Errorf("invalid script: %w", err)
	}

	category := item.Category
	if category == "" {
		category = "custom"
	}
	if !validActionCategories[category] {
		return nil, fmt.Errorf("invalid category; must be one of: packages, scripts, security, monitoring, custom")
	}

	if len(item.Parameters) > 0 {
		if err := validateParameters(item.Parameters); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
	}

	tags, err := normalizeTags(item.Tags)
	if err != nil {
		return nil, fmt.Errorf("invalid tags: %w", err)
	}

	return &models.Action{
		Name:        item.Name,
		Description: item.Description,
		Category:    category,
		Script:      item.Script,
		Parameters:  item.Parameters,
		Tags:        tags,
	}, nil
}

// Import creates one or more actions from a previously exported JSON file.
// Each entry runs through the same validation as Create, and a bad entry
// only fails that entry — the rest of the batch still imports. Imported
// actions are always plain (non-builtin, "bash") actions, same as anything
// created through the UI.
func (h *ActionHandler) Import(w http.ResponseWriter, r *http.Request) {
	var req actionImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.Actions) == 0 {
		writeError(w, "no actions to import", http.StatusBadRequest)
		return
	}
	if len(req.Actions) > maxImportActions {
		writeError(w, fmt.Sprintf("too many actions in one import (max %d)", maxImportActions), http.StatusBadRequest)
		return
	}

	user := middleware.UserFromContext(r.Context())
	resp := actionImportResponse{Results: make([]actionImportResult, 0, len(req.Actions))}

	for i, item := range req.Actions {
		result := actionImportResult{Index: i, Name: item.Name}

		action, err := buildImportedAction(item)
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			resp.Results = append(resp.Results, result)
			resp.Failed++
			continue
		}

		if err := h.db.CreateAction(action); err != nil {
			slog.Error("failed to create imported action", "name", item.Name, "error", err)
			result.Status = "failed"
			result.Error = "failed to save action"
			resp.Results = append(resp.Results, result)
			resp.Failed++
			continue
		}

		h.audit.Log(user.Username, &user.ID, "action.import", "action", fmt.Sprintf("%d", action.ID), service.IPFromRequest(r), map[string]interface{}{
			"action_name": action.Name, "category": action.Category,
		})

		result.Status = "created"
		result.ID = action.ID
		resp.Results = append(resp.Results, result)
		resp.Created++
	}

	// Always 200: the request itself was well-formed, so per-item outcomes
	// (some or all of which may have failed validation) belong in the body,
	// not the status code — callers read resp.Created/resp.Failed/resp.Results.
	writeJSON(w, http.StatusOK, resp)
}

// actionVersionFromCurrent adapts the live action into the same shape as a
// stored ActionVersion, so callers see one consistent timeline regardless
// of whether an entry is the current row or a superseded snapshot.
func actionVersionFromCurrent(a *models.Action) models.ActionVersion {
	return models.ActionVersion{
		ActionID:    a.ID,
		Version:     a.Version,
		Name:        a.Name,
		Description: a.Description,
		Category:    a.Category,
		Script:      a.Script,
		ScriptType:  a.ScriptType,
		Platform:    a.Platform,
		Parameters:  a.Parameters,
		Tags:        a.Tags,
		CreatedAt:   a.UpdatedAt,
	}
}

func (h *ActionHandler) ListVersions(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	current, err := h.db.GetAction(id)
	if err != nil {
		writeError(w, "action not found", http.StatusNotFound)
		return
	}
	history, err := h.db.ListActionVersionHistory(id)
	if err != nil {
		writeErrorLog(w, "failed to list action versions", http.StatusInternalServerError, err)
		return
	}
	versions := append([]models.ActionVersion{actionVersionFromCurrent(current)}, history...)
	writeJSON(w, http.StatusOK, versions)
}

func (h *ActionHandler) GetVersion(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	version, err := strconv.Atoi(chi.URLParam(r, "version"))
	if err != nil {
		writeError(w, "invalid version", http.StatusBadRequest)
		return
	}

	current, err := h.db.GetAction(id)
	if err != nil {
		writeError(w, "action not found", http.StatusNotFound)
		return
	}
	if version == current.Version {
		writeJSON(w, http.StatusOK, actionVersionFromCurrent(current))
		return
	}
	v, err := h.db.GetActionVersion(id, version)
	if err != nil {
		writeError(w, "version not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

type rollbackActionRequest struct {
	Version int `json:"version"`
}

func (h *ActionHandler) Rollback(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	var req rollbackActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Version < 1 {
		writeError(w, "invalid request body: version is required", http.StatusBadRequest)
		return
	}

	user := middleware.UserFromContext(r.Context())
	restored, err := h.db.RollbackAction(id, req.Version, &user.ID)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	h.audit.Log(user.Username, &user.ID, "action.rollback", "action", fmt.Sprintf("%d", id), service.IPFromRequest(r), map[string]interface{}{
		"action_name": restored.Name, "rolled_back_to": req.Version, "new_version": restored.Version,
	})

	writeJSON(w, http.StatusOK, restored)
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
