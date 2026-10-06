package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicAdapterRequestAndResponse(t *testing.T) {
	var got map[string]any
	var hdr http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path %s", r.URL.Path)
		}
		hdr = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"model":"claude-x","content":[{"type":"text","text":"{\"ok\":true}"}],"usage":{"input_tokens":12,"output_tokens":5}}`))
	}))
	defer srv.Close()
	p, err := New(Config{Provider: ProviderAnthropic, BaseURL: srv.URL, Model: "claude-x", APIKey: "k", AllowPrivateEndpoint: true})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Complete(context.Background(), Request{System: "sys", User: "hi", MaxTokens: 100, Temperature: 0.2, JSON: true})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != `{"ok":true}` || resp.Model != "claude-x" || resp.InputTokens != 12 || resp.OutputTokens != 5 {
		t.Errorf("resp %+v", resp)
	}
	if hdr.Get("x-api-key") != "k" || hdr.Get("anthropic-version") == "" {
		t.Errorf("headers %v", hdr)
	}
	if got["system"] != "sys" || got["max_tokens"].(float64) != 100 || got["model"] != "claude-x" {
		t.Errorf("body %v", got)
	}
}

func TestOpenAIAdapterRequestResponseAndErrors(t *testing.T) {
	var got map[string]any
	var auth string
	status := 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		if status != 200 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided","type":"invalid_request_error"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"model":"gpt-x","choices":[{"message":{"role":"assistant","content":"OK"}}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`))
	}))
	defer srv.Close()
	p, err := New(Config{Provider: ProviderOpenAI, BaseURL: srv.URL + "/v1/", Model: "gpt-x", APIKey: "k", AllowPrivateEndpoint: true})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Complete(context.Background(), Request{System: "s", User: "u", JSON: true})
	if err != nil || resp.Text != "OK" || resp.InputTokens != 3 {
		t.Fatalf("resp %+v err %v", resp, err)
	}
	if auth != "Bearer k" {
		t.Errorf("auth %q", auth)
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" {
		t.Errorf("messages %v", msgs)
	}
	if rf, ok := got["response_format"].(map[string]any); !ok || rf["type"] != "json_object" {
		t.Errorf("json mode not requested: %v", got["response_format"])
	}
	// Keyless (Ollama): no Authorization header.
	p2, _ := New(Config{Provider: ProviderOpenAI, BaseURL: srv.URL + "/v1", Model: "llama3", AllowPrivateEndpoint: true})
	_, _ = p2.Complete(context.Background(), Request{User: "u"})
	if auth != "" {
		t.Errorf("keyless endpoint must not send Authorization, got %q", auth)
	}
	// Upstream 401 becomes a ProviderError with the upstream message.
	status = 401
	_, err = p.Complete(context.Background(), Request{User: "u"})
	var pe *ProviderError
	if !errors.As(err, &pe) || pe.Status != 401 || !strings.Contains(pe.Message, "Incorrect API key") {
		t.Errorf("want ProviderError 401 with upstream message, got %v", err)
	}
}

func TestPrivateEndpointRefusedUnlessAllowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
	}))
	defer srv.Close()
	cfg := Config{Provider: ProviderOpenAI, BaseURL: srv.URL, Model: "m"}
	if err := CheckEndpoint(cfg); !errors.Is(err, ErrPrivateEndpoint) {
		t.Errorf("CheckEndpoint should refuse loopback by default, got %v", err)
	}
	p, _ := New(cfg)
	if _, err := p.Complete(context.Background(), Request{User: "u"}); err == nil || !strings.Contains(err.Error(), "private") {
		t.Errorf("dial should refuse loopback by default, got %v", err)
	}
	cfg.AllowPrivateEndpoint = true
	if err := CheckEndpoint(cfg); err != nil {
		t.Errorf("allowed: %v", err)
	}
	p, _ = New(cfg)
	if resp, err := p.Complete(context.Background(), Request{User: "u"}); err != nil || resp.Text != "OK" {
		t.Errorf("allowed call: %+v %v", resp, err)
	}
}

func TestConfigValidate(t *testing.T) {
	cases := map[string]Config{
		"no provider":        {Model: "m"},
		"no model":           {Provider: ProviderOpenAI},
		"anthropic no key":   {Provider: ProviderAnthropic, Model: "m"},
		"bad url scheme":     {Provider: ProviderOpenAI, Model: "m", BaseURL: "ftp://x"},
		"url with user info": {Provider: ProviderOpenAI, Model: "m", BaseURL: "https://u:p@x.example"},
	}
	for name, c := range cases {
		if err := c.Validate(); !errors.Is(err, ErrNotConfigured) {
			t.Errorf("%s: want ErrNotConfigured, got %v", name, err)
		}
	}
	if err := (Config{Provider: ProviderOpenAI, Model: "llama3", BaseURL: "http://ollama:11434/v1"}).Validate(); err != nil {
		t.Errorf("keyless openai-compatible must be valid: %v", err)
	}
	if DefaultBaseURL(ProviderAnthropic) == "" || DefaultBaseURL(ProviderOpenAI) == "" {
		t.Error("defaults missing")
	}
}
