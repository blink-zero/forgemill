package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/factory"
	"github.com/forgemill/forgemill/internal/service"
)

type FactoryHandler struct {
	svc   *service.FactoryService
	audit *service.AuditService
}

func NewFactoryHandler(svc *service.FactoryService, audit *service.AuditService) *FactoryHandler {
	return &FactoryHandler{svc: svc, audit: audit}
}

type startBuildRequest struct {
	OSDefinitionID string `json:"os_definition_id"`
	TargetID       int64  `json:"target_id"`
	TemplateName   string `json:"template_name"` // deprecated: ignored, names are auto-generated
	CPU            int    `json:"cpu"`
	MemoryMB       int    `json:"memory_mb"`
	DiskGB         int    `json:"disk_gb"`
	Datacenter     string `json:"datacenter"`
	Cluster        string `json:"cluster"`
	Datastore      string `json:"datastore"`
	Folder         string `json:"folder"`
	Network        string `json:"network"`
	BuildIP        string `json:"build_ip"`
	IPAddress      string `json:"ip_address"`
	Netmask        string `json:"netmask"`
	Gateway        string `json:"gateway"`
	Node           string `json:"node"`
	StoragePool    string `json:"storage_pool"`
	Bridge         string `json:"bridge"`
	ISOStorage     string `json:"iso_storage"`
}

func (h *FactoryHandler) ListOSDefinitions(w http.ResponseWriter, r *http.Request) {
	defs := h.svc.ListOSDefinitions()
	writeJSON(w, http.StatusOK, defs)
}

func (h *FactoryHandler) GetOSDefinition(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	def, err := h.svc.GetOSDefinition(id)
	if err != nil {
		writeError(w, "OS definition not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, def)
}

func (h *FactoryHandler) StartBuild(w http.ResponseWriter, r *http.Request) {
	var req startBuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.OSDefinitionID == "" {
		writeError(w, "os_definition_id is required", http.StatusBadRequest)
		return
	}
	if req.TargetID == 0 {
		writeError(w, "target_id is required", http.StatusBadRequest)
		return
	}
	// Template names are auto-generated as fm-{os_id}-{target_name}-v{version}.
	// Any user-provided template_name is ignored.
	req.TemplateName = "auto" // placeholder; service overwrites with auto-generated name

	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	cfg := factory.BuildConfig{
		TemplateName: req.TemplateName,
		CPU:          req.CPU,
		MemoryMB:     req.MemoryMB,
		DiskGB:       req.DiskGB,
		Datacenter:   req.Datacenter,
		Cluster:      req.Cluster,
		Datastore:    req.Datastore,
		Folder:       req.Folder,
		Network:      req.Network,
		BuildIP:      firstNonEmpty(req.BuildIP, req.IPAddress),
		Netmask:      req.Netmask,
		Gateway:      req.Gateway,
		Node:         req.Node,
		StoragePool:  req.StoragePool,
		Bridge:       req.Bridge,
		ISOStorage:   req.ISOStorage,
	}

	if err := factory.ValidateBuildConfig(&cfg); err != nil {
		slog.Warn("invalid build configuration", "error", err)
		writeError(w, fmt.Sprintf("Invalid build configuration: %s", err.Error()), http.StatusBadRequest)
		return
	}

	build, err := h.svc.StartBuild(req.OSDefinitionID, req.TargetID, cfg, user.ID)
	if err != nil {
		slog.Error("failed to start build", "error", err)
		// Pass through the actual error — it contains user-actionable info (e.g. "another build in progress")
		writeError(w, err.Error(), http.StatusConflict)
		return
	}

	writeJSON(w, http.StatusAccepted, build)
}

func (h *FactoryHandler) ListBuilds(w http.ResponseWriter, r *http.Request) {
	builds, err := h.svc.ListBuilds()
	if err != nil {
		writeErrorLog(w, "failed to list builds", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, builds)
}

func (h *FactoryHandler) GetBuild(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid build ID", http.StatusBadRequest)
		return
	}

	build, err := h.svc.GetBuild(id)
	if err != nil {
		writeError(w, "build not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, build)
}

// GetBuildHCL returns the stored HCL and autoinstall config for a build (admin only).
func (h *FactoryHandler) GetBuildHCL(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid build ID", http.StatusBadRequest)
		return
	}

	build, err := h.svc.GetBuild(id)
	if err != nil {
		writeError(w, "build not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"packer_template":    build.PackerTemplate,
		"autoinstall_config": build.AutoinstallConfig,
	})
}

func (h *FactoryHandler) CancelBuild(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid build ID", http.StatusBadRequest)
		return
	}

	if err := h.svc.CancelBuild(id); err != nil {
		writeErrorLog(w, "failed to cancel build", http.StatusInternalServerError, err)
		return
	}

	if actor := middleware.UserFromContext(r.Context()); actor != nil {
		slog.Info("audit", "action", "factory.build.cancel", "actor", actor.Username, "build_id", id)
		h.audit.Log(actor.Username, &actor.ID, "factory.build.cancel", "build", strconv.FormatInt(id, 10), service.IPFromRequest(r), map[string]interface{}{"build_id": id})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelling"})
}

func (h *FactoryHandler) DeleteBuild(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid build ID", http.StatusBadRequest)
		return
	}

	if err := h.svc.DeleteBuild(id); err != nil {
		writeErrorLog(w, "failed to delete build", http.StatusInternalServerError, err)
		return
	}

	if actor := middleware.UserFromContext(r.Context()); actor != nil {
		slog.Info("audit", "action", "factory.build.delete", "actor", actor.Username, "build_id", id)
		h.audit.Log(actor.Username, &actor.ID, "factory.build.delete", "build", strconv.FormatInt(id, 10), service.IPFromRequest(r), map[string]interface{}{"build_id": id})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *FactoryHandler) GetPrerequisites(w http.ResponseWriter, r *http.Request) {
	status := h.svc.CheckPrerequisites()
	writeJSON(w, http.StatusOK, status)
}

func (h *FactoryHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"build_running": h.svc.IsRunning()})
}

// --- Phase 5: Update checking ---

func (h *FactoryHandler) CheckAllUpdates(w http.ResponseWriter, r *http.Request) {
	updates, err := h.svc.CheckAllUpdates()
	if err != nil {
		writeErrorLog(w, "failed to check for updates", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, updates)
}

func (h *FactoryHandler) CheckTemplateUpdate(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "templateId")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, "invalid template ID", http.StatusBadRequest)
		return
	}

	update, err := h.svc.CheckTemplateUpdate(id)
	if err != nil {
		writeErrorLog(w, "failed to check template update", http.StatusInternalServerError, err)
		return
	}

	if update == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"update_available": false})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"update_available": true,
		"update":           update,
	})
}

func (h *FactoryHandler) RebuildTemplate(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "templateId")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, "invalid template ID", http.StatusBadRequest)
		return
	}

	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	build, err := h.svc.RebuildTemplate(id, user.ID)
	if err != nil {
		writeErrorLog(w, "failed to rebuild template", http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusAccepted, build)
}

// --- Phase 5: Schedules ---

type createScheduleRequest struct {
	TemplateID         int64  `json:"template_id"`
	BuildConfigJSON    string `json:"build_config_json"`
	Strategy           string `json:"strategy"`
	IntervalDays       int    `json:"interval_days"`
	CheckIntervalHours int    `json:"check_interval_hours"`
	Enabled            bool   `json:"enabled"`
}

// V4-M1: Separate update struct uses *bool to distinguish missing from false,
// preventing silent schedule disabling on partial updates.
type updateScheduleRequest struct {
	Strategy           string `json:"strategy"`
	IntervalDays       int    `json:"interval_days"`
	CheckIntervalHours int    `json:"check_interval_hours"`
	BuildConfigJSON    string `json:"build_config_json"`
	Enabled            *bool  `json:"enabled"`
}

func (h *FactoryHandler) ListSchedules(w http.ResponseWriter, r *http.Request) {
	schedules, err := h.svc.ListSchedules()
	if err != nil {
		writeErrorLog(w, "failed to list schedules", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, schedules)
}

func (h *FactoryHandler) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	var req createScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.TemplateID == 0 {
		writeError(w, "template_id is required", http.StatusBadRequest)
		return
	}
	if req.Strategy == "" {
		req.Strategy = "on_update"
	}
	// SEC-11: Validate strategy against allowed values
	validStrategies := map[string]bool{"interval": true, "on_update": true, "both": true}
	if !validStrategies[req.Strategy] {
		writeError(w, "strategy must be one of: interval, on_update, both", http.StatusBadRequest)
		return
	}
	if req.IntervalDays == 0 {
		req.IntervalDays = 30
	}
	if req.CheckIntervalHours == 0 {
		req.CheckIntervalHours = 24
	}

	nextCheck := time.Now().Add(time.Duration(req.CheckIntervalHours) * time.Hour)
	sched := &models.TemplateSchedule{
		TemplateID:         req.TemplateID,
		BuildConfigJSON:    req.BuildConfigJSON,
		Strategy:           req.Strategy,
		IntervalDays:       req.IntervalDays,
		CheckIntervalHours: req.CheckIntervalHours,
		NextCheckAt:        &nextCheck,
		Enabled:            req.Enabled,
	}

	if err := h.svc.CreateSchedule(sched); err != nil {
		writeErrorLog(w, "failed to create schedule", http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusCreated, sched)
}

func (h *FactoryHandler) GetSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid schedule ID", http.StatusBadRequest)
		return
	}

	sched, err := h.svc.GetSchedule(id)
	if err != nil {
		writeError(w, "schedule not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, sched)
}

func (h *FactoryHandler) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid schedule ID", http.StatusBadRequest)
		return
	}

	sched, err := h.svc.GetSchedule(id)
	if err != nil {
		writeError(w, "schedule not found", http.StatusNotFound)
		return
	}

	// V4-M1: Use updateScheduleRequest with *bool to prevent silent disable on partial update
	var req updateScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Strategy != "" {
		// SEC-11: Validate strategy on update too
		validStrategies := map[string]bool{"interval": true, "on_update": true, "both": true}
		if !validStrategies[req.Strategy] {
			writeError(w, "strategy must be one of: interval, on_update, both", http.StatusBadRequest)
			return
		}
		sched.Strategy = req.Strategy
	}
	if req.IntervalDays > 0 {
		sched.IntervalDays = req.IntervalDays
	}
	if req.CheckIntervalHours > 0 {
		sched.CheckIntervalHours = req.CheckIntervalHours
	}
	if req.BuildConfigJSON != "" {
		sched.BuildConfigJSON = req.BuildConfigJSON
	}
	// V4-M1: Only update Enabled when explicitly provided (non-nil)
	if req.Enabled != nil {
		sched.Enabled = *req.Enabled
	}

	if err := h.svc.UpdateSchedule(sched); err != nil {
		writeErrorLog(w, "failed to update schedule", http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, sched)
}

func (h *FactoryHandler) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid schedule ID", http.StatusBadRequest)
		return
	}

	if err := h.svc.DeleteSchedule(id); err != nil {
		writeErrorLog(w, "failed to delete schedule", http.StatusInternalServerError, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Phase 5: Template History ---

func (h *FactoryHandler) GetTemplateHistory(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid template ID", http.StatusBadRequest)
		return
	}

	history, err := h.svc.GetTemplateHistory(id)
	if err != nil {
		writeErrorLog(w, "failed to get template history", http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, history)
}

func (h *FactoryHandler) CleanupSuperseded(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid template ID", http.StatusBadRequest)
		return
	}

	deleted, err := h.svc.CleanupSuperseded(id)
	if err != nil {
		writeErrorLog(w, "failed to cleanup superseded templates", http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]int64{"deleted": deleted})
}

func (h *FactoryHandler) ListTemplateFamilies(w http.ResponseWriter, r *http.Request) {
	families, err := h.svc.ListTemplateFamilies()
	if err != nil {
		writeErrorLog(w, "failed to list template families", http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, families)
}

func (h *FactoryHandler) GetFamilyHistory(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid family ID", http.StatusBadRequest)
		return
	}

	templates, err := h.svc.GetTemplatesByFamily(id)
	if err != nil {
		writeErrorLog(w, "failed to get family history", http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, templates)
}
