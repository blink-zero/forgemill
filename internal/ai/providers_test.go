package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	// The current Claude models reject `temperature`; never send it.
	if _, present := got["temperature"]; present {
		t.Error("anthropic request must not carry temperature")
	}
	// A JSON answer is requested as a forced tool call with the schema.
	tools, _ := got["tools"].([]any)
	choice, _ := got["tool_choice"].(map[string]any)
	if len(tools) != 1 || choice["type"] != "tool" || choice["name"] != "answer" {
		t.Errorf("JSON answers must be forced tool calls: tools=%v choice=%v", tools, choice)
	}
}

// The structured answer comes back as a tool_use block; its input is the
// JSON, intact even with newlines and quotes inside strings.
func TestAnthropicParsesToolUseAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"claude-x","content":[{"type":"text","text":"Here is the draft:"},{"type":"tool_use","id":"t1","name":"answer","input":{"name":"Install nginx","script":"#!/bin/bash\nset -euo pipefail\necho \"hi\"\n"}}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":2}}`))
	}))
	defer srv.Close()
	p, _ := New(Config{Provider: ProviderAnthropic, BaseURL: srv.URL, Model: "claude-x", APIKey: "k", AllowPrivateEndpoint: true})
	resp, err := p.Complete(context.Background(), Request{User: "u", JSON: true, Schema: map[string]any{"type": "object"}})
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Name   string `json:"name"`
		Script string `json:"script"`
	}
	if err := json.Unmarshal([]byte(resp.Text), &out); err != nil || out.Name != "Install nginx" || !strings.Contains(out.Script, "echo \"hi\"") {
		t.Errorf("tool_use input must be the answer: %q err %v", resp.Text, err)
	}
}

// A compatible server that refuses sampling parameters gets one retry without them.
func TestOpenAIAdapterRetriesWithoutRefusedParameters(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got map[string]any
		_ = json.NewDecoder(r.Body).Decode(&got)
		bodies = append(bodies, got)
		if _, has := got["temperature"]; has {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"message":"Unsupported parameter: 'temperature' is not supported with this model.","type":"invalid_request_error"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
	}))
	defer srv.Close()
	p, _ := New(Config{Provider: ProviderOpenAI, BaseURL: srv.URL, Model: "o-x", AllowPrivateEndpoint: true})
	resp, err := p.Complete(context.Background(), Request{User: "u", Temperature: 0.2, JSON: true})
	if err != nil || resp.Text != "OK" {
		t.Fatalf("retry: %+v %v", resp, err)
	}
	if len(bodies) != 2 {
		t.Fatalf("want 2 requests, got %d", len(bodies))
	}
	if _, has := bodies[1]["temperature"]; has {
		t.Error("retry must drop temperature")
	}
	if _, has := bodies[1]["response_format"]; has {
		t.Error("retry must drop response_format")
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

// A reasoning model that exhausts max_tokens before emitting text gets an
// error that says so, not "empty answer".
func TestAnthropicExplainsExhaustedBudget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"claude-x","content":[{"type":"thinking","thinking":"..."}],"stop_reason":"max_tokens","usage":{"input_tokens":12,"output_tokens":20}}`))
	}))
	defer srv.Close()
	p, _ := New(Config{Provider: ProviderAnthropic, BaseURL: srv.URL, Model: "claude-x", APIKey: "k", AllowPrivateEndpoint: true})
	_, err := p.Complete(context.Background(), Request{User: "ping", MaxTokens: 20})
	var pe *ProviderError
	if !errors.As(err, &pe) || !strings.Contains(pe.Message, "output budget") || !strings.Contains(pe.Message, "20 tokens") {
		t.Errorf("want budget explanation, got %v", err)
	}
}

func TestTransportMessageIsPlain(t *testing.T) {
	msg := transportMessage(errors.New(`Post "https://api.example.com/v1/messages": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`), 4*time.Minute)
	if !strings.HasPrefix(msg, "no answer within 4m0s") || strings.Contains(msg, "Client.Timeout") {
		t.Errorf("timeout message: %q", msg)
	}
	if msg := transportMessage(errors.New("dial tcp: lookup api.nope: no such host"), time.Minute); !strings.Contains(msg, "does not resolve") {
		t.Errorf("dns: %q", msg)
	}
}
