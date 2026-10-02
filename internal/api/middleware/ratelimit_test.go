package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/time/rate"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
}

func TestRateLimiterAllowsBurstThenRefusesWithRetryAfter(t *testing.T) {
	rl := NewRateLimiter(rate.Limit(1), 3)
	defer rl.Stop()
	h := rl.Limit(okHandler())

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/vms", nil)
		req.RemoteAddr = "10.0.0.1:50000"
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d within burst: got %d", i+1, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/vms", nil)
	req.RemoteAddr = "10.0.0.1:50001" // same IP, different ephemeral port
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request past burst: got %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "1" {
		t.Errorf("Retry-After = %q, want \"1\" for a 1/s limiter", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestRateLimiterIsPerIP(t *testing.T) {
	rl := NewRateLimiter(rate.Limit(1), 1)
	defer rl.Stop()
	h := rl.Limit(okHandler())
	for _, ip := range []string{"10.0.0.1:1", "10.0.0.2:1"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/vms", nil)
		req.RemoteAddr = ip
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("first request from %s: got %d", ip, rec.Code)
		}
	}
}

func TestRetryAfterSeconds(t *testing.T) {
	cases := map[rate.Limit]int{5: 1, 1: 1, 0.5: 2, 5.0 / 60.0: 12, 0: 60}
	for r, want := range cases {
		if got := retryAfterSeconds(r); got != want {
			t.Errorf("retryAfterSeconds(%v) = %d, want %d", r, got, want)
		}
	}
}
