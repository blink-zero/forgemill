package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"golang.org/x/crypto/bcrypt"

	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/service"
)

type AuthHandler struct {
	db    *db.DB
	auth  *middleware.AuthMiddleware
	ldap  *service.LDAPService
	audit *service.AuditService
}

func NewAuthHandler(db *db.DB, auth *middleware.AuthMiddleware, audit *service.AuditService) *AuthHandler {
	return &AuthHandler{db: db, auth: auth, audit: audit}
}

func (h *AuthHandler) SetLDAP(ldap *service.LDAPService) {
	h.ldap = ldap
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string      `json:"token"`
	User  interface{} `json:"user"`
}

// dummyHash is a pre-computed bcrypt hash used for constant-time comparison
// when the user is not found, preventing timing-based username enumeration.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcrypt.DefaultCost)

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// SEC-10: Enforce username and password length limits to prevent DoS via bcrypt
	if len(req.Username) == 0 || len(req.Username) > 256 {
		writeError(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if len(req.Password) == 0 || len(req.Password) > 72 {
		// bcrypt silently truncates at 72 bytes; reject longer passwords
		writeError(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	// Try local auth first
	user, err := h.db.GetUserByUsername(req.Username)
	if err == nil && user.IsActive && user.ExternalID == "" {
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err == nil {
			token, err := h.auth.GenerateToken(user)
			if err != nil {
				writeError(w, "failed to generate token", http.StatusInternalServerError)
				return
			}
			if err := h.db.UpdateUserLogin(user.ID); err != nil {
				slog.Error("failed to update user login", "user_id", user.ID, "error", err)
			}
			// M5: Audit log
			slog.Info("audit",
				"action", "auth.login",
				"username", user.Username,
				"method", "local",
			)
			h.audit.Log(user.Username, &user.ID, "auth.login", "user", strconv.FormatInt(user.ID, 10), service.IPFromRequest(r), map[string]interface{}{"method": "local"})
			writeJSON(w, http.StatusOK, loginResponse{Token: token, User: user})
			return
		}
	}

	// F-48: Dummy bcrypt compare when user not found to prevent timing side-channel
	if err != nil {
		bcrypt.CompareHashAndPassword(dummyHash, []byte(req.Password))
	}

	// Try LDAP auth
	if h.ldap != nil {
		ldapUser, err := h.ldap.Authenticate(req.Username, req.Password)
		if err == nil {
			token, err := h.auth.GenerateToken(ldapUser)
			if err != nil {
				writeError(w, "failed to generate token", http.StatusInternalServerError)
				return
			}
			if err := h.db.UpdateUserLogin(ldapUser.ID); err != nil {
				slog.Error("failed to update user login", "user_id", ldapUser.ID, "error", err)
			}
			// M5: Audit log
			slog.Info("audit",
				"action", "auth.login",
				"username", ldapUser.Username,
				"method", "ldap",
			)
			h.audit.Log(ldapUser.Username, &ldapUser.ID, "auth.login", "user", strconv.FormatInt(ldapUser.ID, 10), service.IPFromRequest(r), map[string]interface{}{"method": "ldap"})
			writeJSON(w, http.StatusOK, loginResponse{Token: token, User: ldapUser})
			return
		}
	}

	// M5: Audit log for failed login
	slog.Warn("audit",
		"action", "auth.login_failed",
		"username", req.Username,
	)
	h.audit.Log(req.Username, nil, "auth.login_failed", "user", "", service.IPFromRequest(r), map[string]interface{}{"username": req.Username})

	writeError(w, "invalid credentials", http.StatusUnauthorized)
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	// V5-M5: Invalidate all existing JWTs for this user by incrementing token version
	user := middleware.UserFromContext(r.Context())
	if user != nil {
		if err := h.db.IncrementTokenVersion(user.ID); err != nil {
			slog.Error("failed to increment token version", "user_id", user.ID, "error", err)
		}
		h.audit.Log(user.Username, &user.ID, "auth.logout", "user", strconv.FormatInt(user.ID, 10), service.IPFromRequest(r), nil)
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, user)
}
