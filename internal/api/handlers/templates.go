package handlers

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/service"
)

type TemplateHandler struct {
	svc   *service.TemplateService
	audit *service.AuditService
}

func NewTemplateHandler(svc *service.TemplateService, audit *service.AuditService) *TemplateHandler {
	return &TemplateHandler{svc: svc, audit: audit}
}

func (h *TemplateHandler) List(w http.ResponseWriter, r *http.Request) {
	templates, err := h.svc.List()
	if err != nil {
		writeError(w, "failed to list templates", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, templates)
}

func (h *TemplateHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	tpl, err := h.svc.Get(id)
	if err != nil {
		writeError(w, "template not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, tpl)
}

func (h *TemplateHandler) GetDetail(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	detail, err := h.svc.GetDetail(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, "template not found", http.StatusNotFound)
			return
		}
		writeErrorLog(w, "failed to get template detail", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (h *TemplateHandler) DeletePreview(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	preview, err := h.svc.DeletePreview(id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, "template not found", http.StatusNotFound)
			return
		}
		writeErrorLog(w, "failed to get delete preview", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (h *TemplateHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	destroy := r.URL.Query().Get("destroy") == "true"
	keepVMs := r.URL.Query().Get("keep_vms") == "true"
	if err := h.svc.Delete(r.Context(), id, destroy, keepVMs); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, "template not found", http.StatusNotFound)
			return
		}
		writeErrorLog(w, "failed to delete template", http.StatusInternalServerError, err)
		return
	}
	if actor := middleware.UserFromContext(r.Context()); actor != nil {
		slog.Info("audit",
			"action", "template.delete",
			"actor", actor.Username,
			"template_id", id,
			"destroy", destroy,
			"keep_vms", keepVMs,
		)
		h.audit.Log(actor.Username, &actor.ID, "template.delete", "template", strconv.FormatInt(id, 10), service.IPFromRequest(r), map[string]interface{}{"template_id": id, "destroy": destroy, "keep_vms": keepVMs})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
