package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxResponseBytes bounds what we read back from a provider.
const maxResponseBytes = 2 << 20

// ErrPrivateEndpoint: the base URL resolves to a private, loopback or
// link-local address and the configuration does not allow that.
var ErrPrivateEndpoint = errors.New("endpoint resolves to a private or loopback address; enable \"allow private endpoint\" if that is intended (e.g. Ollama on your LAN)")

var privateRanges = func() []*net.IPNet {
	var nets []*net.IPNet
	for _, c := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "169.254.0.0/16", "0.0.0.0/8", "::1/128", "fc00::/7", "fe80::/10"} {
		_, n, _ := net.ParseCIDR(c)
		nets = append(nets, n)
	}
	return nets
}()

func isPrivate(ip net.IP) bool {
	for _, n := range privateRanges {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// newHTTPClient dials only addresses the configuration permits, follows no
// redirect to a different host, and times out as configured.
func newHTTPClient(cfg Config) *http.Client {
	allowPrivate := cfg.AllowPrivateEndpoint
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if !allowPrivate && isPrivate(ip.IP) {
					return nil, ErrPrivateEndpoint
				}
			}
			// Pin the connection to a resolved address so a DNS answer
			// can't change between the check and the dial.
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: cfg.timeout(),
		MaxIdleConns:          4,
	}
	return &http.Client{
		Timeout:   cfg.timeout(),
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			if req.URL.Host != via[0].URL.Host {
				return fmt.Errorf("refusing redirect to a different host (%s)", req.URL.Host)
			}
			return nil
		},
	}
}

// CheckEndpoint resolves the configured base URL now and applies the
// private-range rule, so Settings → Test can explain a refusal before any
// request is made.
func CheckEndpoint(cfg Config) error {
	u, err := url.Parse(cfg.effectiveBaseURL())
	if err != nil {
		return err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(context.Background(), u.Hostname())
	if err != nil {
		return fmt.Errorf("cannot resolve %s: %v", u.Hostname(), err)
	}
	if !cfg.AllowPrivateEndpoint {
		for _, ip := range ips {
			if isPrivate(ip.IP) {
				return ErrPrivateEndpoint
			}
		}
	}
	return nil
}

// readBody reads at most maxResponseBytes.
func readBody(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, maxResponseBytes))
}

// trimMessage keeps upstream error text short enough for a UI toast.
func trimMessage(s string) string {
	s = string([]rune(s))
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

// transportMessage turns Go's transport errors into one plain sentence.
func transportMessage(err error, timeout time.Duration) string {
	l := strings.ToLower(err.Error())
	switch {
	case strings.Contains(l, "client.timeout") || strings.Contains(l, "deadline exceeded") || strings.Contains(l, "timeout awaiting"):
		return fmt.Sprintf("no answer within %s — the model may be slow or overloaded; try again, pick a smaller or faster model, or shorten the request", timeout.Round(time.Second))
	case strings.Contains(l, "private or loopback"):
		return err.Error()
	case strings.Contains(l, "no such host"):
		return "the endpoint's hostname does not resolve"
	case strings.Contains(l, "connection refused"):
		return "the endpoint refused the connection"
	case strings.Contains(l, "certificate"), strings.Contains(l, "tls"):
		return "TLS failed: " + trimMessage(err.Error())
	}
	return trimMessage(err.Error())
}
