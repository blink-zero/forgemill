package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
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
func writeErrorLog(w http.ResponseWriter, clientMsg string, status int, err error) {
	slog.Error(clientMsg, "error", err)
	writeError(w, clientMsg, status)
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
