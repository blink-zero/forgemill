package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/crypto"
	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/service"
)

// addressHarness mounts the VM and execution handlers behind real auth so
// the address endpoint and the executor's refusal mapping are exercised end
// to end over HTTP.
type addressHarness struct {
	t      *testing.T
	db     *db.DB
	audit  *service.AuditService
	router http.Handler
	token  string
	vmID   int64
}

func newAddressHarness(t *testing.T) *addressHarness {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "addr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	enc, err := crypto.NewEncryptor(exportTestKey)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := middleware.NewAuthMiddleware(database, exportTestJWT, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	admin := &models.User{Username: "alice", Role: "admin", IsActive: true, PasswordHash: "x"}
	if err := database.CreateUser(admin); err != nil {
		t.Fatal(err)
	}
	encPW, _ := enc.Encrypt("target-secret")
	target := &models.Target{Name: "pve", Type: "proxmox", Hostname: "pve.example.com", Port: 8006, Username: "root@pam", PasswordEncrypt: encPW}
	if err := database.CreateTarget(target); err != nil {
		t.Fatal(err)
	}
	// An adopted VM that is on but whose hypervisor reports no address.
	vm := &models.ManagedVM{TargetID: target.ID, VMName: "adopted-01", VMRef: "101", PowerState: "poweredOn", Origin: "adopted"}
	if err := database.CreateManagedVM(vm); err != nil {
		t.Fatal(err)
	}
	audit := service.NewAuditService(database)
	t.Cleanup(audit.Stop)
	vmSvc := service.NewVMService(database, nil, enc)
	vmH := NewVMHandler(vmSvc, audit)
	execH := NewExecutionHandler(service.NewExecutorService(database, nil, enc, nil), audit)

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(auth.Authenticate)
		r.Put("/vms/{id}/address", vmH.SetAddress)
		r.Post("/vms/{id}/execute", execH.Execute)
	})
	tok, err := auth.GenerateToken(admin)
	if err != nil {
		t.Fatal(err)
	}
	return &addressHarness{t: t, db: database, audit: audit, router: r, token: tok, vmID: vm.ID}
}

func (x *addressHarness) do(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+x.token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	x.router.ServeHTTP(rec, req)
	return rec
}

func TestSetAddressValidatesStoresAndAudits(t *testing.T) {
	x := newAddressHarness(t)
	id := int(x.vmID)

	if rec := x.do("PUT", "/vms/"+itoa(id)+"/address", `{"address":"ssh://10.0.0.5"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad address: %d %s", rec.Code, rec.Body)
	}
	if rec := x.do("PUT", "/vms/999/address", `{"address":"10.0.0.5"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("missing vm: %d %s", rec.Code, rec.Body)
	}

	rec := x.do("PUT", "/vms/"+itoa(id)+"/address", `{"address":" 10.0.0.5 "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set: %d %s", rec.Code, rec.Body)
	}
	var vm models.ManagedVM
	if err := json.Unmarshal(rec.Body.Bytes(), &vm); err != nil {
		t.Fatal(err)
	}
	if vm.AddressOverride != "10.0.0.5" || vm.IPAddress != "" {
		t.Fatalf("response: %+v", vm)
	}

	if rec := x.do("PUT", "/vms/"+itoa(id)+"/address", `{"address":""}`); rec.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", rec.Code, rec.Body)
	}
	got, _ := x.db.GetManagedVM(x.vmID)
	if got.AddressOverride != "" {
		t.Fatalf("not cleared: %+v", got)
	}

	// Both the set and the clear are audited with the actor.
	deadline := time.Now().Add(2 * time.Second)
	for {
		page, err := x.db.ListAuditLogs(db.AuditLogFilter{Action: "vm.address.set", PageSize: 10})
		if err != nil {
			t.Fatal(err)
		}
		logs := page.Logs
		if len(logs) == 2 {
			if logs[0].Actor != "alice" {
				t.Fatalf("actor: %+v", logs[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("want 2 audit rows, got %d", len(logs))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestExecuteReportsWhyItRefused(t *testing.T) {
	x := newAddressHarness(t)
	path := "/vms/" + itoa(int(x.vmID)) + "/execute"

	rec := x.do("POST", path, `{"script":"#!/bin/bash\necho hi"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no address: %d %s", rec.Code, rec.Body)
	}
	var body struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "no_address" || !strings.Contains(body.Error, "guest agent") || !strings.Contains(body.Error, "manually") {
		t.Fatalf("body: %+v", body)
	}

	// Once an address is set the next obstacle is reported instead, with
	// no code: the login is missing.
	if rec := x.do("PUT", "/vms/"+itoa(int(x.vmID))+"/address", `{"address":"192.0.2.10"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	rec = x.do("POST", path, `{"script":"#!/bin/bash\necho hi"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no creds: %d %s", rec.Code, rec.Body)
	}
	body = struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}{}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Code != "" || !strings.Contains(body.Error, "no SSH credentials") {
		t.Fatalf("body: %+v", body)
	}
}

func itoa(i int) string { return strconv.Itoa(i) }
