package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/service"
)

// WebhookEncryptor is the interface for encrypting/decrypting webhook secrets.
// V3-M14: Webhook secrets are encrypted at rest using the same encryptor as target passwords.
type WebhookEncryptor interface {
	Encrypt(string) (string, error)
	Decrypt(string) (string, error)
}

type WebhookHandler struct {
	db           *db.DB
	allowPrivate bool
	enc          WebhookEncryptor // V3-M14
	svc          *service.WebhookService
	audit        *service.AuditService
}

func NewWebhookHandler(db *db.DB, allowPrivate bool, enc WebhookEncryptor, svc *service.WebhookService, audit *service.AuditService) *WebhookHandler {
	return &WebhookHandler{db: db, allowPrivate: allowPrivate, enc: enc, svc: svc, audit: audit}
}

type createWebhookRequest struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Events   string `json:"events"`
	Secret   string `json:"secret"`
	IsActive bool   `json:"is_active"`
}

func (h *WebhookHandler) List(w http.ResponseWriter, r *http.Request) {
	webhooks, err := h.db.ListWebhooks()
	if err != nil {
		writeErrorLog(w, "failed to list webhooks", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, webhooks)
}

func (h *WebhookHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.URL == "" || req.Events == "" {
		writeError(w, "name, url, and events are required", http.StatusBadRequest)
		return
	}

	if err := service.ValidateWebhookURL(req.URL, h.allowPrivate); err != nil {
		writeErrorLog(w, "invalid webhook URL", http.StatusBadRequest, err)
		return
	}

	// Minimum secret length for HMAC security (128 bits)
	if req.Secret != "" && len(req.Secret) < 16 {
		writeError(w, "webhook secret must be at least 16 characters", http.StatusBadRequest)
		return
	}

	// V3-M14: Encrypt webhook secret before storing
	secret := req.Secret
	if secret != "" && h.enc != nil {
		encrypted, err := h.enc.Encrypt(secret)
		if err != nil {
			writeErrorLog(w, "failed to encrypt webhook secret", http.StatusInternalServerError, err)
			return
		}
		secret = encrypted
	}

	wh := &models.Webhook{
		Name:     req.Name,
		URL:      req.URL,
		Events:   req.Events,
		Secret:   secret,
		IsActive: req.IsActive,
	}

	if err := h.db.CreateWebhook(wh); err != nil {
		writeErrorLog(w, "failed to create webhook", http.StatusInternalServerError, err)
		return
	}

	// M5: Audit log
	if actor := middleware.UserFromContext(r.Context()); actor != nil {
		slog.Info("audit",
			"action", "webhook.create",
			"actor", actor.Username,
			"webhook_name", wh.Name,
		)
		h.audit.Log(actor.Username, &actor.ID, "webhook.create", "webhook", fmt.Sprintf("%d", wh.ID), service.IPFromRequest(r), map[string]interface{}{"webhook_name": wh.Name})
	}

	// Don't expose encrypted secret in response
	wh.Secret = ""
	writeJSON(w, http.StatusCreated, wh)
}

func (h *WebhookHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	wh, err := h.db.GetWebhook(id)
	if err != nil {
		writeError(w, "webhook not found", http.StatusNotFound)
		return
	}
	// V3-M14: Don't expose encrypted secret in response
	wh.Secret = ""
	writeJSON(w, http.StatusOK, wh)
}

func (h *WebhookHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}

	existing, err := h.db.GetWebhook(id)
	if err != nil {
		writeError(w, "webhook not found", http.StatusNotFound)
		return
	}

	// V3-M18: Use pointer for IsActive to distinguish omitted from explicitly-false
	var req struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		Events   string `json:"events"`
		Secret   string `json:"secret"`
		IsActive *bool  `json:"is_active,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.URL != "" {
		if err := service.ValidateWebhookURL(req.URL, h.allowPrivate); err != nil {
			writeErrorLog(w, "invalid webhook URL", http.StatusBadRequest, err)
			return
		}
		existing.URL = req.URL
	}
	if req.Events != "" {
		existing.Events = req.Events
	}
	// V3-M14: Encrypt new secret if provided
	if req.Secret != "" {
		// Minimum secret length for HMAC security (128 bits)
		if len(req.Secret) < 16 {
			writeError(w, "webhook secret must be at least 16 characters", http.StatusBadRequest)
			return
		}
		if h.enc != nil {
			encrypted, err := h.enc.Encrypt(req.Secret)
			if err != nil {
				writeErrorLog(w, "failed to encrypt webhook secret", http.StatusInternalServerError, err)
				return
			}
			existing.Secret = encrypted
		} else {
			existing.Secret = req.Secret
		}
	}
	// V3-M18: Only update IsActive when explicitly provided in request
	if req.IsActive != nil {
		existing.IsActive = *req.IsActive
	}

	if err := h.db.UpdateWebhook(existing); err != nil {
		writeErrorLog(w, "failed to update webhook", http.StatusInternalServerError, err)
		return
	}
	if actor := middleware.UserFromContext(r.Context()); actor != nil {
		h.audit.Log(actor.Username, &actor.ID, "webhook.update", "webhook", strconv.FormatInt(id, 10), service.IPFromRequest(r), map[string]interface{}{"webhook_name": existing.Name})
	}
	// V3-M14: Don't expose encrypted secret in response
	existing.Secret = ""
	writeJSON(w, http.StatusOK, existing)
}

func (h *WebhookHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	if err := h.db.DeleteWebhook(id); err != nil {
		writeErrorLog(w, "failed to delete webhook", http.StatusInternalServerError, err)
		return
	}

	// M5: Audit log
	if actor := middleware.UserFromContext(r.Context()); actor != nil {
		slog.Info("audit",
			"action", "webhook.delete",
			"actor", actor.Username,
			"webhook_id", id,
		)
		h.audit.Log(actor.Username, &actor.ID, "webhook.delete", "webhook", strconv.FormatInt(id, 10), service.IPFromRequest(r), map[string]interface{}{"webhook_id": id})
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *WebhookHandler) Test(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}

	wh, err := h.db.GetWebhook(id)
	if err != nil {
		writeError(w, "webhook not found", http.StatusNotFound)
		return
	}

	if h.svc == nil {
		writeError(w, "webhook service not available", http.StatusInternalServerError)
		return
	}

	statusCode, err := h.svc.SendTest(wh)
	if err != nil {
		slog.Warn("webhook test failed", "webhook", wh.Name, "error", err)
		writeError(w, "webhook test failed — check the URL is reachable and returns a 2xx status", http.StatusBadGateway)
		return
	}

	if actor := middleware.UserFromContext(r.Context()); actor != nil {
		slog.Info("audit",
			"action", "webhook.test",
			"actor", actor.Username,
			"webhook_name", wh.Name,
		)
		h.audit.Log(actor.Username, &actor.ID, "webhook.test", "webhook", strconv.FormatInt(id, 10), service.IPFromRequest(r), map[string]interface{}{"webhook_name": wh.Name})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":     statusCode < 300,
		"status_code": statusCode,
	})
}
