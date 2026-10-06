package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/service"
)

func TestAILintEndpointWorksWithAIOff(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	h := NewAIHandler(service.NewAIAssistService(database, nil, nil))

	rec := httptest.NewRecorder()
	h.LintAction(rec, httptest.NewRequest(http.MethodPost, "/ai/actions/lint", strings.NewReader(`{"script":"apt-get install nginx\n"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var out service.ActionReview
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if !out.LintOnly || len(out.Findings) == 0 || out.Risk == "" {
		t.Errorf("lint: %+v", out)
	}
	// Review with AI off is the same lint result, not an error.
	rec = httptest.NewRecorder()
	h.ReviewAction(rec, httptest.NewRequest(http.MethodPost, "/ai/actions/review", strings.NewReader(`{"script":"set -e\necho hi\n"}`)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"lint_only":true`) {
		t.Errorf("review off: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.LintAction(rec, httptest.NewRequest(http.MethodPost, "/ai/actions/lint", strings.NewReader(`{"script":"  "}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty script: %d", rec.Code)
	}
	// Status for a non-admin carries only the switch.
	rec = httptest.NewRecorder()
	h.Status(rec, httptest.NewRequest(http.MethodGet, "/ai/status", nil))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "problem") {
		t.Errorf("status: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAIDraftEndpointIs409WhenOff(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	h := NewAIHandler(service.NewAIAssistService(database, nil, nil))
	rec := httptest.NewRecorder()
	h.DraftAction(rec, httptest.NewRequest(http.MethodPost, "/ai/actions/draft", strings.NewReader(`{"prompt":"install nginx and enable it"}`)))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "turned off") {
		t.Errorf("draft with AI off: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.DraftAction(rec, httptest.NewRequest(http.MethodPost, "/ai/actions/draft", strings.NewReader(`{"prompt":"x"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("tiny prompt: %d", rec.Code)
	}
}
