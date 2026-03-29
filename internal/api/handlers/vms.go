package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/service"
)

type VMHandler struct {
	svc   *service.VMService
	audit *service.AuditService
}

func NewVMHandler(svc *service.VMService, audit *service.AuditService) *VMHandler {
	return &VMHandler{svc: svc, audit: audit}
}

func (h *VMHandler) List(w http.ResponseWriter, r *http.Request) {
	vms, err := h.svc.List()
	if err != nil {
		writeErrorLog(w, "failed to list VMs", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, vms)
}

func (h *VMHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	vm, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeError(w, "VM not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, vm)
}

func (h *VMHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	force := r.URL.Query().Get("force") == "true"
	if err := h.svc.Delete(r.Context(), id, force); err != nil {
		writeErrorLog(w, "failed to delete VM", http.StatusInternalServerError, err)
		return
	}
	if actor := middleware.UserFromContext(r.Context()); actor != nil {
		h.audit.Log(actor.Username, &actor.ID, "vm.delete", "vm", fmt.Sprintf("%d", id), service.IPFromRequest(r), map[string]interface{}{"force": force})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *VMHandler) PowerAction(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	action := chi.URLParam(r, "action")
	if action != "start" && action != "stop" && action != "restart" && action != "suspend" {
		writeError(w, "invalid power action", http.StatusBadRequest)
		return
	}
	if err := h.svc.PowerAction(r.Context(), id, action); err != nil {
		writeErrorLog(w, "power action failed", http.StatusInternalServerError, err)
		return
	}
	if actor := middleware.UserFromContext(r.Context()); actor != nil {
		h.audit.Log(actor.Username, &actor.ID, "vm.power."+action, "vm", fmt.Sprintf("%d", id), service.IPFromRequest(r), nil)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": action})
}

func (h *VMHandler) SyncAll(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.SyncAll(r.Context())
	if err != nil {
		writeErrorLog(w, "sync-all failed", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *VMHandler) SyncOne(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	vm, err := h.svc.SyncState(r.Context(), id)
	if err != nil {
		writeErrorLog(w, "failed to sync VM", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, vm)
}

func (h *VMHandler) ListSnapshots(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	snapshots, err := h.svc.ListSnapshots(id)
	if err != nil {
		writeErrorLog(w, "failed to list snapshots", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, snapshots)
}

type createSnapshotRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Memory      bool   `json:"memory"`
}

func (h *VMHandler) CreateSnapshot(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	var req createSnapshotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		writeError(w, "snapshot name is required", http.StatusBadRequest)
		return
	}
	if err := h.svc.CreateSnapshot(r.Context(), id, req.Name, req.Description, req.Memory); err != nil {
		writeErrorLog(w, "failed to create snapshot", http.StatusInternalServerError, err)
		return
	}
	if actor := middleware.UserFromContext(r.Context()); actor != nil {
		h.audit.Log(actor.Username, &actor.ID, "vm.snapshot.create", "vm", fmt.Sprintf("%d", id), service.IPFromRequest(r), map[string]interface{}{"snapshot_name": req.Name})
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}

func (h *VMHandler) RevertSnapshot(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	snapID, err := strconv.ParseInt(chi.URLParam(r, "snapId"), 10, 64)
	if err != nil {
		writeError(w, "invalid snapshot ID", http.StatusBadRequest)
		return
	}
	if err := h.svc.RevertSnapshot(r.Context(), id, snapID); err != nil {
		writeErrorLog(w, "failed to revert snapshot", http.StatusInternalServerError, err)
		return
	}
	if actor := middleware.UserFromContext(r.Context()); actor != nil {
		h.audit.Log(actor.Username, &actor.ID, "vm.snapshot.revert", "vm", fmt.Sprintf("%d", id), service.IPFromRequest(r), map[string]interface{}{"snapshot_id": snapID})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reverted"})
}

func (h *VMHandler) DeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	snapID, err := strconv.ParseInt(chi.URLParam(r, "snapId"), 10, 64)
	if err != nil {
		writeError(w, "invalid snapshot ID", http.StatusBadRequest)
		return
	}
	if err := h.svc.DeleteSnapshot(r.Context(), id, snapID); err != nil {
		writeErrorLog(w, "failed to delete snapshot", http.StatusInternalServerError, err)
		return
	}
	if actor := middleware.UserFromContext(r.Context()); actor != nil {
		h.audit.Log(actor.Username, &actor.ID, "vm.snapshot.delete", "vm", fmt.Sprintf("%d", id), service.IPFromRequest(r), map[string]interface{}{"snapshot_id": snapID})
	}
	w.WriteHeader(http.StatusNoContent)
}

type resizeRequest struct {
	CPU      int `json:"cpu"`
	MemoryMB int `json:"memory_mb"`
}

func (h *VMHandler) Resize(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	var req resizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	// F-66: Reject negative values individually, then require at least one positive value
	if req.CPU < 0 || req.MemoryMB < 0 {
		writeError(w, "cpu and memory_mb must not be negative", http.StatusBadRequest)
		return
	}
	if req.CPU == 0 && req.MemoryMB == 0 {
		writeError(w, "cpu or memory_mb is required", http.StatusBadRequest)
		return
	}
	if err := h.svc.Resize(r.Context(), id, req.CPU, req.MemoryMB); err != nil {
		if strings.Contains(err.Error(), "must be powered off") || strings.Contains(err.Error(), "hot-add") {
			writeError(w, "VM must be powered off to change this resource (hot-add not enabled)", http.StatusConflict)
			return
		}
		writeErrorLog(w, "failed to resize VM", http.StatusInternalServerError, err)
		return
	}
	if actor := middleware.UserFromContext(r.Context()); actor != nil {
		h.audit.Log(actor.Username, &actor.ID, "vm.resize", "vm", fmt.Sprintf("%d", id), service.IPFromRequest(r), map[string]interface{}{"cpu": req.CPU, "memory_mb": req.MemoryMB})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "resized"})
}

type expandDiskRequest struct {
	NewSizeGB int `json:"new_size_gb"`
}

func (h *VMHandler) ListDisks(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	disks, err := h.svc.ListDisks(r.Context(), id)
	if err != nil {
		writeErrorLog(w, "failed to list disks", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, disks)
}

func (h *VMHandler) ExpandDisk(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	diskKey, err := strconv.ParseInt(chi.URLParam(r, "key"), 10, 64)
	if err != nil {
		writeError(w, "invalid disk key", http.StatusBadRequest)
		return
	}
	var req expandDiskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.NewSizeGB <= 0 {
		writeError(w, "new_size_gb is required", http.StatusBadRequest)
		return
	}
	if err := h.svc.ExpandDisk(r.Context(), id, int(diskKey), req.NewSizeGB); err != nil {
		writeErrorLog(w, "failed to expand disk", http.StatusInternalServerError, err)
		return
	}
	if actor := middleware.UserFromContext(r.Context()); actor != nil {
		h.audit.Log(actor.Username, &actor.ID, "vm.disk.expand", "vm", fmt.Sprintf("%d", id), service.IPFromRequest(r), map[string]interface{}{"disk_key": diskKey, "new_size_gb": req.NewSizeGB})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "expanded"})
}

func (h *VMHandler) GetConsoleURL(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	url, err := h.svc.GetConsoleURL(r.Context(), id)
	if err != nil {
		writeErrorLog(w, "failed to get console URL", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

// HIGH-03: Dedicated request struct to prevent mass assignment
type registerVMRequest struct {
	VMName   string `json:"vm_name"`
	VMRef    string `json:"vm_ref"`
	TargetID int64  `json:"target_id"`
}

func (h *VMHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerVMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.VMName == "" || req.VMRef == "" || req.TargetID == 0 {
		writeError(w, "vm_name, vm_ref, and target_id are required", http.StatusBadRequest)
		return
	}
	vm := &models.ManagedVM{
		VMName:   req.VMName,
		VMRef:    req.VMRef,
		TargetID: req.TargetID,
	}
	if err := h.svc.Create(vm); err != nil {
		if strings.Contains(err.Error(), "already registered") {
			writeError(w, "a VM with this reference is already registered", http.StatusConflict)
			return
		}
		writeErrorLog(w, "failed to register VM", http.StatusInternalServerError, err)
		return
	}
	if actor := middleware.UserFromContext(r.Context()); actor != nil {
		h.audit.Log(actor.Username, &actor.ID, "vm.register", "vm", fmt.Sprintf("%d", vm.ID), service.IPFromRequest(r), map[string]interface{}{"vm_name": vm.VMName, "target_id": vm.TargetID})
	}
	writeJSON(w, http.StatusCreated, vm)
}

func (h *VMHandler) ResetHostKey(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	if err := h.svc.ResetHostKey(id); err != nil {
		writeErrorLog(w, "failed to reset host key", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "host key reset"})
}

func (h *VMHandler) GetCredentials(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	creds, err := h.svc.GetCredentials(r.Context(), id)
	if err != nil {
		writeErrorLog(w, "credentials not available", http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, creds)
}
