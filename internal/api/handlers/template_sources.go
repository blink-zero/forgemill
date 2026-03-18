package handlers

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"

	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
)

// SEC-6: isPrivateIP checks if an IP address is in a private/reserved range (SSRF protection).
func isPrivateIP(ip net.IP) bool {
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
	}
	for _, cidr := range privateRanges {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// V3-L1: validateSourceURL performs URL format validation.
// SEC-6: Added SSRF protection — resolves hostname and rejects private IPs.
func validateSourceURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if !(u.Scheme == "http" || u.Scheme == "https") || u.Host == "" {
		return false
	}
	// SEC-6: Resolve hostname and check for private/reserved IPs
	hostname := u.Hostname()
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return false
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return false
		}
	}
	return true
}

type TemplateSourceHandler struct {
	db *db.DB
}

func NewTemplateSourceHandler(db *db.DB) *TemplateSourceHandler {
	return &TemplateSourceHandler{db: db}
}

type createTemplateSourceRequest struct {
	Name                string `json:"name"`
	OSType              string `json:"os_type"`
	ISOURL              string `json:"iso_url"`
	ChecksumURL         string `json:"checksum_url"`
	PackerConfig        string `json:"packer_config"`
	AutoRefresh         bool   `json:"auto_refresh"`
	RefreshIntervalDays int    `json:"refresh_interval_days"`
	TargetID            int64  `json:"target_id"`
}

func (h *TemplateSourceHandler) List(w http.ResponseWriter, r *http.Request) {
	sources, err := h.db.ListTemplateSources()
	if err != nil {
		writeErrorLog(w, "failed to list template sources", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, sources)
}

func (h *TemplateSourceHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createTemplateSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.OSType == "" || req.ISOURL == "" {
		writeError(w, "name, os_type, and iso_url are required", http.StatusBadRequest)
		return
	}

	// V3-L1: Validate URL format for ISO and checksum URLs
	if !validateSourceURL(req.ISOURL) {
		writeError(w, "iso_url must be a valid http or https URL", http.StatusBadRequest)
		return
	}
	if req.ChecksumURL != "" && !validateSourceURL(req.ChecksumURL) {
		writeError(w, "checksum_url must be a valid http or https URL", http.StatusBadRequest)
		return
	}

	if req.RefreshIntervalDays == 0 {
		req.RefreshIntervalDays = 30
	}

	ts := &models.TemplateSource{
		Name:                req.Name,
		OSType:              req.OSType,
		ISOURL:              req.ISOURL,
		ChecksumURL:         req.ChecksumURL,
		PackerConfig:        req.PackerConfig,
		AutoRefresh:         req.AutoRefresh,
		RefreshIntervalDays: req.RefreshIntervalDays,
		TargetID:            req.TargetID,
	}

	if err := h.db.CreateTemplateSource(ts); err != nil {
		writeErrorLog(w, "failed to create template source", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, ts)
}

func (h *TemplateSourceHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	ts, err := h.db.GetTemplateSource(id)
	if err != nil {
		writeError(w, "template source not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, ts)
}

type updateTemplateSourceRequest struct {
	Name                string `json:"name"`
	OSType              string `json:"os_type"`
	ISOURL              string `json:"iso_url"`
	ChecksumURL         string `json:"checksum_url"`
	PackerConfig        string `json:"packer_config"`
	AutoRefresh         *bool  `json:"auto_refresh"`
	RefreshIntervalDays int    `json:"refresh_interval_days"`
	TargetID            int64  `json:"target_id"`
}

func (h *TemplateSourceHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}

	existing, err := h.db.GetTemplateSource(id)
	if err != nil {
		writeError(w, "template source not found", http.StatusNotFound)
		return
	}

	var req updateTemplateSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.OSType != "" {
		existing.OSType = req.OSType
	}
	if req.ISOURL != "" {
		// V3-L1: Validate URL format
		if !validateSourceURL(req.ISOURL) {
			writeError(w, "iso_url must be a valid http or https URL", http.StatusBadRequest)
			return
		}
		existing.ISOURL = req.ISOURL
	}
	if req.ChecksumURL != "" {
		if !validateSourceURL(req.ChecksumURL) {
			writeError(w, "checksum_url must be a valid http or https URL", http.StatusBadRequest)
			return
		}
		existing.ChecksumURL = req.ChecksumURL
	}
	if req.PackerConfig != "" {
		existing.PackerConfig = req.PackerConfig
	}
	if req.AutoRefresh != nil {
		existing.AutoRefresh = *req.AutoRefresh
	}
	if req.RefreshIntervalDays > 0 {
		existing.RefreshIntervalDays = req.RefreshIntervalDays
	}
	if req.TargetID > 0 {
		existing.TargetID = req.TargetID
	}

	if err := h.db.UpdateTemplateSource(existing); err != nil {
		writeErrorLog(w, "failed to update template source", http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (h *TemplateSourceHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, "invalid ID", http.StatusBadRequest)
		return
	}
	if err := h.db.DeleteTemplateSource(id); err != nil {
		writeErrorLog(w, "failed to delete template source", http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
