package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
)

// privateRanges defines CIDR ranges considered private/internal.
var privateRanges = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"::1/128",
	"fc00::/7",
}

// isPrivateIP checks if an IP address falls within private/internal ranges.
func isPrivateIP(ip net.IP) bool {
	for _, cidr := range privateRanges {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateWebhookURL checks that a webhook URL is safe to call.
// It rejects private/internal IP addresses unless allowPrivate is true.
func ValidateWebhookURL(rawURL string, allowPrivate bool) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("URL must use http or https")
	}
	if u.Hostname() == "" {
		return fmt.Errorf("URL must have a hostname")
	}

	if allowPrivate {
		return nil
	}

	// Resolve hostname and check against private ranges
	ips, err := net.LookupIP(u.Hostname())
	if err != nil {
		return fmt.Errorf("cannot resolve hostname: %s", u.Hostname())
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("webhook URL must not point to private/internal addresses")
		}
	}
	return nil
}

// WebhookSecretDecryptor decrypts webhook secrets stored encrypted at rest.
// V3-M14: Secrets are encrypted by the handler before storage.
type WebhookSecretDecryptor interface {
	Decrypt(string) (string, error)
}

type WebhookService struct {
	db           *db.DB
	client       *http.Client
	allowPrivate bool
	dec          WebhookSecretDecryptor // V3-M14
}

func NewWebhookService(db *db.DB, allowPrivate bool) *WebhookService {
	return &WebhookService{
		db:           db,
		allowPrivate: allowPrivate,
		client: &http.Client{
			Timeout: 10 * time.Second,
			// V3-M9: Validate redirect targets against private IP ranges
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return fmt.Errorf("too many redirects")
				}
				if err := ValidateWebhookURL(req.URL.String(), allowPrivate); err != nil {
					return fmt.Errorf("redirect to blocked URL")
				}
				return nil
			},
		},
	}
}

// SetDecryptor configures the secret decryptor for HMAC signing.
// V3-M14: Required to decrypt webhook secrets stored encrypted at rest.
func (s *WebhookService) SetDecryptor(dec WebhookSecretDecryptor) {
	s.dec = dec
}

type WebhookPayload struct {
	Event      string             `json:"event"`
	Deployment *models.Deployment `json:"deployment"`
	Timestamp  string             `json:"timestamp"`
}

// Fire sends webhook notifications for the given event.
func (s *WebhookService) Fire(event string, deployment *models.Deployment) {
	webhooks, err := s.db.ListActiveWebhooks()
	if err != nil {
		slog.Error("failed to list active webhooks", "error", err)
		return
	}

	payload := WebhookPayload{
		Event:      event,
		Deployment: deployment,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("failed to marshal webhook payload", "error", err)
		return
	}

	// MED-18: Use a semaphore to bound concurrent webhook goroutines
	sem := make(chan struct{}, 10)
	for _, wh := range webhooks {
		if !matchesEvent(wh.Events, event) {
			continue
		}
		sem <- struct{}{}
		go func(w models.Webhook) {
			defer func() { <-sem }()
			s.send(w, body)
		}(wh)
	}
}

func (s *WebhookService) send(wh models.Webhook, body []byte) {
	// Fix 12: Validate webhook URL before sending
	if err := ValidateWebhookURL(wh.URL, s.allowPrivate); err != nil {
		slog.Warn("webhook URL validation failed", "webhook", wh.Name, "url", wh.URL, "error", err)
		return
	}

	// V3-H3: Pin the resolved IP to prevent DNS rebinding attacks.
	// Resolve DNS once and connect to the validated IP directly.
	u, _ := url.Parse(wh.URL)
	var pinnedIP string
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	if !s.allowPrivate {
		ips, err := net.LookupIP(u.Hostname())
		if err != nil || len(ips) == 0 {
			slog.Warn("webhook DNS resolution failed", "webhook", wh.Name, "url", wh.URL)
			return
		}
		// V4-L3: Validate resolved IP is not in a private/loopback/link-local range
		// to prevent SSRF via DNS records pointing to internal addresses.
		ip := ips[0]
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			slog.Warn("webhook DNS resolved to private IP", "webhook", wh.Name, "ip", ip)
			return
		}
		pinnedIP = ip.String()
	}

	// Create a per-request client with pinned IP transport if needed
	client := s.client
	if pinnedIP != "" {
		transport := &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, pinnedIP+":"+port)
			},
		}
		// 5.7: Close idle connections after use to prevent transport/goroutine leaks
		defer transport.CloseIdleConnections()
		client = &http.Client{
			Timeout:       10 * time.Second,
			Transport:     transport,
			CheckRedirect: s.client.CheckRedirect,
		}
	}

	req, err := http.NewRequest(http.MethodPost, wh.URL, bytes.NewReader(body))
	if err != nil {
		slog.Error("failed to create webhook request", "webhook", wh.Name, "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	if wh.Secret != "" {
		// V3-M14 + V4-M2: Decrypt webhook secret before using for HMAC signing.
		// If decryptor is nil or decryption fails, skip signing entirely rather
		// than using the ciphertext as the HMAC key.
		var secret string
		if s.dec != nil {
			decrypted, err := s.dec.Decrypt(wh.Secret)
			if err != nil {
				slog.Warn("webhook secret decryption failed, skipping signature", "webhook", wh.Name)
			} else {
				secret = decrypted
			}
		} else {
			slog.Warn("webhook decryptor not configured, skipping signature", "webhook", wh.Name)
		}
		if secret != "" {
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write(body)
			signature := hex.EncodeToString(mac.Sum(nil))
			req.Header.Set("X-Forgemill-Signature", signature)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("webhook delivery failed", "webhook", wh.Name, "url", wh.URL, "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		slog.Warn("webhook returned non-success status", "webhook", wh.Name, "url", wh.URL, "status", resp.StatusCode)
	} else {
		slog.Debug("webhook delivered", "webhook", wh.Name, "status", resp.StatusCode)
	}
}

// sendWithStatus is like send but returns the HTTP status code for synchronous callers.
func (s *WebhookService) sendWithStatus(wh models.Webhook, body []byte) (int, error) {
	if err := ValidateWebhookURL(wh.URL, s.allowPrivate); err != nil {
		return 0, fmt.Errorf("URL validation failed: %w", err)
	}

	u, _ := url.Parse(wh.URL)
	var pinnedIP string
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	if !s.allowPrivate {
		ips, err := net.LookupIP(u.Hostname())
		if err != nil || len(ips) == 0 {
			return 0, fmt.Errorf("DNS resolution failed for %s", u.Hostname())
		}
		ip := ips[0]
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return 0, fmt.Errorf("webhook URL resolves to private IP")
		}
		pinnedIP = ip.String()
	}

	client := s.client
	if pinnedIP != "" {
		transport := &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, pinnedIP+":"+port)
			},
		}
		defer transport.CloseIdleConnections()
		client = &http.Client{
			Timeout:       10 * time.Second,
			Transport:     transport,
			CheckRedirect: s.client.CheckRedirect,
		}
	}

	req, err := http.NewRequest(http.MethodPost, wh.URL, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if wh.Secret != "" {
		var secret string
		if s.dec != nil {
			decrypted, err := s.dec.Decrypt(wh.Secret)
			if err != nil {
				slog.Warn("webhook secret decryption failed, skipping signature", "webhook", wh.Name)
			} else {
				secret = decrypted
			}
		}
		if secret != "" {
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write(body)
			signature := hex.EncodeToString(mac.Sum(nil))
			req.Header.Set("X-Forgemill-Signature", signature)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("delivery failed: %w", err)
	}
	defer resp.Body.Close()

	return resp.StatusCode, nil
}

// FireTemplateEvent sends webhook notifications for template lifecycle events.
func (s *WebhookService) FireTemplateEvent(event string, data map[string]interface{}) {
	webhooks, err := s.db.ListActiveWebhooks()
	if err != nil {
		slog.Error("failed to list active webhooks", "error", err)
		return
	}

	payload := map[string]interface{}{
		"event":     event,
		"data":      data,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("failed to marshal template webhook payload", "error", err)
		return
	}

	// HIGH-03 fix: Use a semaphore to bound concurrent webhook goroutines,
	// matching the pattern in Fire(). Without this, rapid template events could
	// spawn unbounded goroutines and exhaust memory.
	sem := make(chan struct{}, 10)
	for _, wh := range webhooks {
		if !matchesEvent(wh.Events, event) {
			continue
		}
		sem <- struct{}{}
		go func(w models.Webhook) {
			defer func() { <-sem }()
			s.send(w, body)
		}(wh)
	}
}

// SendTest sends a test webhook payload synchronously and returns the HTTP status code.
func (s *WebhookService) SendTest(wh *models.Webhook) (int, error) {
	if err := ValidateWebhookURL(wh.URL, s.allowPrivate); err != nil {
		return 0, fmt.Errorf("URL validation failed: %w", err)
	}

	payload := map[string]interface{}{
		"event":   "test",
		"message": "This is a test webhook from Forgemill.",
		"webhook": map[string]interface{}{
			"id":   wh.ID,
			"name": wh.Name,
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal payload: %w", err)
	}

	return s.sendWithStatus(*wh, body)
}

func matchesEvent(configured, event string) bool {
	for _, e := range strings.Split(configured, ",") {
		if strings.TrimSpace(e) == event {
			return true
		}
	}
	return false
}
