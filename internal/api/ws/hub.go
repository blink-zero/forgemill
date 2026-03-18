package ws

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"

	"github.com/forgemill/forgemill/internal/db/models"
)

// V4-L2: wsConn wraps a websocket.Conn with a mutex to serialize writes.
// gorilla/websocket requires that only one goroutine writes at a time.
type wsConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *wsConn) WriteMessage(messageType int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.conn.WriteMessage(messageType, data)
}

// UserStore is the interface for looking up user data during WS auth.
// V3-H2: WebSocket auth must check IsActive and TokenVersion like HTTP auth.
type UserStore interface {
	GetUserByID(id int64) (*models.User, error)
}

// ValidateOrigin checks if the request origin is allowed.
// Non-browser clients (no Origin header) are allowed.
// Browser clients must match the request Host exactly.
// M2: Fixed to use exact host matching instead of prefix-based comparison.
// V4-L4: Normalize both sides to hostname-only to handle port mismatches
// from reverse proxies stripping ports.
func ValidateOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // non-browser clients
	}
	originURL, err := url.Parse(origin)
	if err != nil {
		return false
	}
	requestHost := r.Host
	if h, _, err := net.SplitHostPort(requestHost); err == nil {
		requestHost = h
	}
	return originURL.Hostname() == requestHost
}

var upgrader = websocket.Upgrader{
	CheckOrigin: ValidateOrigin,
}

type Message struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// wsClaims holds JWT claims for WebSocket authentication.
type wsClaims struct {
	UserID       int64  `json:"user_id"`
	Username     string `json:"username"`
	Role         string `json:"role"`
	TokenVersion int    `json:"token_version"`
	jwt.RegisteredClaims
}

// maxConnsPerDeployment limits concurrent WebSocket connections per deployment to prevent FD exhaustion.
const maxConnsPerDeployment = 50

type Hub struct {
	mu        sync.RWMutex
	clients   map[int64]map[*wsConn]bool
	jwtSecret []byte
	users     UserStore // V3-H2: DB access for full auth validation
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[int64]map[*wsConn]bool),
	}
}

// 3.9: Thread-safe SetJWTSecret — protects against data races with concurrent reads.
func (h *Hub) SetJWTSecret(secret string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.jwtSecret = []byte(secret)
}

// 3.9: Thread-safe SetUserStore — protects against data races with concurrent reads.
// V3-H2: Required for checking IsActive and TokenVersion.
func (h *Hub) SetUserStore(users UserStore) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.users = users
}

func (h *Hub) SendProgress(deploymentID int64, msg any) {
	h.mu.RLock()
	conns, ok := h.clients[deploymentID]
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
	// V4-L2: Writes are serialized via wsConn.mu
	var toDelete []*wsConn
	for wc := range conns {
		if err := wc.WriteMessage(websocket.TextMessage, data); err != nil {
			slog.Debug("ws write error", "error", err)
			toDelete = append(toDelete, wc)
		}
	}
	h.mu.RUnlock()

	if len(toDelete) > 0 {
		h.mu.Lock()
		currentConns := h.clients[deploymentID]
		for _, wc := range toDelete {
			// F-43: Check connection still exists in map before closing —
			// HandleWS cleanup may have already removed+closed it between RUnlock and Lock
			if currentConns != nil && currentConns[wc] {
				delete(currentConns, wc)
				wc.conn.Close()
			}
		}
		if currentConns != nil && len(currentConns) == 0 {
			delete(h.clients, deploymentID)
		}
		h.mu.Unlock()
	}
}

// validateWSTokenClaims validates a JWT token for WebSocket authentication and returns claims.
// V3-H2: Full validation: signature, expiry, issuer, audience, user IsActive, TokenVersion.
// V5-M2: Returns claims so handlers can enforce role-based access control.
// I-4: Acquires read lock before reading jwtSecret and users to prevent data race.
func (h *Hub) validateWSTokenClaims(tokenStr string) (*wsClaims, bool) {
	h.mu.RLock()
	secret := h.jwtSecret
	users := h.users
	h.mu.RUnlock()

	if len(secret) == 0 {
		return nil, false
	}
	claims := &wsClaims{}
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

	// MED-07: Fail closed if user store is not configured
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

// extractTokenFromSubprotocol extracts a JWT from the WebSocket subprotocol header.
// H3: Tokens are sent as subprotocol "token.<jwt>" instead of query parameters.
func extractTokenFromSubprotocol(r *http.Request) string {
	for _, proto := range websocket.Subprotocols(r) {
		if strings.HasPrefix(proto, "token.") {
			return proto[6:]
		}
	}
	return ""
}

func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	// V3-M11: Removed query parameter fallback — subprotocol is the only transport
	tokenStr := extractTokenFromSubprotocol(r)
	if tokenStr == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	claims, ok := h.validateWSTokenClaims(tokenStr)
	if !ok {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	// V5-M2: Viewer role cannot watch deployment progress
	if claims.Role == "viewer" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	idStr := chi.URLParam(r, "id")
	deploymentID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid deployment ID", http.StatusBadRequest)
		return
	}

	// Echo back the token subprotocol so Chrome accepts the connection.
	var respHeader http.Header
	for _, proto := range websocket.Subprotocols(r) {
		if strings.HasPrefix(proto, "token.") {
			respHeader = http.Header{"Sec-WebSocket-Protocol": {proto}}
			break
		}
	}

	rawConn, err := upgrader.Upgrade(w, r, respHeader)
	if err != nil {
		slog.Error("ws upgrade failed", "error", err)
		return
	}

	// V4-L2: Wrap connection for serialized writes
	wc := &wsConn{conn: rawConn}

	// V3-M4: Set read limit to prevent unbounded memory allocation
	rawConn.SetReadLimit(4096)

	// V3-M5: Configure ping/pong heartbeat for stale connection detection
	rawConn.SetReadDeadline(time.Now().Add(60 * time.Second))
	rawConn.SetPongHandler(func(string) error {
		rawConn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// 3.7: Limit concurrent WebSocket connections per deployment
	h.mu.Lock()
	if h.clients[deploymentID] == nil {
		h.clients[deploymentID] = make(map[*wsConn]bool)
	}
	if len(h.clients[deploymentID]) >= maxConnsPerDeployment {
		h.mu.Unlock()
		rawConn.Close()
		slog.Warn("ws connection limit reached", "deployment_id", deploymentID)
		return
	}
	h.clients[deploymentID][wc] = true
	h.mu.Unlock()

	slog.Debug("ws client connected", "deployment_id", deploymentID)

	// V4-L1: Use done channel to signal ping goroutine before closing connection
	done := make(chan struct{})
	defer func() {
		close(done)
		h.mu.Lock()
		delete(h.clients[deploymentID], wc)
		if len(h.clients[deploymentID]) == 0 {
			delete(h.clients, deploymentID)
		}
		h.mu.Unlock()
		rawConn.Close()
	}()

	// V3-M5: Start ping ticker to detect stale connections
	// V4-L1: Ping goroutine exits cleanly via done channel
	// V4-L2: Pings use serialized wsConn.WriteMessage
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
