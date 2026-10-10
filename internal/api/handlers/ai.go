package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/forgemill/forgemill/internal/ai"
	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/service"
)

// AIHandler exposes AI assistance. Status is for any signed-in user (the
// editor needs to know whether to show the buttons); everything that calls
// a model is admin-only, like creating actions.
type AIHandler struct {
	svc *service.AIAssistService
}

func NewAIHandler(svc *service.AIAssistService) *AIHandler { return &AIHandler{svc: svc} }

// aiRequestDeadline is how long a model-backed request may take end to end.
// The server's global write timeout (60 s) suits every other endpoint; a
// draft is two model calls back to back and a large model can need more,
// so these handlers extend their own deadline instead of raising it for all.
const aiRequestDeadline = 12 * time.Minute

func extendDeadline(w http.ResponseWriter) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(aiRequestDeadline))
	_ = rc.SetReadDeadline(time.Now().Add(aiRequestDeadline))
}

// Status: GET /api/ai/status
func (h *AIHandler) Status(w http.ResponseWriter, r *http.Request) {
	st := h.svc.Status()
	if u := middleware.UserFromContext(r.Context()); u == nil || u.Role != "admin" {
		// Non-admins get the switch, not the configuration — exactly these
		// three fields, nothing about keys or endpoints.
		writeJSON(w, http.StatusOK, map[string]any{"enabled": st.Enabled, "configured": st.Configured, "redact_hostnames": st.RedactHostnames})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// Test: POST /api/ai/test — round trip against the configured provider.
func (h *AIHandler) Test(w http.ResponseWriter, r *http.Request) {
	extendDeadline(w)
	actor, actorID := "api", (*int64)(nil)
	if u := middleware.UserFromContext(r.Context()); u != nil {
		actor, actorID = u.Username, &u.ID
	}
	writeJSON(w, http.StatusOK, h.svc.Test(r.Context(), actor, actorID))
}

func (h *AIHandler) actor(r *http.Request) (string, *int64) {
	if u := middleware.UserFromContext(r.Context()); u != nil {
		return u.Username, &u.ID
	}
	return "api", nil
}

func decodeReviewInput(w http.ResponseWriter, r *http.Request) (service.ActionReviewInput, bool) {
	var in service.ActionReviewInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, ai.MaxInputBytes+64*1024)).Decode(&in); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return in, false
	}
	return in, true
}

func writeAIError(w http.ResponseWriter, what string, err error) {
	switch {
	case errors.Is(err, service.ErrAIInput):
		writeError(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ai.ErrNotConfigured):
		writeError(w, err.Error(), http.StatusConflict)
	case errors.Is(err, service.ErrAIRateLimited):
		writeError(w, err.Error(), http.StatusTooManyRequests)
	default:
		var pe *ai.ProviderError
		if errors.As(err, &pe) {
			writeError(w, "The model could not be reached or refused: "+pe.Message, http.StatusBadGateway)
			return
		}
		writeErrorLog(w, what, http.StatusInternalServerError, err)
	}
}

// LintAction: POST /api/ai/actions/lint — deterministic checks only; works with AI off.
func (h *AIHandler) LintAction(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeReviewInput(w, r)
	if !ok {
		return
	}
	review, err := h.svc.LintOnly(in)
	if err != nil {
		writeAIError(w, "lint failed", err)
		return
	}
	writeJSON(w, http.StatusOK, review)
}

// ReviewAction: POST /api/ai/actions/review — lint plus the model's review
// when AI assistance is on. A model failure still returns the lint result.
func (h *AIHandler) ReviewAction(w http.ResponseWriter, r *http.Request) {
	extendDeadline(w)
	in, ok := decodeReviewInput(w, r)
	if !ok {
		return
	}
	actor, actorID := h.actor(r)
	review, err := h.svc.ReviewAction(r.Context(), in, actor, actorID)
	if err != nil {
		writeAIError(w, "review failed", err)
		return
	}
	writeJSON(w, http.StatusOK, review)
}

// DraftAction: POST /api/ai/actions/draft — a complete, validated, reviewed
// action from a description. Needs AI assistance on (409 otherwise).
func (h *AIHandler) DraftAction(w http.ResponseWriter, r *http.Request) {
	extendDeadline(w)
	var in service.ActionDraftInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, ai.MaxInputBytes+64*1024)).Decode(&in); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	actor, actorID := h.actor(r)
	draft, err := h.svc.DraftAction(r.Context(), in, actor, actorID)
	if err != nil {
		writeAIError(w, "draft failed", err)
		return
	}
	writeJSON(w, http.StatusOK, draft)
}

// ListModels: GET /api/ai/models — what the configured provider offers.
func (h *AIHandler) ListModels(w http.ResponseWriter, r *http.Request) {
	extendDeadline(w)
	models, err := h.svc.ListModels(r.Context())
	if err != nil {
		writeAIError(w, "list models failed", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

// Background jobs: start returns 202 with the job; poll until done/failed.
// Each poll is a short request, so proxies with default timeouts are fine.

func (h *AIHandler) StartReviewJob(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeReviewInput(w, r)
	if !ok {
		return
	}
	actor, actorID := h.actor(r)
	job, err := h.svc.StartReviewJob(in, actor, actorID)
	if err != nil {
		writeAIJobError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (h *AIHandler) StartDraftJob(w http.ResponseWriter, r *http.Request) {
	var in service.ActionDraftInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, ai.MaxInputBytes+64*1024)).Decode(&in); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	actor, actorID := h.actor(r)
	job, err := h.svc.StartDraftJob(in, actor, actorID)
	if err != nil {
		writeAIJobError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

// GetJob: GET /api/ai/jobs/{id}
func (h *AIHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	_, actorID := h.actor(r)
	admin := false
	if u := middleware.UserFromContext(r.Context()); u != nil && u.Role == "admin" {
		admin = true
	}
	job, err := h.svc.GetJob(chi.URLParam(r, "id"), actorID, admin)
	if err != nil {
		writeError(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func writeAIJobError(w http.ResponseWriter, err error) {
	if errors.Is(err, service.ErrAIBusy) {
		writeError(w, err.Error(), http.StatusTooManyRequests)
		return
	}
	writeAIError(w, "could not start job", err)
}

func decodeFixInput(w http.ResponseWriter, r *http.Request) (service.ActionFixInput, bool) {
	var in service.ActionFixInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, ai.MaxInputBytes+128*1024)).Decode(&in); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return in, false
	}
	return in, true
}

// AutoFix: POST /api/ai/actions/autofix — deterministic fixes only (AI off is fine).
func (h *AIHandler) AutoFix(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeFixInput(w, r)
	if !ok {
		return
	}
	res, err := h.svc.AutoFixOnly(in)
	if err != nil {
		writeAIError(w, "autofix failed", err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// StartFixJob: POST /api/ai/jobs/fix — deterministic + model fixes, polled.
func (h *AIHandler) StartFixJob(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeFixInput(w, r)
	if !ok {
		return
	}
	actor, actorID := h.actor(r)
	job, err := h.svc.StartFixJob(in, actor, actorID)
	if err != nil {
		writeAIJobError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}
