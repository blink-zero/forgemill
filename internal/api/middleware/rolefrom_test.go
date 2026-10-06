package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/forgemill/forgemill/internal/db/models"
)

func TestRequireRoleFromReadsTheMinimumRolePerRequest(t *testing.T) {
	m := &AuthMiddleware{}
	minRole := "admin"
	h := m.RequireRoleFrom(func() string { return minRole }, "admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	as := func(role string) int {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, &models.User{ID: 1, Username: "u", Role: role}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if as("user") != http.StatusForbidden || as("admin") != 204 {
		t.Errorf("admin-only: user=%d admin=%d", as("user"), as("admin"))
	}
	minRole = "user" // the setting was loosened — no restart, no re-wiring
	if as("user") != 204 || as("viewer") != http.StatusForbidden {
		t.Errorf("operators allowed: user=%d viewer=%d", as("user"), as("viewer"))
	}
	minRole = "garbage" // an unknown value falls back to the safe default
	if as("user") != http.StatusForbidden || as("admin") != 204 {
		t.Errorf("fallback to admin: user=%d admin=%d", as("user"), as("admin"))
	}
}
