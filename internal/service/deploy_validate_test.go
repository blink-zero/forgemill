package service

import (
	"errors"
	"testing"
)

func TestValidateDeployRequestReturnsSentinelWithExactMessage(t *testing.T) {
	cases := []struct {
		name string
		req  DeployRequest
		want string
	}{
		{"missing cpu", DeployRequest{VMName: "web-01", TemplateID: 1, TargetID: 1, MemoryMB: 2048}, "CPU must be between 1 and 128"},
		{"missing memory", DeployRequest{VMName: "web-01", TemplateID: 1, TargetID: 1, CPU: 2}, "memory must be between 256MB and 1TB"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateDeployRequest(&tc.req)
			if err == nil {
				t.Fatal("expected a validation error")
			}
			if !errors.Is(err, ErrInvalidDeployRequest) {
				t.Errorf("validation errors must match ErrInvalidDeployRequest, got %T", err)
			}
			// Preflight shows Error() verbatim as a blocker — no prefix allowed.
			if err.Error() != tc.want {
				t.Errorf("message changed: got %q want %q", err.Error(), tc.want)
			}
		})
	}
}
