package ai

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Config is everything needed to talk to one model. It comes from
// app_settings; the key is decrypted by the caller.
type Config struct {
	Enabled              bool
	Provider             string // anthropic | openai
	BaseURL              string // empty = provider default
	Model                string
	APIKey               string // may be empty for keyless local endpoints (openai only)
	RedactHostnames      bool   // also redact IPs and FQDNs in what is sent
	AllowPrivateEndpoint bool   // permit BaseURL on private/loopback ranges (Ollama on the LAN)
	Timeout              time.Duration
}

// DefaultBaseURL is where each provider lives when no base URL is set.
func DefaultBaseURL(provider string) string {
	switch provider {
	case ProviderAnthropic:
		return "https://api.anthropic.com"
	case ProviderOpenAI:
		return "https://api.openai.com/v1"
	}
	return ""
}

// DefaultTimeout bounds one completion call.
const DefaultTimeout = 60 * time.Second

// MaxInputBytes caps what one request may carry — the action size limit,
// so an editor payload always fits.
const MaxInputBytes = 64 * 1024

// Validate checks the configuration is complete enough to call a model.
func (c Config) Validate() error {
	if c.Provider != ProviderAnthropic && c.Provider != ProviderOpenAI {
		return fmt.Errorf("%w: provider must be %q or %q", ErrNotConfigured, ProviderAnthropic, ProviderOpenAI)
	}
	if strings.TrimSpace(c.Model) == "" {
		return fmt.Errorf("%w: no model set", ErrNotConfigured)
	}
	if c.Provider == ProviderAnthropic && c.APIKey == "" {
		return fmt.Errorf("%w: Anthropic needs an API key", ErrNotConfigured)
	}
	if err := ValidateBaseURL(c.effectiveBaseURL()); err != nil {
		return fmt.Errorf("%w: %v", ErrNotConfigured, err)
	}
	return nil
}

func (c Config) effectiveBaseURL() string {
	if strings.TrimSpace(c.BaseURL) == "" {
		return DefaultBaseURL(c.Provider)
	}
	return strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
}

func (c Config) timeout() time.Duration {
	if c.Timeout <= 0 {
		return DefaultTimeout
	}
	return c.Timeout
}

// ValidateBaseURL accepts http(s) URLs with a host and nothing else.
// Reachability of private ranges is decided at dial time (see http.go).
func ValidateBaseURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil // provider default applies
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("base URL is not a valid URL")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("base URL must use http or https")
	}
	if u.Hostname() == "" {
		return fmt.Errorf("base URL must have a hostname")
	}
	if u.User != nil {
		return fmt.Errorf("base URL must not carry credentials")
	}
	return nil
}
