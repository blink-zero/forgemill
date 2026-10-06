// Package ai is Forgemill's model-provider layer: a tiny interface, two
// adapters (Anthropic Messages, OpenAI-compatible Chat Completions — the
// latter covers OpenAI, Azure-style gateways, OpenRouter, vLLM, LM Studio and
// Ollama), redaction of what leaves the box, and an outbound HTTP client
// that refuses private endpoints unless told otherwise. It knows nothing
// about Forgemill's own types; the service layer builds prompts and parses
// answers.
package ai

import (
	"context"
	"errors"
	"fmt"
)

// Request is one completion call.
type Request struct {
	System      string  // instructions; may be empty
	User        string  // the content to act on
	MaxTokens   int     // output cap; adapters apply a default when 0
	Temperature float64 // 0..1
	JSON        bool    // ask for a JSON object answer where the API supports it
}

// Response is what came back.
type Response struct {
	Text         string
	Model        string
	InputTokens  int
	OutputTokens int
}

// Provider completes a request against one configured model.
type Provider interface {
	Name() string
	Complete(ctx context.Context, req Request) (*Response, error)
}

// ErrNotConfigured: AI assistance is off or has no usable configuration.
var ErrNotConfigured = errors.New("AI assistance is not configured")

// ProviderError is what the upstream API said when it refused or failed.
// Handlers map it to a 502 with Message; Status is the upstream HTTP status
// (0 for transport errors).
type ProviderError struct {
	Provider string
	Status   int
	Message  string
}

func (e *ProviderError) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("%s returned HTTP %d: %s", e.Provider, e.Status, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Provider, e.Message)
}

const (
	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai" // any OpenAI-compatible endpoint
)

// New returns the adapter for cfg. cfg must be Valid.
func New(cfg Config) (Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	client := newHTTPClient(cfg)
	switch cfg.Provider {
	case ProviderAnthropic:
		return &anthropic{cfg: cfg, http: client}, nil
	case ProviderOpenAI:
		return &openAI{cfg: cfg, http: client}, nil
	}
	return nil, fmt.Errorf("%w: unknown provider %q", ErrNotConfigured, cfg.Provider)
}
