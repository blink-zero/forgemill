package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter provides per-IP rate limiting.
type RateLimiter struct {
	visitors map[string]*visitor
	mu       sync.Mutex
	r        rate.Limit
	b        int
	done     chan struct{}
}

// NewRateLimiter creates a rate limiter allowing r events per second with burst b.
func NewRateLimiter(r rate.Limit, b int) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		r:        r,
		b:        b,
		done:     make(chan struct{}),
	}
	go rl.cleanup()
	return rl
}

// Stop shuts down the cleanup goroutine.
func (rl *RateLimiter) Stop() {
	close(rl.done)
}

func (rl *RateLimiter) getVisitor(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[ip]
	if !exists {
		// V3-M6: Cap the visitor map to prevent memory exhaustion from IP spoofing
		if len(rl.visitors) >= 100000 {
			// Evict all expired entries eagerly
			for k, vis := range rl.visitors {
				if time.Since(vis.lastSeen) > 3*time.Minute {
					delete(rl.visitors, k)
				}
			}
			// If still over cap after eviction, return a zero-burst limiter that rejects
			if len(rl.visitors) >= 100000 {
				return rate.NewLimiter(0, 0)
			}
		}
		limiter := rate.NewLimiter(rl.r, rl.b)
		rl.visitors[ip] = &visitor{limiter: limiter, lastSeen: time.Now()}
		return limiter
	}
	v.lastSeen = time.Now()
	return v.limiter
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(3 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-rl.done:
			return
		case <-ticker.C:
			rl.mu.Lock()
			for ip, v := range rl.visitors {
				if time.Since(v.lastSeen) > 3*time.Minute {
					delete(rl.visitors, ip)
				}
			}
			rl.mu.Unlock()
		}
	}
}

// Limit returns middleware that rate limits requests per IP.
func (rl *RateLimiter) Limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// M1: Extract only the host portion, stripping the ephemeral port
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr // fallback for unix sockets etc
		}
		limiter := rl.getVisitor(ip)
		if !limiter.Allow() {
			// F-37: Use correct Content-Type for JSON error body (http.Error sets text/plain)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate limit exceeded"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
