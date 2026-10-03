package handlers

import (
	"net/http"
	"time"

	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/service"
	"github.com/forgemill/forgemill/internal/version"
)

// DiagnosticsHandler serves the admin operational snapshot: what an
// operator would otherwise need the server log for.
type DiagnosticsHandler struct {
	db  *db.DB
	vms *service.VMService
}

func NewDiagnosticsHandler(database *db.DB, vms *service.VMService) *DiagnosticsHandler {
	return &DiagnosticsHandler{db: database, vms: vms}
}

type diagnosticsTarget struct {
	ID              int64                   `json:"id"`
	Name            string                  `json:"name"`
	Type            string                  `json:"type"`
	Status          string                  `json:"status"`
	LastConnectedAt *time.Time              `json:"last_connected_at"`
	LastSync        *service.TargetSyncInfo `json:"last_sync"`
}

type diagnosticsFailedDeployment struct {
	ID           int64      `json:"id"`
	VMName       string     `json:"vm_name"`
	TargetName   string     `json:"target_name"`
	ErrorMessage string     `json:"error_message"`
	CompletedAt  *time.Time `json:"completed_at"`
}

type diagnosticsResponse struct {
	GeneratedAt             time.Time                     `json:"generated_at"`
	Build                   map[string]string             `json:"build"`
	Targets                 []diagnosticsTarget           `json:"targets"`
	RecentVMEvents          []models.VMEvent              `json:"recent_vm_events"`
	RecentFailedDeployments []diagnosticsFailedDeployment `json:"recent_failed_deployments"`
	RecentServerErrors      []ServerError                 `json:"recent_server_errors"`
	RateLimitedRequests     int64                         `json:"rate_limited_requests"`
}

// Get assembles the snapshot. Each section degrades independently: a DB
// error in one list is reported in that list's place, not as a 500 for
// the whole page — this endpoint exists for when things are already wrong.
func (h *DiagnosticsHandler) Get(w http.ResponseWriter, r *http.Request) {
	resp := diagnosticsResponse{
		GeneratedAt:         time.Now().UTC(),
		Build:               map[string]string{"version": version.Version, "commit": version.Commit, "date": version.Date},
		Targets:             []diagnosticsTarget{},
		RecentVMEvents:      []models.VMEvent{},
		RecentServerErrors:  RecentServerErrors(),
		RateLimitedRequests: middleware.RateLimitRejections(),
	}

	lastSync := h.vms.LastSyncByTarget()
	if targets, err := h.db.ListTargets(); err == nil {
		for _, t := range targets {
			dt := diagnosticsTarget{ID: t.ID, Name: t.Name, Type: t.Type, Status: t.Status, LastConnectedAt: t.LastConnectedAt}
			if info, ok := lastSync[t.ID]; ok {
				info := info
				dt.LastSync = &info
			}
			resp.Targets = append(resp.Targets, dt)
		}
	} else {
		recordServerError(http.StatusInternalServerError, "diagnostics: list targets", err)
	}

	if events, err := h.db.ListRecentVMEvents(50, true); err == nil {
		resp.RecentVMEvents = events
	} else {
		recordServerError(http.StatusInternalServerError, "diagnostics: list VM events", err)
	}

	resp.RecentFailedDeployments = []diagnosticsFailedDeployment{}
	if page, err := h.db.ListDeployments(db.DeploymentFilter{Status: "failed", Page: 1, PerPage: 20}); err == nil {
		for _, d := range page.Data {
			resp.RecentFailedDeployments = append(resp.RecentFailedDeployments, diagnosticsFailedDeployment{
				ID: d.ID, VMName: d.VMName, TargetName: d.TargetName, ErrorMessage: d.ErrorMessage, CompletedAt: d.CompletedAt,
			})
		}
	} else {
		recordServerError(http.StatusInternalServerError, "diagnostics: list failed deployments", err)
	}

	writeJSON(w, http.StatusOK, resp)
}
