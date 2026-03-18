package middleware

import (
	"net/http"
	"strings"
)

// SecurityHeaders adds standard security headers to all responses.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "0")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		// L1: HSTS header for HTTPS deployments
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		// Only apply no-store to API responses, not static assets
		path := r.URL.Path
		if strings.HasPrefix(path, "/api") || path == "/login" || path == "/logout" {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}
