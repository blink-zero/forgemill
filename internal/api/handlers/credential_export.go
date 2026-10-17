package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/service"
)

// CredentialExportHandler serves Settings → Credentials: a counts-only
// preview and the export itself. The export is the one place Forgemill
// hands out secrets in bulk, so it is gated harder than anything else:
// admin role, an interactive session (no API keys), the caller's own
// password re-entered on every request, a per-user lockout, and an audit
// entry written synchronously before the first byte of the file.
type CredentialExportHandler struct {
	db            *db.DB
	svc           *service.CredentialExportService
	audit         *service.AuditService
	notifications *service.NotificationService
	webhooks      *service.WebhookService
	ldap          *service.LDAPService
	lockout       *stepUpLockout
}

func NewCredentialExportHandler(database *db.DB, svc *service.CredentialExportService, audit *service.AuditService) *CredentialExportHandler {
	return &CredentialExportHandler{db: database, svc: svc, audit: audit, lockout: newStepUpLockout(stepUpMaxFailures, stepUpWindow)}
}

func (h *CredentialExportHandler) SetNotifications(n *service.NotificationService) {
	h.notifications = n
}
func (h *CredentialExportHandler) SetWebhooks(w *service.WebhookService) { h.webhooks = w }
func (h *CredentialExportHandler) SetLDAP(l *service.LDAPService)        { h.ldap = l }

// --- per-user lockout --------------------------------------------------------

const (
	stepUpMaxFailures = 5
	stepUpWindow      = 15 * time.Minute
)

// stepUpLockout counts wrong passwords per user. The IP limiter on the
// route bounds a stranger; this bounds a stolen session trying passwords
// from many addresses.
type stepUpLockout struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	failures map[int64][]time.Time
	now      func() time.Time
}

func newStepUpLockout(max int, window time.Duration) *stepUpLockout {
	return &stepUpLockout{max: max, window: window, failures: map[int64][]time.Time{}, now: time.Now}
}

// blocked reports whether the user is locked out and, if so, for how long.
func (l *stepUpLockout) blocked(userID int64) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	recent := l.prune(userID)
	if len(recent) < l.max {
		return false, 0
	}
	return true, recent[0].Add(l.window).Sub(l.now())
}

func (l *stepUpLockout) fail(userID int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures[userID] = append(l.prune(userID), l.now())
}

func (l *stepUpLockout) reset(userID int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, userID)
}

// prune drops failures older than the window; caller holds the lock.
func (l *stepUpLockout) prune(userID int64) []time.Time {
	cutoff := l.now().Add(-l.window)
	kept := l.failures[userID][:0]
	for _, t := range l.failures[userID] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.failures, userID)
		return nil
	}
	l.failures[userID] = kept
	return kept
}

// --- requests ----------------------------------------------------------------

type exportScopeRequest struct {
	All      bool    `json:"all"`
	TargetID int64   `json:"target_id"`
	VMIDs    []int64 `json:"vm_ids"`
}

type exportRequest struct {
	Password                string             `json:"password"`
	Format                  string             `json:"format"`
	Scope                   exportScopeRequest `json:"scope"`
	IncludeSudoPasswords    bool               `json:"include_sudo_passwords"`
	IncludePrivateKeys      bool               `json:"include_private_keys"`
	IncludeDeploymentLogins *bool              `json:"include_deployment_logins"`
}

func (r exportRequest) scope() service.ExportScope {
	return service.ExportScope{TargetID: r.Scope.TargetID, VMIDs: r.Scope.VMIDs}
}

func (r exportRequest) options() service.ExportOptions {
	inc := true
	if r.IncludeDeploymentLogins != nil {
		inc = *r.IncludeDeploymentLogins
	}
	return service.ExportOptions{
		Format:                  strings.ToLower(strings.TrimSpace(r.Format)),
		IncludeSudoPasswords:    r.IncludeSudoPasswords,
		IncludePrivateKeys:      r.IncludePrivateKeys,
		IncludeDeploymentLogins: inc,
	}
}

// scopeFromQuery reads the preview's scope from ?target_id= and ?vm_ids=1,2.
func scopeFromQuery(r *http.Request) (service.ExportScope, error) {
	var s service.ExportScope
	if v := r.URL.Query().Get("target_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			return s, fmt.Errorf("invalid target_id")
		}
		s.TargetID = id
	}
	if v := r.URL.Query().Get("vm_ids"); v != "" {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := strconv.ParseInt(part, 10, 64)
			if err != nil || id <= 0 {
				return s, fmt.Errorf("invalid vm_ids")
			}
			s.VMIDs = append(s.VMIDs, id)
		}
	}
	return s, nil
}

func scopeMetadata(s service.ExportScope) map[string]interface{} {
	switch {
	case s.TargetID != 0:
		return map[string]interface{}{"target_id": s.TargetID}
	case len(s.VMIDs) > 0:
		return map[string]interface{}{"vm_ids": s.VMIDs}
	default:
		return map[string]interface{}{"all": true}
	}
}

// Preview returns counts for the selected scope. No secret is decrypted.
// GET /api/settings/credentials/export/preview
func (h *CredentialExportHandler) Preview(w http.ResponseWriter, r *http.Request) {
	scope, err := scopeFromQuery(r)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	opts := service.ExportOptions{IncludeDeploymentLogins: r.URL.Query().Get("include_deployment_logins") != "false"}
	p, err := h.svc.Preview(scope, opts)
	if err != nil {
		writeErrorLog(w, "failed to build export preview", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// Export verifies the caller's password and returns the file.
// POST /api/settings/credentials/export
func (h *CredentialExportHandler) Export(w http.ResponseWriter, r *http.Request) {
	actor := middleware.UserFromContext(r.Context())
	if actor == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// The router enforces this too; the handler repeats it so a future
	// re-mount cannot silently drop the check.
	if middleware.AuthMethodFromContext(r.Context()) != middleware.AuthMethodJWT {
		writeError(w, "credential export requires an interactive session", http.StatusForbidden)
		return
	}
	ip := service.IPFromRequest(r)

	var req exportRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	opts := req.options()
	if opts.Format != service.ExportFormat1PUX && opts.Format != service.ExportFormatCSV {
		writeError(w, "format must be \"1pux\" or \"csv\"", http.StatusBadRequest)
		return
	}
	scope := req.scope()

	if blocked, left := h.lockout.blocked(actor.ID); blocked {
		h.denied(actor.Username, actor.ID, ip, "rate_limited")
		w.Header().Set("Retry-After", strconv.Itoa(int(left.Seconds())+1))
		writeError(w, "too many failed password attempts; try again later", http.StatusTooManyRequests)
		return
	}

	// Same bounds as login: bcrypt truncates at 72 bytes.
	if len(req.Password) == 0 || len(req.Password) > 72 || !h.passwordMatches(actor, req.Password) {
		h.lockout.fail(actor.ID)
		h.denied(actor.Username, actor.ID, ip, "password")
		// 403 with a code rather than 401: the UI treats any 401 as an
		// expired session and reloads to the login page, which would throw
		// the admin's form away for a typo.
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "password not accepted", "code": "password"})
		return
	}
	h.lockout.reset(actor.ID)

	buf, res, err := h.svc.ExportToBuffer(r.Context(), scope, opts, actor.Username)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrExportEmpty):
			writeError(w, err.Error(), http.StatusUnprocessableEntity)
		case errors.Is(err, service.ErrExportTooLarge):
			writeError(w, err.Error(), http.StatusUnprocessableEntity)
		default:
			writeErrorLog(w, "failed to build credential export", http.StatusInternalServerError, err)
		}
		return
	}

	// Record it before anything leaves. An export that cannot be recorded
	// does not happen.
	meta := map[string]interface{}{
		"format":                    res.Format,
		"scope":                     scopeMetadata(scope),
		"vm_count":                  res.Exported,
		"skipped":                   res.Skipped,
		"vm_ids":                    res.VMIDs,
		"include_sudo_passwords":    opts.IncludeSudoPasswords,
		"include_private_keys":      opts.IncludePrivateKeys,
		"include_deployment_logins": opts.IncludeDeploymentLogins,
		"user_agent":                r.UserAgent(),
	}
	if err := h.audit.LogSync(actor.Username, &actor.ID, "credentials.export", "credentials", "", ip, meta); err != nil {
		writeErrorLog(w, "credential export refused: audit entry could not be written", http.StatusInternalServerError, err)
		return
	}
	slog.Info("credentials exported", "actor", actor.Username, "format", res.Format, "vm_count", res.Exported, "skipped", res.Skipped, "ip", ip)

	if h.notifications != nil {
		h.notifications.EmitForAdmins("warning", "Credentials exported",
			fmt.Sprintf("%s exported the SSH logins of %d VM(s) as %s.", actor.Username, res.Exported, strings.ToUpper(res.Format)),
			"/settings?tab=credentials", "credentials.exported")
	}
	if h.webhooks != nil {
		h.webhooks.FireTemplateEvent("credentials.exported", map[string]interface{}{
			"actor":    actor.Username,
			"actor_id": actor.ID,
			"format":   res.Format,
			"vm_count": res.Exported,
			"scope":    scopeMetadata(scope),
		})
	}

	w.Header().Set("Content-Type", res.ContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+res.Filename+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Forgemill-Exported", strconv.Itoa(res.Exported))
	w.Header().Set("X-Forgemill-Skipped", strconv.Itoa(res.Skipped))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(buf.Bytes()); err != nil {
		slog.Warn("credential export: client went away mid-response", "actor", actor.Username, "error", err)
	}
}

// passwordMatches checks the caller's own password: bcrypt for local
// accounts, a bind for LDAP accounts. It never falls through from one to
// the other, so an LDAP user with an empty local hash cannot pass with an
// empty password and a local user cannot be verified by a directory.
func (h *CredentialExportHandler) passwordMatches(actor *models.User, password string) bool {
	if actor.ExternalID != "" {
		if h.ldap == nil {
			return false
		}
		u, err := h.ldap.Authenticate(actor.Username, password)
		return err == nil && u != nil && u.ID == actor.ID
	}
	if actor.PasswordHash == "" {
		bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(actor.PasswordHash), []byte(password)) == nil
}

func (h *CredentialExportHandler) denied(actor string, actorID int64, ip, reason string) {
	if err := h.audit.LogSync(actor, &actorID, "credentials.export.denied", "credentials", "", ip, map[string]interface{}{"reason": reason}); err != nil {
		slog.Error("audit: could not record denied credential export", "actor", actor, "reason", reason, "error", err)
	}
}
