package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/service"
)

type APIKeyHandler struct {
	db    *db.DB
	audit *service.AuditService
}

func NewAPIKeyHandler(db *db.DB, audit *service.AuditService) *APIKeyHandler {
	return &APIKeyHandler{db: db, audit: audit}
}

type createAPIKeyRequest struct {
	Name      string `json:"name"`
	ExpiresAt string `json:"expires_at"`
}

type createAPIKeyResponse struct {
	Key    string        `json:"key"`
	APIKey models.APIKey `json:"api_key"`
}

func (h *APIKeyHandler) List(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var keys []models.APIKey
	var err error
	if user.Role == "admin" {
		keys, err = h.db.ListAllAPIKeys()
	} else {
		keys, err = h.db.ListAPIKeys(user.ID)
	}
	if err != nil {
		writeErrorLog(w, "failed to list API keys", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

func (h *APIKeyHandler) Create(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req createAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		writeError(w, "name is required", http.StatusBadRequest)
		return
	}

	// L2: Enforce per-user API key limit
	count, err := h.db.CountAPIKeysByUser(user.ID)
	if err != nil {
		writeErrorLog(w, "failed to check API key count", http.StatusInternalServerError, err)
		return
	}
	if count >= 20 {
		writeError(w, "maximum of 20 API keys per user", http.StatusBadRequest)
		return
	}

	// Generate key: fm_ + 32 random hex chars
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		writeError(w, "failed to generate key", http.StatusInternalServerError)
		return
	}
	rawKey := "fm_" + hex.EncodeToString(randomBytes)
	prefix := rawKey[:11] // "fm_" + 8 chars

	hash, err := bcrypt.GenerateFromPassword([]byte(rawKey), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, "failed to hash key", http.StatusInternalServerError)
		return
	}

	apiKey := &models.APIKey{
		UserID:  user.ID,
		Name:    req.Name,
		KeyHash: string(hash),
		Prefix:  prefix,
	}

	if req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			writeError(w, "invalid expires_at format, expected RFC3339", http.StatusBadRequest)
			return
		}
		apiKey.ExpiresAt = &t
	}

	if err := h.db.CreateAPIKey(apiKey); err != nil {
		writeErrorLog(w, "failed to create API key", http.StatusInternalServerError, err)
		return
	}

	// M5: Audit log
	slog.Info("audit",
		"action", "apikey.create",
		"actor", user.Username,
		"key_name", req.Name,
	)
	h.audit.Log(user.Username, &user.ID, "api_key.create", "api_key", strconv.FormatInt(apiKey.ID, 10), service.IPFromRequest(r), map[string]interface{}{"key_name": req.Name})

	writeJSON(w, http.StatusCreated, createAPIKeyResponse{
		Key:    rawKey,
		APIKey: *apiKey,
	})
}

func (h *APIKeyHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}

	// H1: Verify ownership before deletion
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	key, err := h.db.GetAPIKeyByID(id)
	if err != nil || (key.UserID != user.ID && user.Role != "admin") {
		writeError(w, "not found", http.StatusNotFound)
		return
	}

	if err := h.db.DeleteAPIKey(id); err != nil {
		writeErrorLog(w, "failed to delete API key", http.StatusInternalServerError, err)
		return
	}

	// M5: Audit log
	slog.Info("audit",
		"action", "apikey.delete",
		"actor", user.Username,
		"key_id", id,
	)
	h.audit.Log(user.Username, &user.ID, "api_key.delete", "api_key", strconv.FormatInt(id, 10), service.IPFromRequest(r), map[string]interface{}{"key_id": id})

	w.WriteHeader(http.StatusNoContent)
}
