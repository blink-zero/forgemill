package handlers

import (
	"net/http"

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

// Status: GET /api/ai/status
func (h *AIHandler) Status(w http.ResponseWriter, r *http.Request) {
	st := h.svc.Status()
	if u := middleware.UserFromContext(r.Context()); u == nil || u.Role != "admin" {
		// Non-admins get the switch, not the configuration.
		st = service.AIStatus{Enabled: st.Enabled, Configured: st.Configured, RedactHostnames: st.RedactHostnames}
	}
	writeJSON(w, http.StatusOK, st)
}

// Test: POST /api/ai/test — round trip against the configured provider.
func (h *AIHandler) Test(w http.ResponseWriter, r *http.Request) {
	actor, actorID := "api", (*int64)(nil)
	if u := middleware.UserFromContext(r.Context()); u != nil {
		actor, actorID = u.Username, &u.ID
	}
	writeJSON(w, http.StatusOK, h.svc.Test(r.Context(), actor, actorID))
}
