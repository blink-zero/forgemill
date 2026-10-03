package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/forgemill/forgemill/internal/crypto"
	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/service"
)

func TestDiagnosticsAssemblesEverySectionAndDegradesGracefully(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "d.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	enc, _ := crypto.NewEncryptor("0123456789abcdef0123456789abcdef")
	target := &models.Target{Name: "lab", Type: "vcenter", Hostname: "vc.example.com", Port: 443, Username: "u", PasswordEncrypt: "enc"}
	if err := database.CreateTarget(target); err != nil {
		t.Fatal(err)
	}
	if err := database.AddVMEvent(1, target.ID, "warn", "Network adapter added but the connect reconfigure failed"); err != nil {
		t.Fatal(err)
	}
	if err := database.AddVMEvent(1, target.ID, "info", "Attached disk"); err != nil {
		t.Fatal(err)
	}
	// One server error recorded through the same path the handlers use.
	writeErrorLog(httptest.NewRecorder(), "failed to add disk", http.StatusInternalServerError, errors.New("vSphere exploded"))

	vms := service.NewVMService(database, service.NewTargetService(database, enc), enc)
	h := NewDiagnosticsHandler(database, vms)
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/diagnostics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp diagnosticsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Build["version"] == "" || resp.GeneratedAt.IsZero() {
		t.Errorf("build/generated_at missing: %+v", resp.Build)
	}
	if len(resp.Targets) != 1 || resp.Targets[0].Name != "lab" || resp.Targets[0].LastSync != nil {
		t.Errorf("targets: %+v (no sync has run, so last_sync must be null)", resp.Targets)
	}
	if len(resp.RecentVMEvents) != 1 || resp.RecentVMEvents[0].Level != "warn" {
		t.Errorf("recent VM events should be warn/error only: %+v", resp.RecentVMEvents)
	}
	if resp.RecentFailedDeployments == nil || len(resp.RecentFailedDeployments) != 0 {
		t.Errorf("failed deployments should be an empty list, got %v", resp.RecentFailedDeployments)
	}
	found := false
	for _, e := range resp.RecentServerErrors {
		if e.Message == "failed to add disk" && e.Error == "vSphere exploded" && e.Status == 500 {
			found = true
		}
	}
	if !found {
		t.Errorf("server error recorded by writeErrorLog should be listed: %+v", resp.RecentServerErrors)
	}
}

func TestRecentServerErrorsIsBoundedAndNewestFirst(t *testing.T) {
	for i := 0; i < recentServerErrorsCap+10; i++ {
		recordServerError(500, "msg", errors.New("e"))
	}
	recordServerError(502, "latest", errors.New("last one"))
	got := RecentServerErrors()
	if len(got) != recentServerErrorsCap {
		t.Errorf("ring must be capped at %d, got %d", recentServerErrorsCap, len(got))
	}
	if got[0].Message != "latest" || got[0].Status != 502 {
		t.Errorf("newest first, got %+v", got[0])
	}
}
