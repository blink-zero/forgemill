package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ParseTrustedProxies turns the FORGEMILL_TRUSTED_PROXIES value — a
// comma-separated list of proxy IPs, optionally CIDRs — into prefixes. A
// bare IP becomes a /32 (or /128). Invalid entries are reported and
// skipped rather than aborting startup, so a typo degrades to "that proxy
// isn't trusted" instead of "nothing is", which is the safe direction.
func ParseTrustedProxies(raw string) []netip.Prefix {
	var out []netip.Prefix
	for _, part := range strings.Split(raw, ",") {
		s := strings.TrimSpace(part)
		if s == "" {
			continue
		}
		if p, err := netip.ParsePrefix(s); err == nil {
			out = append(out, p.Masked())
			continue
		}
		if a, err := netip.ParseAddr(s); err == nil {
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
			continue
		}
		slog.Error("FORGEMILL_TRUSTED_PROXIES: ignoring entry that is neither an IP nor a CIDR", "entry", s)
	}
	return out
}

// TrustedProxyRealIP rewrites r.RemoteAddr from the forwarding headers a
// reverse proxy sets — but only when the direct peer is one of the trusted
// proxies. chi's RealIP (deprecated, GHSA-3fxj-6jh8-hvhx) applied the
// headers for every peer, so once the setting was on, any client could
// pick the IP the audit log and rate limiter saw.
//
// Header precedence and the resulting RemoteAddr form (bare IP, no port)
// match chi's RealIP exactly so downstream readers see no difference for
// traffic that really did come through the proxy.
func TrustedProxyRealIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if len(trusted) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if peerIsTrusted(r.RemoteAddr, trusted) {
				if ip := forwardedClientIP(r.Header); ip != "" {
					r.RemoteAddr = ip
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func peerIsTrusted(remoteAddr string, trusted []netip.Prefix) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr // already a bare IP (e.g. set by an outer proxy layer)
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	peer = peer.Unmap()
	for _, p := range trusted {
		if p.Contains(peer) {
			return true
		}
	}
	return false
}

// forwardedClientIP returns the client IP a proxy reported, using the same
// precedence as chi/middleware.RealIP: True-Client-IP, X-Real-IP, then the
// leftmost X-Forwarded-For entry. Returns "" when none parses as an IP.
func forwardedClientIP(h http.Header) string {
	candidates := []string{h.Get("True-Client-IP"), h.Get("X-Real-IP")}
	if xff := h.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		candidates = append(candidates, first)
	}
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, err := netip.ParseAddr(c); err == nil {
			return c
		}
		return "" // a header was present but malformed: don't fall through to a weaker one
	}
	return ""
}
