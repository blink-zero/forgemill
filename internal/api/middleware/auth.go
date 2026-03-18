package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
)

type contextKey string

const userContextKey contextKey = "user"

// 3.11: writeJSONError writes a JSON error response with correct Content-Type.
// http.Error() sets text/plain which is incorrect for JSON bodies.
func writeJSONError(w http.ResponseWriter, body string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write([]byte(body))
}

type AuthMiddleware struct {
	db        *db.DB
	jwtSecret []byte
	jwtExpiry time.Duration
}

func NewAuthMiddleware(db *db.DB, jwtSecret string, jwtExpiry time.Duration) (*AuthMiddleware, error) {
	// MED-06 fix: Return an error instead of just warning when the JWT secret is too short.
	// A short JWT secret (e.g., 8 characters) would be trivially brute-forceable,
	// allowing token forgery and full authentication bypass.
	if len(jwtSecret) < 32 {
		return nil, fmt.Errorf("JWT secret must be at least 32 bytes (got %d) — refusing to start with insecure secret", len(jwtSecret))
	}
	return &AuthMiddleware{
		db:        db,
		jwtSecret: []byte(jwtSecret),
		jwtExpiry: jwtExpiry,
	}, nil
}

type Claims struct {
	UserID       int64  `json:"user_id"`
	Username     string `json:"username"`
	Role         string `json:"role"`
	TokenVersion int    `json:"token_version"`
	jwt.RegisteredClaims
}

func (m *AuthMiddleware) GenerateToken(user *models.User) (string, error) {
	claims := Claims{
		UserID:       user.ID,
		Username:     user.Username,
		Role:         user.Role,
		TokenVersion: user.TokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			// H5: Set issuer and audience claims
			Issuer:    "forgemill",
			Audience:  jwt.ClaimStrings{"forgemill"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(m.jwtExpiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.jwtSecret)
}

func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeJSONError(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenStr == authHeader {
			writeJSONError(w, `{"error":"invalid token format"}`, http.StatusUnauthorized)
			return
		}

		// API key auth: Bearer fm_...
		if strings.HasPrefix(tokenStr, "fm_") {
			m.authenticateAPIKey(w, r, next, tokenStr)
			return
		}

		// JWT auth
		claims := &Claims{}
		// H5: Validate issuer and audience claims
		// V3-M1: Explicitly validate signing algorithm to prevent algorithm confusion
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return m.jwtSecret, nil
		},
			jwt.WithIssuer("forgemill"),
			jwt.WithAudience("forgemill"),
		)
		if err != nil || !token.Valid {
			writeJSONError(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}

		user, err := m.db.GetUserByID(claims.UserID)
		if err != nil || !user.IsActive {
			writeJSONError(w, `{"error":"user not found or inactive"}`, http.StatusUnauthorized)
			return
		}

		// M4: Check token version for revocation
		if claims.TokenVersion != user.TokenVersion {
			writeJSONError(w, `{"error":"token revoked"}`, http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *AuthMiddleware) authenticateAPIKey(w http.ResponseWriter, r *http.Request, next http.Handler, rawKey string) {
	// F-32: Require minimum key length including prefix + secret material
	// Format is "fm_" + 8-char prefix + secret hash material = minimum ~20 chars
	if len(rawKey) < 20 {
		writeJSONError(w, `{"error":"invalid API key"}`, http.StatusUnauthorized)
		return
	}

	prefix := rawKey[:11]
	candidates, err := m.db.GetAllAPIKeysByPrefix(prefix)
	if err != nil || len(candidates) == 0 {
		writeJSONError(w, `{"error":"invalid API key"}`, http.StatusUnauthorized)
		return
	}

	var matched *models.APIKey
	for i := range candidates {
		if bcrypt.CompareHashAndPassword([]byte(candidates[i].KeyHash), []byte(rawKey)) == nil {
			matched = &candidates[i]
			break
		}
	}

	if matched == nil {
		writeJSONError(w, `{"error":"invalid API key"}`, http.StatusUnauthorized)
		return
	}

	// Check expiration
	if matched.ExpiresAt != nil && matched.ExpiresAt.Before(time.Now()) {
		writeJSONError(w, `{"error":"API key expired"}`, http.StatusUnauthorized)
		return
	}

	user, err := m.db.GetUserByID(matched.UserID)
	if err != nil || !user.IsActive {
		writeJSONError(w, `{"error":"user not found or inactive"}`, http.StatusUnauthorized)
		return
	}

	// Update last used timestamp
	if err := m.db.UpdateAPIKeyLastUsed(matched.ID); err != nil {
		slog.Error("failed to update API key last used", "key_id", matched.ID, "error", err)
	}

	ctx := context.WithValue(r.Context(), userContextKey, user)
	next.ServeHTTP(w, r.WithContext(ctx))
}

func (m *AuthMiddleware) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil || user.Role != "admin" {
			writeJSONError(w, `{"error":"admin access required"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole returns middleware that enforces a minimum role level.
// Role hierarchy: viewer < user < admin.
func (m *AuthMiddleware) RequireRole(minRole string) func(http.Handler) http.Handler {
	roleLevel := map[string]int{"viewer": 0, "user": 1, "admin": 2}
	minLevel, ok := roleLevel[minRole]
	if !ok {
		panic(fmt.Sprintf("RequireRole called with unknown role %q", minRole))
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if user == nil {
				writeJSONError(w, `{"error":"insufficient permissions"}`, http.StatusForbidden)
				return
			}
			// F-31: Unknown roles must be denied — map lookup returns 0 for unknown keys,
			// which would silently grant viewer-level access
			level, known := roleLevel[user.Role]
			if !known || level < minLevel {
				writeJSONError(w, `{"error":"insufficient permissions"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func UserFromContext(ctx context.Context) *models.User {
	user, _ := ctx.Value(userContextKey).(*models.User)
	return user
}
