package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forgemill/forgemill/internal/crypto"
	"github.com/forgemill/forgemill/internal/db"
)

// The AI API key is write-only: stored encrypted, reported only as set.
func TestAIAPIKeyIsWriteOnlyAndEncrypted(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	enc, err := crypto.NewEncryptor("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	h := NewSettingsHandler(database, nil)
	h.SetEncryptor(enc)

	put := func(body string) (*httptest.ResponseRecorder, map[string]string) {
		rec := httptest.NewRecorder()
		h.UpdateSettings(rec, httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(body)))
		var out map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec, out
	}
	rec, out := put(`{"ai_api_key":"sk-super-secret","ai_provider":"anthropic","ai_model":"claude-sonnet-5","ai_enabled":"true"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "sk-super-secret") || strings.Contains(rec.Body.String(), "ai_api_key_enc") {
		t.Fatalf("response must never carry the key: %s", rec.Body.String())
	}
	if out["ai_api_key_set"] != "true" || out["ai_provider"] != "anthropic" {
		t.Errorf("flags: %v", out)
	}
	stored, _ := database.GetAllSettings()
	if stored["ai_api_key_enc"] == "" || stored["ai_api_key_enc"] == "sk-super-secret" || stored["ai_api_key"] != "" {
		t.Errorf("storage: enc=%q plain=%q", stored["ai_api_key_enc"], stored["ai_api_key"])
	}
	if plain, _ := enc.Decrypt(stored["ai_api_key_enc"]); plain != "sk-super-secret" {
		t.Errorf("round trip: %q", plain)
	}
	// GET hides it too.
	rec = httptest.NewRecorder()
	h.GetSettings(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	if strings.Contains(rec.Body.String(), "sk-super-secret") || strings.Contains(rec.Body.String(), "ai_api_key_enc") || !strings.Contains(rec.Body.String(), `"ai_api_key_set":"true"`) {
		t.Errorf("get: %s", rec.Body.String())
	}
	// Empty clears.
	_, out = put(`{"ai_api_key":""}`)
	if out["ai_api_key_set"] != "false" {
		t.Errorf("clear: %v", out)
	}
	// Validation.
	if rec, _ := put(`{"ai_provider":"gemini"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad provider accepted: %d", rec.Code)
	}
	if rec, _ := put(`{"ai_base_url":"ftp://x"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad url accepted: %d", rec.Code)
	}
	// Without an encryptor the key is refused, not stored in clear.
	h2 := NewSettingsHandler(database, nil)
	rec = httptest.NewRecorder()
	h2.UpdateSettings(rec, httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(`{"ai_api_key":"x"}`)))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("no encryptor: %d", rec.Code)
	}
	if stored, _ := database.GetAllSettings(); stored["ai_api_key"] != "" {
		t.Error("key stored in clear")
	}
}
