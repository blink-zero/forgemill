package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/provider"
	"github.com/forgemill/forgemill/internal/service"
)

type TargetHandler struct {
	svc   *service.TargetService
	audit *service.AuditService
}

func NewTargetHandler(svc *service.TargetService, audit *service.AuditService) *TargetHandler {
	return &TargetHandler{svc: svc, audit: audit}
}

type createTargetRequest struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	Hostname      string `json:"hostname"`
	Port          int    `json:"port"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	// V3-M12: Use pointer type so omitted defaults to true (secure default)
	ValidateCerts *bool `json:"validate_certs,omitempty"`
}

func (h *TargetHandler) List(w http.ResponseWriter, r *http.Request) {
	targets, err := h.svc.List()
	if err != nil {
		writeErrorLog(w, "failed to list targets", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, targets)
}

func (h *TargetHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.Type == "" || req.Hostname == "" || req.Username == "" || req.Password == "" {
		writeError(w, "name, type, hostname, username, and password are required", http.StatusBadRequest)
		return
	}

	// V3-M12: Default to TLS validation enabled; only disable if explicitly set to false
	validateCerts := true
	if req.ValidateCerts != nil {
		validateCerts = *req.ValidateCerts
	}
	target := &models.Target{
		Name:          req.Name,
		Type:          req.Type,
		Hostname:      req.Hostname,
		Port:          req.Port,
		Username:      req.Username,
		ValidateCerts: validateCerts,
	}

	if err := h.svc.Create(target, req.Password); err != nil {
		writeErrorLog(w, "failed to create target", http.StatusInternalServerError, err)
		return
	}

	// M5: Audit log
	if actor := middleware.UserFromContext(r.Context()); actor != nil {
		slog.Info("audit",
			"action", "target.create",
			"actor", actor.Username,
			"target_name", target.Name,
			"target_type", target.Type,
		)
		h.audit.Log(actor.Username, &actor.ID, "target.create", "target", fmt.Sprintf("%d", target.ID), service.IPFromRequest(r), map[string]interface{}{"target_name": target.Name, "target_type": target.Type})
	}

	writeJSON(w, http.StatusCreated, target)
}

func (h *TargetHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	target, err := h.svc.Get(id)
	if err != nil {
		writeError(w, "target not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, target)
}

func (h *TargetHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}

	existing, err := h.svc.Get(id)
	if err != nil {
		writeError(w, "target not found", http.StatusNotFound)
		return
	}

	// V3-H5: Use pointer type for ValidateCerts to distinguish omitted from explicitly-false
	var req struct {
		Name          string `json:"name"`
		Type          string `json:"type"`
		Hostname      string `json:"hostname"`
		Port          int    `json:"port"`
		Username      string `json:"username"`
		Password      string `json:"password"`
		ValidateCerts *bool  `json:"validate_certs,omitempty"`
		// Platform-specific fields
		StoragePool   string `json:"storage_pool,omitempty"`
		NetworkBridge string `json:"network_bridge,omitempty"`
		Datacenter    string `json:"datacenter,omitempty"`
		Datastore     string `json:"datastore,omitempty"`
		Network       string `json:"network,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Prevent changing target type — would break provider logic for all linked resources
	if req.Type != "" && req.Type != existing.Type {
		writeError(w, "target type cannot be changed", http.StatusBadRequest)
		return
	}

	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Hostname != "" {
		existing.Hostname = req.Hostname
	}
	if req.Port > 0 {
		existing.Port = req.Port
	}
	if req.Username != "" {
		existing.Username = req.Username
	}
	// V3-H5: Only update ValidateCerts when explicitly provided in request
	if req.ValidateCerts != nil {
		existing.ValidateCerts = *req.ValidateCerts
	}
	// Platform-specific fields
	if req.StoragePool != "" {
		existing.StoragePool = req.StoragePool
	}
	if req.NetworkBridge != "" {
		existing.NetworkBridge = req.NetworkBridge
	}
	if req.Datacenter != "" {
		existing.Datacenter = req.Datacenter
	}
	if req.Datastore != "" {
		existing.Datastore = req.Datastore
	}
	if req.Network != "" {
		existing.Network = req.Network
	}

	if err := h.svc.Update(existing, req.Password); err != nil {
		writeErrorLog(w, "failed to update target", http.StatusInternalServerError, err)
		return
	}

	// M5: Audit log
	if actor := middleware.UserFromContext(r.Context()); actor != nil {
		slog.Info("audit",
			"action", "target.update",
			"actor", actor.Username,
			"target_id", existing.ID,
			"target_name", existing.Name,
		)
		h.audit.Log(actor.Username, &actor.ID, "target.update", "target", strconv.FormatInt(existing.ID, 10), service.IPFromRequest(r), map[string]interface{}{"target_name": existing.Name})
	}

	writeJSON(w, http.StatusOK, existing)
}

func (h *TargetHandler) DeletePreview(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	preview, err := h.svc.DeletePreview(id)
	if err != nil {
		writeErrorLog(w, "failed to get delete preview", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (h *TargetHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	if err := h.svc.Delete(id); err != nil {
		writeErrorLog(w, "failed to delete target", http.StatusInternalServerError, err)
		return
	}

	// M5: Audit log
	if actor := middleware.UserFromContext(r.Context()); actor != nil {
		slog.Info("audit",
			"action", "target.delete",
			"actor", actor.Username,
			"target_id", id,
		)
		h.audit.Log(actor.Username, &actor.ID, "target.delete", "target", strconv.FormatInt(id, 10), service.IPFromRequest(r), nil)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *TargetHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	// 15-second timeout for connection tests to prevent hanging
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := h.svc.TestConnection(ctx, id); err != nil {
		msg := err.Error()
		// Provide user-friendly messages for common errors
		if ctx.Err() == context.DeadlineExceeded {
			msg = "Connection timed out after 15 seconds — check hostname/port and network connectivity"
		} else if strings.Contains(msg, "connection refused") {
			msg = "Connection refused — verify the host is running and the port is correct"
		} else if strings.Contains(msg, "no such host") || strings.Contains(msg, "lookup") {
			msg = "DNS resolution failed — check the hostname"
		} else if strings.Contains(msg, "certificate") || strings.Contains(msg, "tls") {
			msg = "TLS/certificate error — try disabling certificate validation or check your certs"
		} else if strings.Contains(msg, "401") || strings.Contains(msg, "authentication") || strings.Contains(msg, "login failed") {
			msg = "Authentication failed — check username and password"
		} else if strings.Contains(msg, "403") {
			msg = "Access denied — user may lack required permissions"
		} else {
			// Catch-all: do not expose raw internal errors to the client
			slog.Warn("target connection test failed with unmatched error", "target_id", id, "error", err)
			msg = "Connection failed — check target configuration and network connectivity"
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": false, "message": msg})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Connection successful"})
}

func (h *TargetHandler) SyncTemplates(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	count, err := h.svc.SyncTemplates(r.Context(), id)
	if err != nil {
		writeErrorLog(w, "sync failed", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"templates_found": count})
}

func (h *TargetHandler) GetResources(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	resources, err := h.svc.GetResources(r.Context(), id)
	if err != nil {
		writeErrorLog(w, "failed to get resources", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, resources)
}

func parseID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

// ListTypes returns metadata for all registered provider types.
// This allows the frontend to dynamically render forms, icons, and features
// without hardcoding provider-specific logic.
func (h *TargetHandler) ListTypes(w http.ResponseWriter, r *http.Request) {
	metadata := provider.GetAllMetadata()
	writeJSON(w, http.StatusOK, map[string]interface{}{"types": metadata})
}
