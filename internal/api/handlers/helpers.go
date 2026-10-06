package handlers

import (
	"encoding/json"

	"github.com/forgemill/forgemill/internal/provider"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// F-47: Log encoding errors instead of silently dropping them
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to encode JSON response", "error", err)
	}
}

func writeError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// F-47: Log encoding errors instead of silently dropping them
	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		slog.Error("failed to encode error response", "error", err)
	}
}

// writeErrorLog logs the full error server-side and returns a generic message to the client.
// The error is also kept in a small in-memory ring so the diagnostics
// endpoint can show operators the last few server-side failures.
func writeErrorLog(w http.ResponseWriter, clientMsg string, status int, err error) {
	if provider.IsLicenseRestricted(err) {
		// Not a server fault: the hypervisor's license refused the write.
		// Say so plainly, once, for every operation that can hit it.
		slog.Warn(clientMsg+": hypervisor license prohibits API writes", "error", err)
		writeError(w, provider.LicenseRestrictedMessage, http.StatusConflict)
		return
	}
	slog.Error(clientMsg, "error", err)
	recordServerError(status, clientMsg, err)
	writeError(w, clientMsg, status)
}

// ServerError is one entry of the recent-errors ring.
type ServerError struct {
	Time    time.Time `json:"time"`
	Status  int       `json:"status"`
	Message string    `json:"message"` // what the client was told
	Error   string    `json:"error"`   // the underlying error
}

const recentServerErrorsCap = 50

var (
	recentErrorsMu sync.Mutex
	recentErrors   []ServerError
)

func recordServerError(status int, msg string, err error) {
	e := ServerError{Time: time.Now().UTC(), Status: status, Message: msg}
	if err != nil {
		e.Error = err.Error()
	}
	recentErrorsMu.Lock()
	defer recentErrorsMu.Unlock()
	recentErrors = append(recentErrors, e)
	if len(recentErrors) > recentServerErrorsCap {
		recentErrors = recentErrors[len(recentErrors)-recentServerErrorsCap:]
	}
}

// RecentServerErrors returns the newest server-side errors first.
func RecentServerErrors() []ServerError {
	recentErrorsMu.Lock()
	defer recentErrorsMu.Unlock()
	out := make([]ServerError, len(recentErrors))
	for i, e := range recentErrors {
		out[len(recentErrors)-1-i] = e
	}
	return out
}

// firstNonEmpty returns the first non-empty string from the arguments.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
