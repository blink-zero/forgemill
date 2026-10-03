package handlers

import (
	"errors"
	"net/http"
	"testing"

	"github.com/forgemill/forgemill/internal/service"
)

func TestStartDeployErrorResponseSeparatesCallerMistakesFromServerFaults(t *testing.T) {
	validation := validateViaService()
	if msg, status := startDeployErrorResponse(validation); status != http.StatusBadRequest || msg != validation.Error() {
		t.Errorf("validation error should be 400 with its own message, got %d %q", status, msg)
	}
	if msg, status := startDeployErrorResponse(errors.New("template not found: sql: no rows")); status != http.StatusInternalServerError || msg != "failed to start deployment" {
		t.Errorf("other errors must stay a generic 500, got %d %q", status, msg)
	}
}

// validateViaService obtains a real validation error through the exported
// sentinel so the test doesn't depend on service internals.
func validateViaService() error {
	svc := &service.DeployService{}
	_, err := svc.Start(&service.DeployRequest{VMName: "x", TemplateID: 1, TargetID: 1}, 1)
	return err
}
