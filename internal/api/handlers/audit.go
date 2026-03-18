package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/forgemill/forgemill/internal/db"
)

type AuditHandler struct {
	db *db.DB
}

func NewAuditHandler(database *db.DB) *AuditHandler {
	return &AuditHandler{db: database}
}

func (h *AuditHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	f := db.AuditLogFilter{}

	if v := q.Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.Page = n
		}
	}
	if v := q.Get("page_size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.PageSize = n
		}
	}
	if v := q.Get("actor_id"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.ActorID = &n
		}
	}
	if v := q.Get("action"); v != "" {
		f.Action = v
	}
	if v := q.Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Since = &t
		}
	}
	if v := q.Get("until"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Until = &t
		}
	}

	result, err := h.db.ListAuditLogs(f)
	if err != nil {
		writeErrorLog(w, "failed to list audit logs", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
