package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/service"
)

// DiscoveryHandler: list what a target has that Forgemill doesn't manage,
// and take it under management.
type DiscoveryHandler struct {
	svc   *service.VMService
	audit *service.AuditService
}

func NewDiscoveryHandler(svc *service.VMService, audit *service.AuditService) *DiscoveryHandler {
	return &DiscoveryHandler{svc: svc, audit: audit}
}

func targetIDParam(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

// Discover: GET /targets/{id}/discover?include_ignored=true
func (h *DiscoveryHandler) Discover(w http.ResponseWriter, r *http.Request) {
	id, err := targetIDParam(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	res, err := h.svc.Discover(r.Context(), id, r.URL.Query().Get("include_ignored") == "true")
	if err != nil {
		if errors.Is(err, service.ErrTargetNotFound) {
			writeError(w, "target not found", http.StatusNotFound)
			return
		}
		writeErrorLog(w, "failed to discover VMs on target", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type adoptRequest struct {
	VMRefs []string `json:"vm_refs"`
}

// Adopt: POST /targets/{id}/adopt {vm_refs}
func (h *DiscoveryHandler) Adopt(w http.ResponseWriter, r *http.Request) {
	id, err := targetIDParam(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	var req adoptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.VMRefs) == 0 {
		writeError(w, "vm_refs is required", http.StatusBadRequest)
		return
	}
	actor := middleware.UserFromContext(r.Context())
	var actorID int64
	actorName := "api"
	if actor != nil {
		actorID, actorName = actor.ID, actor.Username
	}
	res, err := h.svc.Adopt(r.Context(), id, req.VMRefs, actorID, actorName)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrTargetNotFound):
			writeError(w, "target not found", http.StatusNotFound)
		case errors.Is(err, service.ErrInvalidNICSpec):
			writeError(w, err.Error(), http.StatusBadRequest)
		default:
			writeErrorLog(w, "failed to adopt VMs", http.StatusInternalServerError, err)
		}
		return
	}
	if actor != nil {
		ids := make([]int64, 0, len(res.Adopted))
		for _, vm := range res.Adopted {
			ids = append(ids, vm.ID)
		}
		h.audit.Log(actor.Username, &actor.ID, "vm.adopt", "target", fmt.Sprintf("%d", id), service.IPFromRequest(r), map[string]interface{}{
			"requested": len(req.VMRefs), "adopted": len(res.Adopted), "skipped": len(res.Skipped), "vm_ids": ids,
		})
	}
	writeJSON(w, http.StatusCreated, res)
}

type ignoreRequest struct {
	VMRefs []string          `json:"vm_refs"`
	Names  map[string]string `json:"names,omitempty"` // ref → display name, optional
}

// Ignore: POST /targets/{id}/ignore {vm_refs, names?}
func (h *DiscoveryHandler) Ignore(w http.ResponseWriter, r *http.Request) {
	id, err := targetIDParam(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	var req ignoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.VMRefs) == 0 {
		writeError(w, "vm_refs is required", http.StatusBadRequest)
		return
	}
	actor := middleware.UserFromContext(r.Context())
	var actorID int64
	if actor != nil {
		actorID = actor.ID
	}
	if err := h.svc.IgnoreDiscovered(id, req.VMRefs, req.Names, actorID); err != nil {
		if errors.Is(err, service.ErrTargetNotFound) {
			writeError(w, "target not found", http.StatusNotFound)
			return
		}
		writeErrorLog(w, "failed to ignore VMs", http.StatusInternalServerError, err)
		return
	}
	if actor != nil {
		h.audit.Log(actor.Username, &actor.ID, "vm.ignore", "target", fmt.Sprintf("%d", id), service.IPFromRequest(r), map[string]interface{}{"vm_refs": req.VMRefs})
	}
	w.WriteHeader(http.StatusNoContent)
}

// Unignore: DELETE /targets/{id}/ignore {vm_refs}
func (h *DiscoveryHandler) Unignore(w http.ResponseWriter, r *http.Request) {
	id, err := targetIDParam(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	var req ignoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.VMRefs) == 0 {
		writeError(w, "vm_refs is required", http.StatusBadRequest)
		return
	}
	if err := h.svc.UnignoreDiscovered(id, req.VMRefs); err != nil {
		if errors.Is(err, service.ErrTargetNotFound) {
			writeError(w, "target not found", http.StatusNotFound)
			return
		}
		writeErrorLog(w, "failed to un-ignore VMs", http.StatusInternalServerError, err)
		return
	}
	if actor := middleware.UserFromContext(r.Context()); actor != nil {
		h.audit.Log(actor.Username, &actor.ID, "vm.unignore", "target", fmt.Sprintf("%d", id), service.IPFromRequest(r), map[string]interface{}{"vm_refs": req.VMRefs})
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListIgnored: GET /targets/{id}/ignored
func (h *DiscoveryHandler) ListIgnored(w http.ResponseWriter, r *http.Request) {
	id, err := targetIDParam(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	list, err := h.svc.ListIgnored(id)
	if err != nil {
		if errors.Is(err, service.ErrTargetNotFound) {
			writeError(w, "target not found", http.StatusNotFound)
			return
		}
		writeErrorLog(w, "failed to list ignored VMs", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}
