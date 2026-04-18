package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/db"
)

type NotificationHandler struct {
	db *db.DB
}

func NewNotificationHandler(database *db.DB) *NotificationHandler {
	return &NotificationHandler{db: database}
}

// List returns notifications for the current user (plus broadcasts).
// Query params: unread_only=true, limit=N (max 200, default 50).
func (h *NotificationHandler) List(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	unreadOnly := r.URL.Query().Get("unread_only") == "true"
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			limit = n
		}
	}
	notifs, err := h.db.ListNotificationsForUser(user.ID, unreadOnly, limit)
	if err != nil {
		writeErrorLog(w, "failed to load notifications", http.StatusInternalServerError, err)
		return
	}
	count, _ := h.db.CountUnreadForUser(user.ID)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"notifications": notifs,
		"unread_count":  count,
	})
}

// UnreadCount returns the unread count for the current user. Used for polling.
func (h *NotificationHandler) UnreadCount(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	count, err := h.db.CountUnreadForUser(user.ID)
	if err != nil {
		writeErrorLog(w, "failed to count notifications", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"unread_count": count})
}

// MarkRead marks a single notification read.
func (h *NotificationHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, "invalid notification ID", http.StatusBadRequest)
		return
	}
	if err := h.db.MarkNotificationRead(id, user.ID); err != nil {
		writeError(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "read"})
}

// MarkAllRead marks every unread notification for the current user as read.
func (h *NotificationHandler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	n, err := h.db.MarkAllNotificationsRead(user.ID)
	if err != nil {
		writeErrorLog(w, "failed to mark all read", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"marked_read": n})
}

// Delete removes a single notification.
func (h *NotificationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, "invalid notification ID", http.StatusBadRequest)
		return
	}
	if err := h.db.DeleteNotification(id, user.ID); err != nil {
		writeError(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
