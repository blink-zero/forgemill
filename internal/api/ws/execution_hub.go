package ws

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

// maxConnsPerExecution limits concurrent WebSocket connections per execution.
const maxConnsPerExecution = 20

// maxBufferedMessages limits how many messages are buffered per execution for replay.
const maxBufferedMessages = 5000

// ExecutionHub streams live output from SSH action executions to WebSocket clients.
// Separate from the deploy Hub — different lifecycle, different message types.
// Buffers messages per execution so late-connecting clients receive a full replay.
type ExecutionHub struct {
	mu        sync.RWMutex
	clients   map[int64]map[*wsConn]bool
	buffers   map[int64][]json.RawMessage // buffered messages per execution for replay
	finished  map[int64]bool              // tracks completed executions for cleanup
	jwtSecret []byte
	users     UserStore
}

func NewExecutionHub() *ExecutionHub {
	return &ExecutionHub{
		clients:  make(map[int64]map[*wsConn]bool),
		buffers:  make(map[int64][]json.RawMessage),
		finished: make(map[int64]bool),
	}
}

func (h *ExecutionHub) SetJWTSecret(secret string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.jwtSecret = []byte(secret)
}

func (h *ExecutionHub) SetUserStore(users UserStore) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.users = users
}

// SendOutput sends a message to all WebSocket clients watching an execution.
// Also buffers the message so late-connecting clients receive a full replay.
// Implements the service.ExecutionHub interface.
func (h *ExecutionHub) SendOutput(executionID int64, msg any) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	h.mu.Lock()

	// Buffer the message for replay (cap at maxBufferedMessages)
	buf := h.buffers[executionID]
	if len(buf) < maxBufferedMessages {
		h.buffers[executionID] = append(buf, json.RawMessage(data))
	}

	// Check if this is a terminal status message
	if raw, ok := msg.(map[string]interface{}); ok {
		if raw["type"] == "status" {
			if d, ok := raw["data"].(map[string]interface{}); ok {
				if s, ok := d["status"].(string); ok {
					if s == "completed" || s == "failed" || s == "cancelled" {
						h.finished[executionID] = true
						// Schedule cleanup of buffer after 60s (clients should have connected by then)
						go func() {
							time.Sleep(60 * time.Second)
							h.mu.Lock()
							delete(h.buffers, executionID)
							delete(h.finished, executionID)
							h.mu.Unlock()
						}()
					}
				}
			}
		}
	}

	conns := h.clients[executionID]
	if len(conns) == 0 {
		h.mu.Unlock()
		return
	}

	var toDelete []*wsConn
	for wc := range conns {
		if err := wc.WriteMessage(websocket.TextMessage, data); err != nil {
			slog.Debug("execution ws write error", "error", err)
			toDelete = append(toDelete, wc)
		}
	}

	for _, wc := range toDelete {
		if conns[wc] {
			delete(conns, wc)
			wc.conn.Close()
		}
	}
	if len(conns) == 0 {
		delete(h.clients, executionID)
	}
	h.mu.Unlock()
}

// replayBuffer sends all buffered messages for an execution to a single client.
// Must be called without holding h.mu.
func (h *ExecutionHub) replayBuffer(executionID int64, wc *wsConn) {
	h.mu.RLock()
	buf := make([]json.RawMessage, len(h.buffers[executionID]))
	copy(buf, h.buffers[executionID])
	h.mu.RUnlock()

	for _, data := range buf {
		if err := wc.WriteMessage(websocket.TextMessage, data); err != nil {
			slog.Debug("execution ws replay error", "error", err)
			return
		}
	}
}

// validateWSTokenClaims validates a JWT for WebSocket authentication.
// Same pattern as Hub.validateWSTokenClaims.
func (h *ExecutionHub) validateWSTokenClaims(tokenStr string) (*wsClaims, bool) {
	h.mu.RLock()
	secret := h.jwtSecret
	users := h.users
	h.mu.RUnlock()

	if len(secret) == 0 {
		return nil, false
	}

	// Reuse the shared validation logic from the deploy hub
	tempHub := &Hub{}
	tempHub.jwtSecret = secret
	tempHub.users = users
	return tempHub.validateWSTokenClaims(tokenStr)
}

// HandleExecutionWS handles WebSocket connections for execution output streaming.
func (h *ExecutionHub) HandleExecutionWS(w http.ResponseWriter, r *http.Request) {
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

	// Viewer role can watch executions (read-only)
	_ = claims

	idStr := chi.URLParam(r, "id")
	executionID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid execution ID", http.StatusBadRequest)
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
		slog.Error("execution ws upgrade failed", "error", err)
		return
	}

	wc := &wsConn{conn: rawConn}
	rawConn.SetReadLimit(4096)
	rawConn.SetReadDeadline(time.Now().Add(60 * time.Second))
	rawConn.SetPongHandler(func(string) error {
		rawConn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	h.mu.Lock()
	if h.clients[executionID] == nil {
		h.clients[executionID] = make(map[*wsConn]bool)
	}
	if len(h.clients[executionID]) >= maxConnsPerExecution {
		h.mu.Unlock()
		rawConn.Close()
		slog.Warn("execution ws connection limit reached", "execution_id", executionID)
		return
	}
	h.clients[executionID][wc] = true
	h.mu.Unlock()

	slog.Debug("execution ws client connected", "execution_id", executionID)

	// Replay any buffered output from before this client connected.
	// This handles the race where the execution starts sending output
	// before the WebSocket connection is established.
	h.replayBuffer(executionID, wc)

	done := make(chan struct{})
	defer func() {
		close(done)
		h.mu.Lock()
		delete(h.clients[executionID], wc)
		if len(h.clients[executionID]) == 0 {
			delete(h.clients, executionID)
		}
		h.mu.Unlock()
		rawConn.Close()
	}()

	// Ping ticker for stale connection detection
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
