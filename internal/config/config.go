package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr           string
	DatabasePath         string
	DataDir              string
	JWTSecret            string
	JWTExpiry            time.Duration
	EncryptionKey        string
	AdminUsername        string
	AdminPassword        string
	LogLevel             string
	FrontendPath         string
	CORSOrigins          string
	AllowPrivateWebhooks bool
	TrustedProxies       string // V3-H1: Only trust X-Forwarded-For when set
	TLSCert              string // V3-M8: TLS certificate file path
	TLSKey               string // V3-M8: TLS key file path
}

// DefaultJWTSecret is the insecure default that must be replaced on first run.
const DefaultJWTSecret = "change-me-in-production"

// DefaultEncryptionKey is the insecure default that must be replaced on first run.
const DefaultEncryptionKey = "change-me-32-byte-key-for-prod!!"

func Load() *Config {
	dataDir := envOrDefault("FORGEMILL_DATA_DIR", "/app/data")
	return &Config{
		ListenAddr:           envOrDefault("FORGEMILL_LISTEN_ADDR", ":8080"),
		DatabasePath:         envOrDefault("FORGEMILL_DB_PATH", dataDir+"/forgemill.db"),
		DataDir:              dataDir,
		// HIGH-07: Support Docker secrets via _FILE env vars
		JWTSecret:            envOrFileOrDefault("FORGEMILL_JWT_SECRET", DefaultJWTSecret),
		JWTExpiry:            envDurationOrDefault("FORGEMILL_JWT_EXPIRY", 1*time.Hour),
		EncryptionKey:        envOrFileOrDefault("FORGEMILL_ENCRYPTION_KEY", DefaultEncryptionKey),
		AdminUsername:        envOrDefault("FORGEMILL_ADMIN_USER", "admin"),
		AdminPassword:        envOrFileOrDefault("FORGEMILL_ADMIN_PASSWORD", ""),
		LogLevel:             envOrDefault("FORGEMILL_LOG_LEVEL", "info"),
		FrontendPath:         envOrDefault("FORGEMILL_FRONTEND_PATH", "./frontend/dist"),
		CORSOrigins:          envOrDefault("FORGEMILL_CORS_ORIGINS", ""),
		AllowPrivateWebhooks: envOrDefault("FORGEMILL_ALLOW_PRIVATE_WEBHOOKS", "") == "true",
		TrustedProxies:       envOrDefault("FORGEMILL_TRUSTED_PROXIES", ""),
		TLSCert:              envOrDefault("FORGEMILL_TLS_CERT", ""),
		TLSKey:               envOrDefault("FORGEMILL_TLS_KEY", ""),
	}
}

func EnvOrDefault(key, fallback string) string {
	return envOrDefault(key, fallback)
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// HIGH-07: envOrFileOrDefault reads from env var, then from a _FILE env var
// (Docker secrets pattern), then falls back to the default.
func envOrFileOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	if filePath := os.Getenv(key + "_FILE"); filePath != "" {
		if data, err := os.ReadFile(filePath); err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return fallback
}

// MED-05: Maximum JWT expiry of 24 hours to limit token theft window.
const maxJWTExpiry = 24 * time.Hour

func envDurationOrDefault(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	// Try standard Go duration format first (e.g., "30m", "2h", "1h30m")
	var d time.Duration
	if parsed, err := time.ParseDuration(v); err == nil {
		d = parsed
	} else {
		// Fall back to integer hours for backwards compatibility
		hours, err := strconv.Atoi(v)
		if err != nil {
			return fallback
		}
		d = time.Duration(hours) * time.Hour
	}
	// MED-05 fix: Enforce maximum JWT expiry to prevent effectively non-expiring tokens.
	if d > maxJWTExpiry {
		slog.Warn("JWT expiry exceeds maximum, clamping to 24h", "requested", d, "max", maxJWTExpiry)
		d = maxJWTExpiry
	}
	return d
}
