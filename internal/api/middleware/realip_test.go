package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func seenRemoteAddr(t *testing.T, trusted string, peer string, headers map[string]string) string {
	t.Helper()
	var got string
	h := TrustedProxyRealIP(ParseTrustedProxies(trusted))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.RemoteAddr
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/vms", nil)
	req.RemoteAddr = peer
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	h.ServeHTTP(httptest.NewRecorder(), req)
	return got
}

func TestTrustedProxyHeadersHonouredOnlyFromTrustedPeer(t *testing.T) {
	// Through the configured proxy: client IP replaces the peer, bare IP
	// like chi's RealIP produced.
	if got := seenRemoteAddr(t, "10.0.0.5", "10.0.0.5:41234", map[string]string{"X-Forwarded-For": "203.0.113.9, 10.0.0.5"}); got != "203.0.113.9" {
		t.Errorf("trusted proxy: RemoteAddr = %q, want 203.0.113.9", got)
	}
	// Directly from an arbitrary client carrying the same header: ignored.
	if got := seenRemoteAddr(t, "10.0.0.5", "198.51.100.7:5555", map[string]string{"X-Forwarded-For": "203.0.113.9"}); got != "198.51.100.7:5555" {
		t.Errorf("untrusted peer must keep its real address, got %q", got)
	}
}

func TestTrustedProxyPrecedenceMatchesChi(t *testing.T) {
	hdrs := map[string]string{"X-Forwarded-For": "203.0.113.1", "X-Real-IP": "203.0.113.2", "True-Client-IP": "203.0.113.3"}
	if got := seenRemoteAddr(t, "10.0.0.5", "10.0.0.5:1", hdrs); got != "203.0.113.3" {
		t.Errorf("True-Client-IP should win, got %q", got)
	}
	if got := seenRemoteAddr(t, "10.0.0.5", "10.0.0.5:1", map[string]string{"X-Forwarded-For": "203.0.113.1", "X-Real-IP": "203.0.113.2"}); got != "203.0.113.2" {
		t.Errorf("X-Real-IP should beat X-Forwarded-For, got %q", got)
	}
}

func TestTrustedProxyCIDRAndMalformedHeader(t *testing.T) {
	if got := seenRemoteAddr(t, "10.0.0.0/24, 192.168.1.1", "10.0.0.77:9", map[string]string{"X-Real-IP": "203.0.113.4"}); got != "203.0.113.4" {
		t.Errorf("CIDR-matched proxy should be trusted, got %q", got)
	}
	if got := seenRemoteAddr(t, "10.0.0.0/24", "10.0.0.77:9", map[string]string{"X-Real-IP": "not-an-ip"}); got != "10.0.0.77:9" {
		t.Errorf("malformed header must leave RemoteAddr alone, got %q", got)
	}
	if got := seenRemoteAddr(t, "", "10.0.0.77:9", map[string]string{"X-Real-IP": "203.0.113.4"}); got != "10.0.0.77:9" {
		t.Errorf("empty trusted list must disable rewriting entirely, got %q", got)
	}
}

func TestParseTrustedProxiesSkipsGarbage(t *testing.T) {
	got := ParseTrustedProxies(" 10.0.0.5 ,fd00::1, 172.16.0.0/12 , nonsense ,")
	if len(got) != 3 {
		t.Fatalf("want 3 prefixes (garbage skipped), got %d: %v", len(got), got)
	}
	if got[0].Bits() != 32 || got[1].Bits() != 128 || got[2].Bits() != 12 {
		t.Errorf("unexpected prefix lengths: %v", got)
	}
}
