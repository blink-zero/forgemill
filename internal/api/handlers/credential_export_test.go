package handlers

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/crypto"
	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/service"
)

const exportTestKey = "0123456789abcdef0123456789abcdef"
const exportTestJWT = "jwt-secret-that-is-at-least-32-bytes-long"

// exportHarness mounts the handler behind the same middleware chain the
// router uses, so the role, scope and session checks are exercised for
// real rather than stubbed.
type exportHarness struct {
	t        *testing.T
	db       *db.DB
	auth     *middleware.AuthMiddleware
	audit    *service.AuditService
	h        *CredentialExportHandler
	router   http.Handler
	admin    *models.User
	admin2   *models.User
	viewer   *models.User
	ldapUser *models.User
	vmID     int64
}

func newExportHarness(t *testing.T) *exportHarness {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "export.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	enc, err := crypto.NewEncryptor(exportTestKey)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := middleware.NewAuthMiddleware(database, exportTestJWT, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("correct horse"), bcrypt.MinCost)
	admin := &models.User{Username: "alice", Role: "admin", IsActive: true, PasswordHash: string(hash)}
	admin2 := &models.User{Username: "bob", Role: "admin", IsActive: true, PasswordHash: string(hash)}
	viewer := &models.User{Username: "vic", Role: "viewer", IsActive: true, PasswordHash: string(hash)}
	for _, u := range []*models.User{admin, admin2, viewer} {
		if err := database.CreateUser(u); err != nil {
			t.Fatal(err)
		}
	}
	// An LDAP account whose local hash happens to match: the handler must
	// never consult it.
	ldapUser := &models.User{Username: "lara", Role: "admin", IsActive: true, PasswordHash: string(hash), ExternalID: "uid=lara,dc=example"}
	if err := database.CreateLDAPUser(ldapUser); err != nil {
		t.Fatal(err)
	}

	encPW, _ := enc.Encrypt("target-secret")
	target := &models.Target{Name: "lab", Type: "esxi", Hostname: "esxi.example.com", Port: 443, Username: "root", PasswordEncrypt: encPW}
	if err := database.CreateTarget(target); err != nil {
		t.Fatal(err)
	}
	vm := &models.ManagedVM{TargetID: target.ID, VMName: "web-01", VMRef: "vm-1", PowerState: "poweredOn", IPAddress: "10.0.0.5"}
	if err := database.CreateManagedVM(vm); err != nil {
		t.Fatal(err)
	}
	secret, _ := enc.Encrypt("vm-password")
	if err := database.SetVMCredential(&models.VMCredential{VMID: vm.ID, Username: "root", Kind: models.CredentialKindPassword, SecretEnc: secret}); err != nil {
		t.Fatal(err)
	}

	audit := service.NewAuditService(database)
	t.Cleanup(audit.Stop)
	h := NewCredentialExportHandler(database, service.NewCredentialExportService(database, enc, "test"), audit)
	h.SetNotifications(service.NewNotificationService(database))

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(auth.Authenticate)
		r.Use(auth.RequireRole("admin"))
		r.Use(auth.RequireScope("full"))
		r.Get("/settings/credentials/export/preview", h.Preview)
		r.With(auth.RequireInteractiveSession).Post("/settings/credentials/export", h.Export)
	})
	return &exportHarness{t: t, db: database, auth: auth, audit: audit, h: h, router: r, admin: admin, admin2: admin2, viewer: viewer, ldapUser: ldapUser, vmID: vm.ID}
}

func (x *exportHarness) token(u *models.User) string {
	tok, err := x.auth.GenerateToken(u)
	if err != nil {
		x.t.Fatal(err)
	}
	return "Bearer " + tok
}

func (x *exportHarness) apiKey(u *models.User, scope string) string {
	raw := make([]byte, 16)
	rand.Read(raw)
	key := "fm_" + hex.EncodeToString(raw)
	hash, _ := bcrypt.GenerateFromPassword([]byte(key), bcrypt.MinCost)
	k := &models.APIKey{UserID: u.ID, Name: "k", KeyHash: string(hash), Prefix: key[:11]}
	if scope != "" {
		k.Scope = &scope
	}
	if err := x.db.CreateAPIKey(k); err != nil {
		x.t.Fatal(err)
	}
	return "Bearer " + key
}

func (x *exportHarness) post(authz, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/settings/credentials/export", strings.NewReader(body))
	req.Header.Set("Authorization", authz)
	req.Header.Set("User-Agent", "test-agent")
	req.RemoteAddr = "192.0.2.10:4242"
	rec := httptest.NewRecorder()
	x.router.ServeHTTP(rec, req)
	return rec
}

// auditActions returns the entries with exactly this action (the store's
// filter is a prefix match, so "credentials.export" would also return the
// denied entries).
func (x *exportHarness) auditActions(action string) []models.AuditLog {
	page, err := x.db.ListAuditLogs(db.AuditLogFilter{Action: action, PageSize: 100})
	if err != nil {
		x.t.Fatal(err)
	}
	var out []models.AuditLog
	for _, e := range page.Logs {
		if e.Action == action {
			out = append(out, e)
		}
	}
	return out
}

const goodBody = `{"password":"correct horse","format":"1pux","scope":{"all":true}}`

func TestCredentialExportRefusesNonAdminsAndAPIKeys(t *testing.T) {
	x := newExportHarness(t)
	if rec := x.post(x.token(x.viewer), goodBody); rec.Code != http.StatusForbidden {
		t.Errorf("viewer: %d %s", rec.Code, rec.Body)
	}
	if rec := x.post(x.apiKey(x.admin, "full"), goodBody); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "interactive_session_required") {
		t.Errorf("full-scope admin API key: %d %s", rec.Code, rec.Body)
	}
	if rec := x.post("", goodBody); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: %d", rec.Code)
	}
	if n := len(x.auditActions("credentials.export")); n != 0 {
		t.Errorf("nothing should have been exported, audit has %d entries", n)
	}
	// The preview is counts only and may be read by a full-scope admin
	// key; it sits in the admin group, so a narrower key is refused.
	preview := func(authz string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/settings/credentials/export/preview", nil)
		req.Header.Set("Authorization", authz)
		rec := httptest.NewRecorder()
		x.router.ServeHTTP(rec, req)
		return rec
	}
	if rec := preview(x.apiKey(x.admin, "full")); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"exportable":1`) {
		t.Errorf("preview via full-scope key: %d %s", rec.Code, rec.Body)
	}
	if rec := preview(x.apiKey(x.admin, "read-only")); rec.Code != http.StatusForbidden {
		t.Errorf("preview via read-only key: %d %s", rec.Code, rec.Body)
	}
	if rec := preview(x.token(x.viewer)); rec.Code != http.StatusForbidden {
		t.Errorf("preview as viewer: %d", rec.Code)
	}
}

func TestCredentialExportWrongPasswordIsDeniedAuditedAndLockedOut(t *testing.T) {
	x := newExportHarness(t)
	bad := `{"password":"wrong","format":"1pux","scope":{"all":true}}`
	for i := 0; i < stepUpMaxFailures; i++ {
		rec := x.post(x.token(x.admin), bad)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"code":"password"`) {
			t.Fatalf("attempt %d: %d %s", i+1, rec.Code, rec.Body)
		}
	}
	// Locked out now, even with the right password.
	rec := x.post(x.token(x.admin), goodBody)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("lockout: %d %s", rec.Code, rec.Body)
	}
	denied := x.auditActions("credentials.export.denied")
	if len(denied) != stepUpMaxFailures+1 {
		t.Fatalf("want %d denied entries, got %d", stepUpMaxFailures+1, len(denied))
	}
	var passwordReasons, rateReasons int
	for _, e := range denied {
		switch {
		case strings.Contains(string(e.Metadata), `"password"`):
			passwordReasons++
		case strings.Contains(string(e.Metadata), `"rate_limited"`):
			rateReasons++
		}
		if e.Actor != "alice" || e.IPAddress != "192.0.2.10" {
			t.Errorf("denied entry: %+v", e)
		}
	}
	if passwordReasons != stepUpMaxFailures || rateReasons != 1 {
		t.Errorf("reasons: password %d rate %d", passwordReasons, rateReasons)
	}
	// Another admin is not affected by alice's lockout.
	if rec := x.post(x.token(x.admin2), goodBody); rec.Code != http.StatusOK {
		t.Errorf("bob: %d %s", rec.Code, rec.Body)
	}
	// Once the window passes alice is let back in.
	x.h.lockout.now = func() time.Time { return time.Now().Add(stepUpWindow + time.Second) }
	if rec := x.post(x.token(x.admin), goodBody); rec.Code != http.StatusOK {
		t.Errorf("after window: %d %s", rec.Code, rec.Body)
	}
}

func TestCredentialExportLDAPUserNeverFallsBackToLocalHash(t *testing.T) {
	x := newExportHarness(t)
	// No LDAP service configured: refuse, do not consult the local hash
	// even though it would match.
	rec := x.post(x.token(x.ldapUser), goodBody)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"code":"password"`) {
		t.Fatalf("ldap user: %d (content-type %s)", rec.Code, rec.Header().Get("Content-Type"))
	}
	if n := len(x.auditActions("credentials.export")); n != 0 {
		t.Errorf("ldap user exported: %d audit entries", n)
	}
}

func TestCredentialExportSuccessAuditsNotifiesAndStreamsArchive(t *testing.T) {
	x := newExportHarness(t)
	rec := x.post(x.token(x.admin), `{"password":"correct horse","format":"1pux","scope":{"all":true},"include_sudo_passwords":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("export: %d %s", rec.Code, rec.Body)
	}
	h := rec.Header()
	if h.Get("Content-Type") != "application/zip" || !strings.HasPrefix(h.Get("Content-Disposition"), `attachment; filename="forgemill-credentials-`) || h.Get("Cache-Control") != "no-store" || h.Get("X-Forgemill-Exported") != "1" {
		t.Errorf("headers: %v", h)
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil || len(zr.File) != 3 {
		t.Fatalf("archive: %v, %d entries", err, len(zr.File))
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("export.data")) {
		t.Error("export.data missing")
	}

	entries := x.auditActions("credentials.export")
	if len(entries) != 1 {
		t.Fatalf("want one audit entry, got %d", len(entries))
	}
	e := entries[0]
	meta := string(e.Metadata)
	if e.Actor != "alice" || e.ResourceType != "credentials" || e.IPAddress != "192.0.2.10" ||
		!strings.Contains(meta, `"format":"1pux"`) || !strings.Contains(meta, `"vm_count":1`) ||
		!strings.Contains(meta, `"include_sudo_passwords":true`) || !strings.Contains(meta, `"user_agent":"test-agent"`) ||
		!strings.Contains(meta, `"scope":{"all":true}`) {
		t.Errorf("audit entry: %+v %s", e, meta)
	}
	if strings.Contains(meta, "vm-password") {
		t.Error("audit metadata must not contain the password")
	}

	// Every admin was told, the viewer was not.
	for _, u := range []*models.User{x.admin, x.admin2} {
		ns, err := x.db.ListNotificationsForUser(u.ID, false, 10)
		if err != nil || len(ns) != 1 || ns[0].Event != "credentials.exported" || ns[0].Level != "warning" || !strings.Contains(ns[0].Body, "alice exported") {
			t.Errorf("%s notifications: %+v err %v", u.Username, ns, err)
		}
	}
	if ns, _ := x.db.ListNotificationsForUser(x.viewer.ID, false, 10); len(ns) != 0 {
		t.Errorf("viewer should not be notified: %+v", ns)
	}
}

func TestCredentialExportValidationAndEmptyScope(t *testing.T) {
	x := newExportHarness(t)
	if rec := x.post(x.token(x.admin), `{"password":"correct horse","format":"xlsx"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad format: %d %s", rec.Code, rec.Body)
	}
	if rec := x.post(x.token(x.admin), `not json`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad json: %d", rec.Code)
	}
	if rec := x.post(x.token(x.admin), `{"password":"correct horse","format":"csv","scope":{"vm_ids":[999]}}`); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("empty scope: %d %s", rec.Code, rec.Body)
	}
	// Format validation happens before the password is checked, so a typo
	// in the form does not burn a lockout attempt.
	if n := len(x.auditActions("credentials.export.denied")); n != 0 {
		t.Errorf("denied entries after validation failures: %d", n)
	}
	// CSV path end to end.
	rec := x.post(x.token(x.admin), `{"password":"correct horse","format":"csv","scope":{"all":true}}`)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") || !strings.Contains(rec.Body.String(), "web-01 (lab),ssh://10.0.0.5,root,vm-password,") {
		t.Errorf("csv: %d %s %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
}

func TestCredentialExportRefusedWhenAuditCannotBeWritten(t *testing.T) {
	x := newExportHarness(t)
	// An audit service with no database behind it cannot persist anything.
	x.h.audit = service.NewAuditService(nil)
	rec := x.post(x.token(x.admin), goodBody)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "audit") || rec.Header().Get("Content-Disposition") != "" {
		t.Fatalf("audit failure must refuse the export: %d %s", rec.Code, rec.Body)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("vm-password")) {
		t.Error("secret leaked despite audit failure")
	}
}

func TestStepUpLockoutWindow(t *testing.T) {
	l := newStepUpLockout(2, time.Minute)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := base
	l.now = func() time.Time { return now }
	l.fail(1)
	if b, _ := l.blocked(1); b {
		t.Fatal("one failure must not block")
	}
	l.fail(1)
	b, left := l.blocked(1)
	if !b || left <= 0 || left > time.Minute {
		t.Fatalf("blocked %v left %v", b, left)
	}
	if b, _ := l.blocked(2); b {
		t.Fatal("other users unaffected")
	}
	now = base.Add(61 * time.Second)
	if b, _ := l.blocked(1); b {
		t.Fatal("failures outside the window must expire")
	}
	l.fail(1)
	l.fail(1)
	l.reset(1)
	if b, _ := l.blocked(1); b {
		t.Fatal("reset must clear the counter")
	}
}
