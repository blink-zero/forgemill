package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListModelsBothProviders(t *testing.T) {
	var path, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-sonnet-5","display_name":"Claude Sonnet 5"},{"id":"claude-haiku-4-5-20251001","display_name":"Claude Haiku 4.5"}]}`))
		case "/compat/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4.1"},{"id":"text-embedding-3-small"},{"id":"whisper-1"},{"id":"llama3.1:8b"}]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	a, _ := New(Config{Provider: ProviderAnthropic, BaseURL: srv.URL, Model: "x", APIKey: "k", AllowPrivateEndpoint: true})
	models, err := a.(ModelLister).ListModels(context.Background())
	if err != nil || len(models) != 2 || models[0].ID != "claude-haiku-4-5-20251001" || models[1].Name != "Claude Sonnet 5" || path != "/v1/models" {
		t.Fatalf("anthropic: %+v err %v path %s", models, err, path)
	}
	o, _ := New(Config{Provider: ProviderOpenAI, BaseURL: srv.URL + "/compat", Model: "x", APIKey: "k", AllowPrivateEndpoint: true})
	models, err = o.(ModelLister).ListModels(context.Background())
	if err != nil || len(models) != 2 || models[0].ID != "gpt-4.1" || models[1].ID != "llama3.1:8b" || auth != "Bearer k" {
		t.Fatalf("openai: %+v err %v auth %q", models, err, auth)
	}
}
