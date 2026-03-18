package factory

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"

	wsutil "github.com/forgemill/forgemill/internal/api/ws"
	"github.com/forgemill/forgemill/internal/db/models"
)

// V4-L2: buildWsConn wraps a websocket.Conn with a mutex to serialize writes.
type buildWsConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *buildWsConn) WriteMessage(messageType int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.conn.WriteMessage(messageType, data)
}

// BuildUserStore is the interface for looking up user data during build WS auth.
// V3-H2: WebSocket auth must check IsActive and TokenVersion.
type BuildUserStore interface {
	GetUserByID(id int64) (*models.User, error)
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: wsutil.ValidateOrigin,
}

// BuildMessage is a WebSocket message for build progress.
type BuildMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// buildWsClaims holds JWT claims for build WebSocket authentication.
type buildWsClaims struct {
	UserID       int64  `json:"user_id"`
	Username     string `json:"username"`
	Role         string `json:"role"`
	TokenVersion int    `json:"token_version"`
	jwt.RegisteredClaims
}

// BuildHub manages WebSocket connections for build progress streaming.
type BuildHub struct {
	mu        sync.RWMutex
	clients   map[int64]map[*buildWsConn]bool
	jwtSecret []byte
	users     BuildUserStore // V3-H2: DB access for full auth validation
}

// NewBuildHub creates a new BuildHub.
func NewBuildHub() *BuildHub {
	return &BuildHub{
		clients: make(map[int64]map[*buildWsConn]bool),
	}
}

// 3.9: Thread-safe SetJWTSecret — protects against data races with concurrent reads.
func (h *BuildHub) SetJWTSecret(secret string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.jwtSecret = []byte(secret)
}

// 3.9: Thread-safe SetUserStore — protects against data races with concurrent reads.
// V3-H2: Required for checking IsActive and TokenVersion.
func (h *BuildHub) SetUserStore(users BuildUserStore) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.users = users
}

// SendBuildProgress broadcasts a message to all clients watching a specific build.
func (h *BuildHub) SendBuildProgress(buildID int64, msg BuildMessage) {
	h.mu.RLock()
	conns, ok := h.clients[buildID]
	if !ok {
		h.mu.RUnlock()
		return
	}

	data, err := json.Marshal(msg)
	if err != nil {
		h.mu.RUnlock()
		return
	}

	// V3-M2: Collect failed connections under RLock, delete under full Lock
	// V4-L2: Writes are serialized via buildWsConn.mu
	var toDelete []*buildWsConn
	for wc := range conns {
		if err := wc.WriteMessage(websocket.TextMessage, data); err != nil {
			slog.Debug("build ws write error", "error", err)
			toDelete = append(toDelete, wc)
		}
	}
	h.mu.RUnlock()

	if len(toDelete) > 0 {
		h.mu.Lock()
		currentConns := h.clients[buildID]
		for _, wc := range toDelete {
			delete(currentConns, wc)
			wc.conn.Close()
		}
		if len(currentConns) == 0 {
			delete(h.clients, buildID)
		}
		h.mu.Unlock()
	}
}

// validateWSTokenClaims validates a JWT token for build WebSocket authentication and returns claims.
// V3-H2: Full validation including user IsActive and TokenVersion checks.
// V5-M2: Returns claims so handlers can enforce role-based access control.
// I-3: Acquires read lock before reading jwtSecret and users to prevent data race.
func (h *BuildHub) validateWSTokenClaims(tokenStr string) (*buildWsClaims, bool) {
	h.mu.RLock()
	secret := h.jwtSecret
	users := h.users
	h.mu.RUnlock()

	if len(secret) == 0 {
		return nil, false
	}
	claims := &buildWsClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return secret, nil
	},
		jwt.WithIssuer("forgemill"),
		jwt.WithAudience("forgemill"),
	)
	if err != nil || !token.Valid {
		return nil, false
	}

	// SEC-9: Fail closed if user store is not configured (was fail-open)
	if users == nil {
		return nil, false
	}
	// V3-H2: Check user is still active and token version matches
	user, err := users.GetUserByID(claims.UserID)
	if err != nil || !user.IsActive {
		return nil, false
	}
	if claims.TokenVersion != user.TokenVersion {
		return nil, false
	}
	return claims, true
}

// HandleBuildWS upgrades an HTTP connection to WebSocket for build progress.
func (h *BuildHub) HandleBuildWS(w http.ResponseWriter, r *http.Request) {
	// V3-M11: Removed query parameter fallback — subprotocol is the only transport
	tokenStr := extractBuildTokenFromSubprotocol(r)
	if tokenStr == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	claims, ok := h.validateWSTokenClaims(tokenStr)
	if !ok {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	// V5-M2: Build progress contains sensitive infrastructure details — admin only
	if claims.Role != "admin" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	idStr := chi.URLParam(r, "id")
	buildID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid build ID", http.StatusBadRequest)
		return
	}

	rawConn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("build ws upgrade failed", "error", err)
		return
	}

	// V4-L2: Wrap connection for serialized writes
	wc := &buildWsConn{conn: rawConn}

	// V3-M4: Set read limit to prevent unbounded memory allocation
	rawConn.SetReadLimit(4096)

	// V3-M5: Configure ping/pong heartbeat for stale connection detection
	rawConn.SetReadDeadline(time.Now().Add(60 * time.Second))
	rawConn.SetPongHandler(func(string) error {
		rawConn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// 3.7: Limit concurrent WebSocket connections per build
	h.mu.Lock()
	if h.clients[buildID] == nil {
		h.clients[buildID] = make(map[*buildWsConn]bool)
	}
	if len(h.clients[buildID]) >= 50 {
		h.mu.Unlock()
		rawConn.Close()
		slog.Warn("build ws connection limit reached", "build_id", buildID)
		return
	}
	h.clients[buildID][wc] = true
	h.mu.Unlock()

	slog.Debug("build ws client connected", "build_id", buildID)

	// V4-L1: Use done channel to signal ping goroutine before closing connection
	done := make(chan struct{})
	defer func() {
		close(done)
		h.mu.Lock()
		delete(h.clients[buildID], wc)
		if len(h.clients[buildID]) == 0 {
			delete(h.clients, buildID)
		}
		h.mu.Unlock()
		rawConn.Close()
	}()

	// V3-M5: Start ping ticker to detect stale connections
	// V4-L1: Ping goroutine exits cleanly via done channel
	// V4-L2: Pings use serialized buildWsConn.WriteMessage
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := wc.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}()

	for {
		_, _, err := rawConn.ReadMessage()
		if err != nil {
			break
		}
	}
}

// extractBuildTokenFromSubprotocol extracts a JWT from the WebSocket subprotocol header.
func extractBuildTokenFromSubprotocol(r *http.Request) string {
	for _, proto := range websocket.Subprotocols(r) {
		if strings.HasPrefix(proto, "token.") {
			return proto[6:]
		}
	}
	return ""
}
