package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/forgemill/forgemill/internal/ai"
	"github.com/forgemill/forgemill/internal/db"
)

// AI assistance: optional, admin-configured, and only ever asked to read
// and draft. This file holds configuration and the connection test; the
// action review and draft features build on it.

// Setting keys. The API key is write-only: callers send ai_api_key, the
// handler stores ai_api_key_enc (AES-256) and never returns either.
const (
	SettingAIEnabled              = "ai_enabled"
	SettingAIProvider             = "ai_provider"
	SettingAIBaseURL              = "ai_base_url"
	SettingAIModel                = "ai_model"
	SettingAIAPIKey               = "ai_api_key"     // request-only
	SettingAIAPIKeyEnc            = "ai_api_key_enc" // storage-only
	SettingAIRedactHostnames      = "ai_redact_hostnames"
	SettingAIAllowPrivateEndpoint = "ai_allow_private_endpoint"
)

// ErrAIRateLimited: too many model calls in a short time.
var ErrAIRateLimited = errors.New("too many AI requests; try again in a moment")

type AIAssistService struct {
	db      *db.DB
	enc     Encryptor
	audit   *AuditService
	limiter *rate.Limiter
	jobs    *aiJobStore
	// newProvider is swapped in tests.
	newProvider func(ai.Config) (ai.Provider, error)
}

func NewAIAssistService(database *db.DB, enc Encryptor, audit *AuditService) *AIAssistService {
	return &AIAssistService{
		db:          database,
		enc:         enc,
		audit:       audit,
		limiter:     rate.NewLimiter(rate.Every(6*time.Second), 10), // 10 calls/min, burst 10
		jobs:        newAIJobStore(),
		newProvider: ai.New,
	}
}

// Config assembles the provider configuration from app_settings.
func (s *AIAssistService) Config() (ai.Config, error) {
	settings, err := s.db.GetAllSettings()
	if err != nil {
		return ai.Config{}, fmt.Errorf("read settings: %w", err)
	}
	cfg := ai.Config{
		Enabled:              settings[SettingAIEnabled] == "true",
		Provider:             settings[SettingAIProvider],
		BaseURL:              settings[SettingAIBaseURL],
		Model:                settings[SettingAIModel],
		RedactHostnames:      settings[SettingAIRedactHostnames] == "true",
		AllowPrivateEndpoint: settings[SettingAIAllowPrivateEndpoint] == "true",
		Timeout:              ai.DefaultTimeout,
	}
	if encKey := settings[SettingAIAPIKeyEnc]; encKey != "" {
		if s.enc == nil {
			return cfg, fmt.Errorf("encryption not available")
		}
		key, err := s.enc.Decrypt(encKey)
		if err != nil {
			return cfg, fmt.Errorf("decrypt AI API key: %w", err)
		}
		cfg.APIKey = key
	}
	return cfg, nil
}

// AIStatus is what any signed-in user may know: whether the buttons exist.
type AIStatus struct {
	Enabled         bool   `json:"enabled"`
	Configured      bool   `json:"configured"` // enabled and valid
	Provider        string `json:"provider,omitempty"`
	Model           string `json:"model,omitempty"`
	BaseURL         string `json:"base_url,omitempty"`
	KeySet          bool   `json:"key_set"`
	RedactHostnames bool   `json:"redact_hostnames"`
	Problem         string `json:"problem,omitempty"` // why Configured is false, for admins
}

func (s *AIAssistService) Status() AIStatus {
	cfg, err := s.Config()
	st := AIStatus{Enabled: cfg.Enabled, Provider: cfg.Provider, Model: cfg.Model, BaseURL: cfg.BaseURL, KeySet: cfg.APIKey != "", RedactHostnames: cfg.RedactHostnames}
	if err != nil {
		st.Problem = err.Error()
		return st
	}
	if !cfg.Enabled {
		return st
	}
	if err := cfg.Validate(); err != nil {
		st.Problem = strings.TrimPrefix(err.Error(), ai.ErrNotConfigured.Error()+": ")
		return st
	}
	st.Configured = true
	return st
}

// provider returns a ready provider or ErrNotConfigured.
func (s *AIAssistService) provider() (ai.Provider, ai.Config, error) {
	cfg, err := s.Config()
	if err != nil {
		return nil, cfg, err
	}
	if !cfg.Enabled {
		return nil, cfg, fmt.Errorf("%w: AI assistance is turned off in Settings → AI", ai.ErrNotConfigured)
	}
	p, err := s.newProvider(cfg)
	if err != nil {
		return nil, cfg, err
	}
	return p, cfg, nil
}

// AITestResult is Settings → Test: did a round trip work, and how fast.
type AITestResult struct {
	OK        bool   `json:"ok"`
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
	LatencyMs int64  `json:"latency_ms,omitempty"`
	Reply     string `json:"reply,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Test asks the configured model for a one-word answer. It is the only call
// that does not need Enabled, so an admin can verify before switching on.
func (s *AIAssistService) Test(ctx context.Context, actor string, actorID *int64) AITestResult {
	cfg, err := s.Config()
	if err != nil {
		return AITestResult{Error: err.Error()}
	}
	if err := cfg.Validate(); err != nil {
		return AITestResult{Provider: cfg.Provider, Model: cfg.Model, Error: strings.TrimPrefix(err.Error(), ai.ErrNotConfigured.Error()+": ")}
	}
	if err := ai.CheckEndpoint(cfg); err != nil {
		return AITestResult{Provider: cfg.Provider, Model: cfg.Model, Error: err.Error()}
	}
	if !s.limiter.Allow() {
		return AITestResult{Provider: cfg.Provider, Model: cfg.Model, Error: ErrAIRateLimited.Error()}
	}
	p, err := s.newProvider(cfg)
	if err != nil {
		return AITestResult{Provider: cfg.Provider, Model: cfg.Model, Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	start := time.Now()
	// Same request shape as review and draft (system prompt, JSON answer,
	// output cap) so a model that would refuse those refuses here too.
	var probe struct {
		OK bool `json:"ok"`
	}
	// Generous output cap: a model that reasons before it answers can spend
	// a tiny budget on thinking and return no text at all.
	resp, err := completeJSON(ctx, p, ai.Request{System: "You are a connectivity check for Forgemill. Answer with exactly this JSON object and nothing else: {\"ok\": true}", User: "ping", MaxTokens: 512, JSON: true}, &probe)
	res := AITestResult{Provider: cfg.Provider, Model: cfg.Model, LatencyMs: time.Since(start).Milliseconds()}
	if err != nil {
		res.Error = userFacingAIError(err)
	} else {
		res.OK = true
		res.Reply = "OK"
		if !probe.OK {
			res.Reply = strings.TrimSpace(resp.Text)
		}
		if resp.Model != "" {
			res.Model = resp.Model
		}
	}
	if s.audit != nil {
		s.audit.Log(actor, actorID, "ai.test", "settings", "ai", "", map[string]interface{}{
			"provider": cfg.Provider, "model": cfg.Model, "ok": res.OK, "latency_ms": res.LatencyMs, "error": res.Error,
		})
	}
	return res
}

// ValidateAISetting checks one AI setting value before it is stored.
func ValidateAISetting(key, value string) error {
	switch key {
	case SettingAIEnabled, SettingAIRedactHostnames, SettingAIAllowPrivateEndpoint:
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("%s must be true or false", key)
		}
	case SettingAIProvider:
		if value != ai.ProviderAnthropic && value != ai.ProviderOpenAI && value != "" {
			return fmt.Errorf("%s must be %q or %q", key, ai.ProviderAnthropic, ai.ProviderOpenAI)
		}
	case SettingAIBaseURL:
		if err := ai.ValidateBaseURL(value); err != nil {
			return err
		}
		if len(value) > 500 {
			return fmt.Errorf("%s is too long", key)
		}
	case SettingAIModel:
		if len(value) > 120 || strings.ContainsAny(value, " \t\r\n") {
			return fmt.Errorf("%s must be a single identifier of at most 120 characters", key)
		}
	case SettingAIAPIKey:
		if len(value) > 1024 || strings.ContainsAny(value, " \t\r\n") {
			return fmt.Errorf("API key must be a single token of at most 1024 characters")
		}
	}
	return nil
}

// ListModels asks the configured provider which models it offers, so the
// Settings page can show a picker instead of a free-text field. Needs the
// provider and (where required) a key, not Enabled.
func (s *AIAssistService) ListModels(ctx context.Context) ([]ai.ModelInfo, error) {
	cfg, err := s.Config()
	if err != nil {
		return nil, err
	}
	// Model is not needed to list models; validate the rest.
	probe := cfg
	if probe.Model == "" {
		probe.Model = "-"
	}
	if err := probe.Validate(); err != nil {
		return nil, err
	}
	if err := ai.CheckEndpoint(cfg); err != nil {
		return nil, err
	}
	p, err := s.newProvider(probe)
	if err != nil {
		return nil, err
	}
	lister, ok := p.(ai.ModelLister)
	if !ok {
		return nil, fmt.Errorf("%w: this provider cannot list models", ai.ErrNotConfigured)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return lister.ListModels(ctx)
}

func errNotConfiguredSentinel() error { return ai.ErrNotConfigured }

func isProviderError(err error) bool {
	var pe *ai.ProviderError
	return errors.As(err, &pe)
}
