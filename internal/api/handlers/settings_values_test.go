package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forgemill/forgemill/internal/db"
)

func TestUpdateSettingsRejectsInvalidAdoptionRole(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	h := NewSettingsHandler(database, nil)

	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.UpdateSettings(rec, httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(body)))
		return rec
	}
	if rec := post(`{"vm_adoption_role":"root"}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid value") {
		t.Errorf("invalid role: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post(`{"vm_adoption_role":"user"}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"vm_adoption_role":"user"`) {
		t.Errorf("valid role: %d %s", rec.Code, rec.Body.String())
	}
}
