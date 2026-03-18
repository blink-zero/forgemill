package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/service"
)

type AuthSourceHandler struct {
	svc *service.LDAPService
}

func NewAuthSourceHandler(svc *service.LDAPService) *AuthSourceHandler {
	return &AuthSourceHandler{svc: svc}
}

func (h *AuthSourceHandler) List(w http.ResponseWriter, r *http.Request) {
	sources, err := h.svc.ListSources()
	if err != nil {
		writeErrorLog(w, "failed to list auth sources", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, sources)
}

func (h *AuthSourceHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	source, err := h.svc.GetSource(id)
	if err != nil {
		writeError(w, "auth source not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, source)
}

type createAuthSourceRequest struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	ConfigJSON string `json:"config_json"`
	IsDefault  bool   `json:"is_default"`
	Enabled    bool   `json:"enabled"`
}

func (h *AuthSourceHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createAuthSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Type == "" {
		writeError(w, "name and type are required", http.StatusBadRequest)
		return
	}
	validTypes := map[string]bool{"ldap": true, "saml": true, "oidc": true}
	if !validTypes[req.Type] {
		writeError(w, "type must be one of: ldap, saml, oidc", http.StatusBadRequest)
		return
	}
	if req.ConfigJSON != "" && !json.Valid([]byte(req.ConfigJSON)) {
		writeError(w, "config_json must be valid JSON", http.StatusBadRequest)
		return
	}

	source := &models.AuthSource{
		Name:       req.Name,
		Type:       req.Type,
		ConfigJSON: req.ConfigJSON,
		IsDefault:  req.IsDefault,
		Enabled:    req.Enabled,
	}
	if source.ConfigJSON == "" {
		source.ConfigJSON = "{}"
	}

	if err := h.svc.CreateSource(source); err != nil {
		writeErrorLog(w, "failed to create auth source", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, source)
}

// V3-M18: Use pointer types for boolean fields to distinguish omitted from explicitly-false
type updateAuthSourceRequest struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	ConfigJSON string `json:"config_json"`
	IsDefault  *bool  `json:"is_default,omitempty"`
	Enabled    *bool  `json:"enabled,omitempty"`
}

func (h *AuthSourceHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}

	existing, err := h.svc.GetSource(id)
	if err != nil {
		writeError(w, "auth source not found", http.StatusNotFound)
		return
	}

	var req updateAuthSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Type != "" {
		// SEC-7/F-53: Validate type against allowed values on update too
		validTypes := map[string]bool{"ldap": true, "saml": true, "oidc": true}
		if !validTypes[req.Type] {
			writeError(w, "type must be one of: ldap, saml, oidc", http.StatusBadRequest)
			return
		}
		existing.Type = req.Type
	}
	if req.ConfigJSON != "" {
		// SEC-7: Validate ConfigJSON is valid JSON on update
		if !json.Valid([]byte(req.ConfigJSON)) {
			writeError(w, "config_json must be valid JSON", http.StatusBadRequest)
			return
		}
		existing.ConfigJSON = req.ConfigJSON
	}
	// V3-M18: Only update booleans when explicitly provided in the request
	if req.IsDefault != nil {
		existing.IsDefault = *req.IsDefault
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}

	if err := h.svc.UpdateSource(existing); err != nil {
		writeErrorLog(w, "failed to update auth source", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (h *AuthSourceHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	if err := h.svc.DeleteSource(id); err != nil {
		writeErrorLog(w, "failed to delete auth source", http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthSourceHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	if err := h.svc.TestConnection(id); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": false, "message": "connection test failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Connection successful"})
}
