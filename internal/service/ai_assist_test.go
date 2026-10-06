package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/forgemill/forgemill/internal/ai"
)

func newAITestService(t *testing.T) (*AIAssistService, map[string]string) {
	t.Helper()
	svc, database, _ := newNICTestService(t, "esxi")
	a := NewAIAssistService(database, svc.encryptor, nil)
	set := func(k, v string) {
		if err := database.SetSetting(k, v); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"llama3","choices":[{"message":{"content":"OK"}}]}`))
	}))
	t.Cleanup(srv.Close)
	set(SettingAIProvider, ai.ProviderOpenAI)
	set(SettingAIBaseURL, srv.URL)
	set(SettingAIModel, "llama3")
	return a, map[string]string{"url": srv.URL}
}

func TestAIStatusAndTestRespectEnabledKeyAndPrivateEndpoint(t *testing.T) {
	a, _ := newAITestService(t)
	st := a.Status()
	if st.Enabled || st.Configured || st.KeySet {
		t.Fatalf("off by default: %+v", st)
	}
	// Test works before enabling, but the loopback endpoint is refused until allowed.
	res := a.Test(context.Background(), "admin", nil)
	if res.OK || !strings.Contains(res.Error, "private") {
		t.Fatalf("private endpoint must be refused by default: %+v", res)
	}
	if err := a.db.SetSetting(SettingAIAllowPrivateEndpoint, "true"); err != nil {
		t.Fatal(err)
	}
	res = a.Test(context.Background(), "admin", nil)
	if !res.OK || res.Reply != "OK" || res.Model != "llama3" || res.LatencyMs < 0 {
		t.Fatalf("test should pass: %+v", res)
	}
	// Enabled + valid → configured; the encrypted key round-trips and is reported only as set.
	enc, _ := a.enc.Encrypt("sk-secret")
	for k, v := range map[string]string{SettingAIEnabled: "true", SettingAIAPIKeyEnc: enc} {
		if err := a.db.SetSetting(k, v); err != nil {
			t.Fatal(err)
		}
	}
	st = a.Status()
	if !st.Enabled || !st.Configured || !st.KeySet || st.Problem != "" {
		t.Fatalf("configured: %+v", st)
	}
	cfg, _ := a.Config()
	if cfg.APIKey != "sk-secret" {
		t.Errorf("key not decrypted: %q", cfg.APIKey)
	}
	// Anthropic without a key is reported as a problem, not a crash.
	_ = a.db.SetSetting(SettingAIProvider, ai.ProviderAnthropic)
	_ = a.db.SetSetting(SettingAIAPIKeyEnc, "")
	st = a.Status()
	if st.Configured || !strings.Contains(st.Problem, "API key") {
		t.Errorf("anthropic without key: %+v", st)
	}
}

func TestValidateAISetting(t *testing.T) {
	bad := map[string]string{SettingAIEnabled: "yes", SettingAIProvider: "gemini", SettingAIBaseURL: "ftp://x", SettingAIModel: "two words", SettingAIAPIKey: "has space"}
	for k, v := range bad {
		if ValidateAISetting(k, v) == nil {
			t.Errorf("%s=%q should be rejected", k, v)
		}
	}
	good := map[string]string{SettingAIEnabled: "true", SettingAIProvider: "openai", SettingAIBaseURL: "http://ollama:11434/v1", SettingAIModel: "llama3.1:8b", SettingAIAPIKey: "sk-abc", SettingAIRedactHostnames: "false"}
	for k, v := range good {
		if err := ValidateAISetting(k, v); err != nil {
			t.Errorf("%s=%q: %v", k, v, err)
		}
	}
}
