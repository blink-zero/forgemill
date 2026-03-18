package handlers

import (
	"net/http"
	"strconv"

	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/service"
)

type HistoryHandler struct {
	svc *service.DeployService
}

func NewHistoryHandler(svc *service.DeployService) *HistoryHandler {
	return &HistoryHandler{svc: svc}
}

func (h *HistoryHandler) List(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	targetID, _ := strconv.ParseInt(r.URL.Query().Get("target_id"), 10, 64)

	filter := db.DeploymentFilter{
		Status:   r.URL.Query().Get("status"),
		TargetID: targetID,
		Search:   r.URL.Query().Get("search"),
		Page:     page,
		PerPage:  perPage,
	}

	result, err := h.svc.ListHistory(filter)
	if err != nil {
		writeError(w, "failed to list history", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *HistoryHandler) Detail(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	deployment, err := h.svc.Get(id)
	if err != nil {
		writeError(w, "deployment not found", http.StatusNotFound)
		return
	}
	// SEC-1: Ownership check — only creator or admin can view deployment details (IDOR fix)
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if deployment.CreatedBy != user.ID && user.Role != "admin" {
		writeError(w, "forbidden", http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, deployment)
}
