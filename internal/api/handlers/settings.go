package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/service"
)

type SettingsHandler struct {
	db    *db.DB
	audit *service.AuditService
}

func NewSettingsHandler(db *db.DB, audit *service.AuditService) *SettingsHandler {
	return &SettingsHandler{db: db, audit: audit}
}

func (h *SettingsHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.db.GetAllSettings()
	if err != nil {
		writeErrorLog(w, "failed to get settings", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

// B-3: Allowlist of valid setting keys to prevent arbitrary key storage.
var allowedSettingKeys = map[string]bool{
	"motd":                  true,
	"theme":                 true,
	"session_timeout":       true,
	"default_target_id":     true,
	"audit_retention_days":  true,
}

func (h *SettingsHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body: values must be strings", http.StatusBadRequest)
		return
	}
	if len(req) == 0 {
		writeError(w, "no settings provided", http.StatusBadRequest)
		return
	}
	// HIGH-04: Validate ALL keys against allowlist before writing any
	for key := range req {
		if !allowedSettingKeys[key] {
			writeError(w, "unknown setting key: "+key, http.StatusBadRequest)
			return
		}
	}
	// F-61: Use a transaction for atomicity — all settings saved or none
	tx, err := h.db.Begin()
	if err != nil {
		writeErrorLog(w, "failed to begin transaction", http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()
	for key, value := range req {
		if _, err := tx.Exec(
			`INSERT INTO app_settings (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`,
			key, value,
		); err != nil {
			writeErrorLog(w, "failed to save setting", http.StatusInternalServerError, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeErrorLog(w, "failed to commit settings", http.StatusInternalServerError, err)
		return
	}
	// LOW-18: Audit log for settings changes
	if actor := middleware.UserFromContext(r.Context()); actor != nil {
		keys := make([]string, 0, len(req))
		for k := range req {
			keys = append(keys, k)
		}
		slog.Info("audit",
			"action", "settings.update",
			"actor", actor.Username,
			"keys", keys,
		)
		h.audit.Log(actor.Username, &actor.ID, "settings.update", "settings", "", service.IPFromRequest(r), map[string]interface{}{"keys": keys})
	}
	// Return the full settings after update
	settings, err := h.db.GetAllSettings()
	if err != nil {
		writeErrorLog(w, "failed to read settings", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (h *SettingsHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.db.ListUsers()
	if err != nil {
		writeErrorLog(w, "failed to list users", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

type createUserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

func (h *SettingsHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Username == "" || req.Password == "" {
		writeError(w, "username and password are required", http.StatusBadRequest)
		return
	}

	// H6: Password complexity requirements
	if len(req.Password) < 12 {
		writeError(w, "password must be at least 12 characters", http.StatusBadRequest)
		return
	}

	// C2: Validate role against allowed set
	role := req.Role
	if role == "" {
		role = "user"
	}
	validRoles := map[string]bool{"viewer": true, "user": true, "admin": true}
	if !validRoles[role] {
		writeError(w, "invalid role: must be viewer, user, or admin", http.StatusBadRequest)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, "failed to hash password", http.StatusInternalServerError)
		return
	}

	user := &models.User{
		Username:     req.Username,
		PasswordHash: string(hash),
		DisplayName:  req.DisplayName,
		Role:         role,
		IsActive:     true,
	}

	if err := h.db.CreateUser(user); err != nil {
		writeErrorLog(w, "failed to create user", http.StatusInternalServerError, err)
		return
	}

	// M5: Audit log
	if actor := middleware.UserFromContext(r.Context()); actor != nil {
		slog.Info("audit",
			"action", "user.create",
			"actor", actor.Username,
			"target_user", user.Username,
			"role", user.Role,
		)
		h.audit.Log(actor.Username, &actor.ID, "user.create", "user", fmt.Sprintf("%d", user.ID), service.IPFromRequest(r), map[string]interface{}{"target_user": user.Username, "role": user.Role})
	}

	writeJSON(w, http.StatusCreated, user)
}

func (h *SettingsHandler) GetDashboardStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.db.GetStats()
	if err != nil {
		writeErrorLog(w, "failed to get stats", http.StatusInternalServerError, err)
		return
	}

	recent, err := h.db.GetRecentDeployments(10)
	if err != nil {
		writeErrorLog(w, "failed to get recent deployments", http.StatusInternalServerError, err)
		return
	}

	targets, err := h.db.ListTargets()
	if err != nil {
		writeErrorLog(w, "failed to get targets", http.StatusInternalServerError, err)
		return
	}

	executions, err := h.db.ListRecentExecutions(10)
	if err != nil {
		executions = []models.ActionExecution{}
	}

	if recent == nil {
		recent = []models.Deployment{}
	}
	if targets == nil {
		targets = []models.Target{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"stats":              stats,
		"recent_deployments": recent,
		"recent_executions":  executions,
		"targets":            targets,
	})
}

func (h *SettingsHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	targetID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, "invalid user ID", http.StatusBadRequest)
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.Password) < 12 {
		writeError(w, "password must be at least 12 characters", http.StatusBadRequest)
		return
	}

	// Only admins can change other users' passwords; any user can change their own
	actor := middleware.UserFromContext(r.Context())
	if actor == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if actor.Role != "admin" && actor.ID != targetID {
		writeError(w, "forbidden: can only change your own password", http.StatusForbidden)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, "failed to hash password", http.StatusInternalServerError)
		return
	}

	if err := h.db.UpdateUserPassword(targetID, string(hash)); err != nil {
		writeErrorLog(w, "failed to update password", http.StatusInternalServerError, err)
		return
	}

	slog.Info("audit", "action", "user.password_changed", "actor", actor.Username, "target_user_id", targetID)
	h.audit.Log(actor.Username, &actor.ID, "user.password_changed", "user", strconv.FormatInt(targetID, 10), service.IPFromRequest(r), map[string]interface{}{"target_user_id": targetID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "password updated"})
}

func (h *SettingsHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	targetID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, "invalid user ID", http.StatusBadRequest)
		return
	}

	actor := middleware.UserFromContext(r.Context())
	if actor == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if actor.ID == targetID {
		writeError(w, "cannot delete your own account", http.StatusBadRequest)
		return
	}

	targetUser, err := h.db.GetUserByID(targetID)
	if err != nil {
		writeError(w, "user not found", http.StatusNotFound)
		return
	}

	// Prevent deleting the last admin
	if targetUser.Role == "admin" {
		users, err := h.db.ListUsers()
		if err != nil {
			writeErrorLog(w, "failed to list users", http.StatusInternalServerError, err)
			return
		}
		adminCount := 0
		for _, u := range users {
			if u.Role == "admin" {
				adminCount++
			}
		}
		if adminCount == 1 {
			writeError(w, "cannot delete the last admin account", http.StatusBadRequest)
			return
		}
	}

	if err := h.db.DeleteUser(targetID); err != nil {
		writeErrorLog(w, "failed to delete user", http.StatusInternalServerError, err)
		return
	}

	slog.Info("audit", "action", "user.delete", "actor", actor.Username, "target_user", targetUser.Username)
	h.audit.Log(actor.Username, &actor.ID, "user.delete", "user", strconv.FormatInt(targetID, 10), service.IPFromRequest(r), map[string]interface{}{"target_user": targetUser.Username})
	w.WriteHeader(http.StatusNoContent)
}

func (h *SettingsHandler) UpdateUserRole(w http.ResponseWriter, r *http.Request) {
	targetID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, "invalid user ID", http.StatusBadRequest)
		return
	}

	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	actor := middleware.UserFromContext(r.Context())
	if actor == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// Prevent admin from demoting themselves
	if actor.ID == targetID {
		writeError(w, "cannot change your own role", http.StatusBadRequest)
		return
	}

	// Get old role for audit log
	targetUser, err := h.db.GetUserByID(targetID)
	if err != nil {
		writeError(w, "user not found", http.StatusNotFound)
		return
	}
	oldRole := targetUser.Role

	if err := h.db.UpdateUserRole(targetID, req.Role); err != nil {
		if strings.Contains(err.Error(), "invalid role") {
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeErrorLog(w, "failed to update user role", http.StatusInternalServerError, err)
		return
	}

	slog.Info("audit", "action", "user.role_change", "actor", actor.Username, "target_user", targetUser.Username, "old_role", oldRole, "new_role", req.Role)
	h.audit.Log(actor.Username, &actor.ID, "user.role_change", "user", strconv.FormatInt(targetID, 10), service.IPFromRequest(r), map[string]interface{}{
		"target_user": targetUser.Username,
		"old_role":    oldRole,
		"new_role":    req.Role,
	})

	writeJSON(w, http.StatusOK, map[string]string{"role": req.Role})
}

func (h *SettingsHandler) ClearDeploymentHistory(w http.ResponseWriter, r *http.Request) {
	count, err := h.db.ClearDeploymentHistory()
	if err != nil {
		writeErrorLog(w, "failed to clear deployment history", http.StatusInternalServerError, err)
		return
	}
	if actor := middleware.UserFromContext(r.Context()); actor != nil {
		slog.Info("audit", "action", "deployment_history.clear", "actor", actor.Username, "deleted", count)
		h.audit.Log(actor.Username, &actor.ID, "deployment_history.clear", "deployment", "", service.IPFromRequest(r), map[string]interface{}{"deleted": count})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"deleted": count})
}
