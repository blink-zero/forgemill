package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/forgemill/forgemill/internal/provider"
)

// Any handler that reports a hypervisor error through writeErrorLog turns
// the license gate into one plain 409, not a generic 500.
func TestWriteErrorLogExplainsLicenseRestriction(t *testing.T) {
	w := httptest.NewRecorder()
	writeErrorLog(w, "power action failed", http.StatusInternalServerError, errors.New("power on: ServerFaultCode: Current license or ESXi version prohibits execution of the requested operation."))
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != provider.LicenseRestrictedMessage || !strings.Contains(body["error"], "free vSphere Hypervisor") {
		t.Errorf("unexpected message: %q", body["error"])
	}

	w = httptest.NewRecorder()
	writeErrorLog(w, "power action failed", http.StatusInternalServerError, errors.New("boom"))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("other errors keep their status, got %d", w.Code)
	}
}
