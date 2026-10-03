package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/forgemill/forgemill/internal/provider"
	"github.com/forgemill/forgemill/internal/service"
)

func TestAddDiskErrorResponseMapsSentinels(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		logIt  bool
	}{
		{"vm not found", fmt.Errorf("%w: id 7", service.ErrVMNotFound), http.StatusNotFound, false},
		{"unsupported", fmt.Errorf("%w: adding a disk is not available", provider.ErrNotSupported), http.StatusBadRequest, false},
		{"datastore", fmt.Errorf("%w: %q", provider.ErrDatastoreNotFound, "nope"), http.StatusBadRequest, false},
		{"inaccessible", fmt.Errorf("%w: %q is not mounted on host esx1", provider.ErrDatastoreNotAccessible, "2TB_G7_02"), http.StatusBadRequest, false},
		{"bad spec", fmt.Errorf("%w: size_gb must be between 1 and 65536", service.ErrInvalidDiskSpec), http.StatusBadRequest, false},
		{"anything else", errors.New("vSphere exploded"), http.StatusInternalServerError, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, msg, logIt := addDiskErrorResponse(tc.err)
			if status != tc.status || logIt != tc.logIt {
				t.Errorf("got %d/%v want %d/%v", status, logIt, tc.status, tc.logIt)
			}
			if !tc.logIt && msg != tc.err.Error() && status != http.StatusNotFound {
				t.Errorf("client should see the specific message, got %q", msg)
			}
		})
	}
}
