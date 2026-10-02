package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/forgemill/forgemill/internal/provider"
	"github.com/forgemill/forgemill/internal/service"
)

func TestAddNICErrorResponseMapsSentinels(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantLog    bool
	}{
		{"vm not found", fmt.Errorf("%w: id 9", service.ErrVMNotFound), http.StatusNotFound, false},
		{"unsupported provider", fmt.Errorf("%w: proxmox", provider.ErrNotSupported), http.StatusBadRequest, false},
		{"network not found", fmt.Errorf("%w: %q", provider.ErrNetworkNotFound, "nope"), http.StatusBadRequest, false},
		{"bad adapter", fmt.Errorf("%w: virtio", provider.ErrInvalidAdapterType), http.StatusBadRequest, false},
		{"bad spec", fmt.Errorf("%w: network is required", service.ErrInvalidNICSpec), http.StatusBadRequest, false},
		{"anything else", errors.New("vSphere exploded"), http.StatusInternalServerError, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, msg, logIt := addNICErrorResponse(tc.err)
			if status != tc.wantStatus {
				t.Errorf("status = %d, want %d", status, tc.wantStatus)
			}
			if logIt != tc.wantLog {
				t.Errorf("logIt = %v, want %v", logIt, tc.wantLog)
			}
			if msg == "" {
				t.Error("message must not be empty")
			}
			// Internal detail must not leak on the generic path.
			if tc.wantLog && strings.Contains(msg, "exploded") {
				t.Errorf("generic 500 message leaked the underlying error: %q", msg)
			}
		})
	}
}

// addNICRequestTo runs the handler with a nil service: every case here must
// be rejected before the service is consulted, or the test panics — which
// is the point.
func addNICRequestTo(t *testing.T, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	h := &VMHandler{}
	req := httptest.NewRequest(http.MethodPost, "/vms/"+id+"/nics", strings.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(contextWithRoute(req, rctx))
	rec := httptest.NewRecorder()
	h.AddNIC(rec, req)
	return rec
}

func TestAddNICHandlerRejectsBadInputBeforeServiceCall(t *testing.T) {
	cases := []struct {
		name string
		id   string
		body string
	}{
		{"non-numeric id", "abc", `{"network":"VM Network"}`},
		{"malformed json", "1", `{"network":`},
		{"missing network", "1", `{"adapter_type":"vmxnet3"}`},
		{"blank network", "1", `{"network":"   "}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := addNICRequestTo(t, tc.id, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func contextWithRoute(req *http.Request, rctx *chi.Context) context.Context {
	return context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
}
