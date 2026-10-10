package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "problem") || strings.Contains(rec.Body.String(), "key_set") || strings.Contains(rec.Body.String(), "provider") {
		t.Errorf("non-admin status must carry only the switch: %d %s", rec.Code, rec.Body.String())
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

// The server's global write timeout must not cut off a slow model-backed
// request: a handler that extends its deadline answers after the global
// timeout; one that doesn't is cut off. (This was the "draft loads for a
// while, then nothing happens" bug.)
func TestExtendDeadlineOutlivesServerWriteTimeout(t *testing.T) {
	slow := func(extend bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if extend {
				extendDeadline(w)
			}
			time.Sleep(1500 * time.Millisecond)
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		}
	}
	mux := http.NewServeMux()
	mux.Handle("/extended", slow(true))
	mux.Handle("/plain", slow(false))
	srv := httptest.NewUnstartedServer(mux)
	srv.Config.WriteTimeout = 1 * time.Second
	srv.Start()
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/extended")
	if err != nil {
		t.Fatalf("extended handler must answer: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("extended: %d %s", resp.StatusCode, body)
	}
	resp, err = http.Get(srv.URL + "/plain")
	if err == nil {
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		if len(body) > 0 {
			t.Fatalf("control handler should have been cut off by the write timeout, got %s", body)
		}
	}
}
