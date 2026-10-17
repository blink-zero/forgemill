package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/forgemill/forgemill/internal/crypto"
	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
)

// exportFixture is a small estate: two targets, five VMs.
//
//	web-01  explicit password login + separate sudo password
//	web-02  explicit key login
//	web-03  deployment-inherited password login
//	db-01   explicit password login on the second target, no IP
//	bare    nothing stored
type exportFixture struct {
	svc      *CredentialExportService
	db       *db.DB
	targetA  int64
	targetB  int64
	web1     int64
	web2     int64
	web3     int64
	db1      int64
	bare     int64
	keyPEM   string
	password string
}

func newExportFixture(t *testing.T) *exportFixture {
	t.Helper()
	database := newTestDB(t)
	enc, err := crypto.NewEncryptor(nicTestKey)
	if err != nil {
		t.Fatal(err)
	}
	encPW, _ := enc.Encrypt("target-secret")
	ta := &models.Target{Name: "pve-lab", Type: "proxmox", Hostname: "pve.example.com", Port: 8006, Username: "root@pam", PasswordEncrypt: encPW}
	tb := &models.Target{Name: "esxi-dc", Type: "esxi", Hostname: "esxi.example.com", Port: 443, Username: "root", PasswordEncrypt: encPW}
	for _, tg := range []*models.Target{ta, tb} {
		if err := database.CreateTarget(tg); err != nil {
			t.Fatal(err)
		}
	}
	admin := &models.User{Username: "alice", Role: "admin", IsActive: true, PasswordHash: "x"}
	if err := database.CreateUser(admin); err != nil {
		t.Fatal(err)
	}
	mk := func(target int64, name, ref, ip string, dep *int64) int64 {
		vm := &models.ManagedVM{TargetID: target, VMName: name, VMRef: ref, PowerState: "poweredOn", IPAddress: ip, DeploymentID: dep}
		if err := database.CreateManagedVM(vm); err != nil {
			t.Fatal(err)
		}
		return vm.ID
	}
	f := &exportFixture{svc: NewCredentialExportService(database, enc, "test"), db: database, targetA: ta.ID, targetB: tb.ID, keyPEM: testPrivateKeyPEM(t), password: "p@ss,word\"with\nnewline"}
	f.svc.now = func() time.Time { return time.Date(2026, 10, 17, 10, 30, 0, 0, time.UTC) }
	// Credentials are written straight to the store: SetCredentials would
	// try to SSH to the VM first.
	setCred := func(vmID int64, username, kind, secret, sudo string) {
		encSecret, err := enc.Encrypt(secret)
		if err != nil {
			t.Fatal(err)
		}
		c := &models.VMCredential{VMID: vmID, Username: username, Kind: kind, SecretEnc: encSecret, SetBy: &admin.ID}
		if sudo != "" {
			if c.SudoPasswordEnc, err = enc.Encrypt(sudo); err != nil {
				t.Fatal(err)
			}
		}
		if err := database.SetVMCredential(c); err != nil {
			t.Fatal(err)
		}
	}

	f.web1 = mk(ta.ID, "web-01", "101", "10.0.0.11", nil)
	setCred(f.web1, "root", models.CredentialKindPassword, f.password, "sudo-secret")
	f.web2 = mk(ta.ID, "web-02", "102", "10.0.0.12", nil)
	setCred(f.web2, "ops", models.CredentialKindPrivateKey, f.keyPEM, "")
	depPW, _ := enc.Encrypt("deploy-pass")
	dep := &models.Deployment{TargetID: ta.ID, VMName: "web-03", Status: "completed", ConfigJSON: "{}", CreatedBy: admin.ID, InitialUsername: "forgemill", InitialPwdEnc: depPW}
	if err := database.CreateDeployment(dep); err != nil {
		t.Fatal(err)
	}
	f.web3 = mk(ta.ID, "web-03", "103", "10.0.0.13", &dep.ID)
	f.db1 = mk(tb.ID, "db-01", "vm-200", "", nil)
	setCred(f.db1, "=admin", models.CredentialKindPassword, "=1+1", "")
	f.bare = mk(tb.ID, "bare", "vm-201", "10.0.0.21", nil)
	return f
}

func TestCredentialExportPreviewCountsWithoutDecrypting(t *testing.T) {
	f := newExportFixture(t)
	p, err := f.svc.Preview(ExportScope{}, ExportOptions{IncludeDeploymentLogins: true})
	if err != nil {
		t.Fatal(err)
	}
	if p.TotalVMs != 5 || p.Exportable != 4 || p.PasswordLogins != 3 || p.KeyLogins != 1 || p.WithSudo != 1 || p.FromDeployment != 1 || p.SkippedNoLogin != 1 {
		t.Fatalf("preview: %+v", p)
	}
	if len(p.ByTarget) != 2 || p.ByTarget[0].Name != "esxi-dc" || p.ByTarget[0].Exportable != 1 || p.ByTarget[1].Name != "pve-lab" || p.ByTarget[1].Exportable != 3 {
		t.Fatalf("by target: %+v", p.ByTarget)
	}

	// Without deployment logins web-03 is counted as skipped.
	p, _ = f.svc.Preview(ExportScope{}, ExportOptions{})
	if p.Exportable != 3 || p.FromDeployment != 0 || p.SkippedNoLogin != 2 {
		t.Fatalf("preview without deployment logins: %+v", p)
	}
	// Scope by target and by id.
	p, _ = f.svc.Preview(ExportScope{TargetID: f.targetB}, ExportOptions{IncludeDeploymentLogins: true})
	if p.TotalVMs != 2 || p.Exportable != 1 {
		t.Fatalf("target scope: %+v", p)
	}
	p, _ = f.svc.Preview(ExportScope{VMIDs: []int64{f.web1, f.bare, 9999}}, ExportOptions{IncludeDeploymentLogins: true})
	if p.TotalVMs != 2 || p.Exportable != 1 || p.SkippedNoLogin != 1 {
		t.Fatalf("id scope: %+v", p)
	}
}

// read1PUX unzips the export and returns the parsed export.data plus the
// list of zip entries.
func read1PUX(t *testing.T, b []byte) (map[string]any, []string) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	var names []string
	var data map[string]any
	for _, zf := range zr.File {
		names = append(names, zf.Name)
		if zf.Name == "export.data" {
			rc, _ := zf.Open()
			raw, _ := io.ReadAll(rc)
			rc.Close()
			if err := json.Unmarshal(raw, &data); err != nil {
				t.Fatalf("export.data is not JSON: %v\n%s", err, raw)
			}
		}
		if zf.Name == "export.attributes" {
			rc, _ := zf.Open()
			var attrs map[string]any
			if err := json.NewDecoder(rc).Decode(&attrs); err != nil || attrs["version"] != float64(3) {
				t.Fatalf("export.attributes: %v %v", attrs, err)
			}
			rc.Close()
		}
	}
	return data, names
}

func items1PUX(t *testing.T, data map[string]any) []map[string]any {
	t.Helper()
	accounts := data["accounts"].([]any)
	vaults := accounts[0].(map[string]any)["vaults"].([]any)
	raw := vaults[0].(map[string]any)["items"].([]any)
	out := make([]map[string]any, len(raw))
	for i, r := range raw {
		out[i] = r.(map[string]any)
	}
	return out
}

func loginField(item map[string]any, designation string) (string, bool) {
	for _, f := range item["details"].(map[string]any)["loginFields"].([]any) {
		m := f.(map[string]any)
		if m["designation"] == designation {
			return m["value"].(string), true
		}
	}
	return "", false
}

func sectionField(item map[string]any, id string) (map[string]any, bool) {
	for _, s := range item["details"].(map[string]any)["sections"].([]any) {
		for _, f := range s.(map[string]any)["fields"].([]any) {
			m := f.(map[string]any)
			if m["id"] == id {
				return m["value"].(map[string]any), true
			}
		}
	}
	return nil, false
}

func TestCredentialExport1PUXDefaultsLeaveSecretsOutUnlessAsked(t *testing.T) {
	f := newExportFixture(t)
	var buf bytes.Buffer
	res, err := f.svc.Export(context.Background(), ExportScope{}, ExportOptions{Format: ExportFormat1PUX, IncludeDeploymentLogins: true}, "alice", &buf)
	if err != nil {
		t.Fatal(err)
	}
	if res.Exported != 4 || res.Skipped != 1 || res.ContentType != "application/zip" || res.Filename != "forgemill-credentials-20261017T1030Z.1pux" || len(res.VMIDs) != 4 {
		t.Fatalf("result: %+v", res)
	}
	data, names := read1PUX(t, buf.Bytes())
	if strings.Join(names, ",") != "export.attributes,export.data,files/" {
		t.Fatalf("zip entries: %v", names)
	}
	items := items1PUX(t, data)
	if len(items) != 4 {
		t.Fatalf("want 4 items, got %d", len(items))
	}
	byTitle := map[string]map[string]any{}
	for _, it := range items {
		byTitle[it["overview"].(map[string]any)["title"].(string)] = it
		if it["categoryUuid"] != "001" || it["state"] != "active" || len(it["uuid"].(string)) != 26 {
			t.Errorf("item shape: %v", it)
		}
	}
	web1 := byTitle["web-01 (pve-lab)"]
	if u, _ := loginField(web1, "username"); u != "root" {
		t.Errorf("web-01 username %q", u)
	}
	if p, _ := loginField(web1, "password"); p != f.password {
		t.Errorf("web-01 password %q", p)
	}
	if web1["overview"].(map[string]any)["url"] != "ssh://10.0.0.11" {
		t.Errorf("web-01 url %v", web1["overview"])
	}
	if tags := web1["overview"].(map[string]any)["tags"].([]any); len(tags) != 3 || tags[0] != "forgemill" || tags[1] != "pve-lab" || tags[2] != "proxmox" {
		t.Errorf("web-01 tags %v", tags)
	}
	if _, ok := sectionField(web1, "sudo_password"); ok {
		t.Error("sudo password exported without opt-in")
	}
	if v, ok := sectionField(web1, "source"); !ok || v["string"] != "vm" {
		t.Errorf("web-01 source %v", v)
	}

	web2 := byTitle["web-02 (pve-lab)"]
	if _, ok := loginField(web2, "password"); ok {
		t.Error("key login must not carry a password field")
	}
	if _, ok := sectionField(web2, "private_key"); ok {
		t.Error("private key exported without opt-in")
	}
	if !strings.Contains(web2["details"].(map[string]any)["notesPlain"].(string), "private key was not included") {
		t.Error("key login without key should say so in the notes")
	}

	web3 := byTitle["web-03 (pve-lab)"]
	if u, _ := loginField(web3, "username"); u != "forgemill" {
		t.Errorf("web-03 username %q", u)
	}
	if p, _ := loginField(web3, "password"); p != "deploy-pass" {
		t.Errorf("web-03 password %q", p)
	}
	if v, _ := sectionField(web3, "source"); v["string"] != "deployment" {
		t.Errorf("web-03 source %v", v)
	}
	if db1 := byTitle["db-01 (esxi-dc)"]; db1["overview"].(map[string]any)["url"] != "" {
		t.Errorf("db-01 has no IP, url must be empty: %v", db1["overview"])
	}
	// The raw archive must not contain what was not asked for.
	if bytes.Contains(buf.Bytes(), []byte("sudo-secret")) || bytes.Contains(buf.Bytes(), []byte("PRIVATE KEY")) {
		t.Error("opt-out secrets present in the archive")
	}
}

func TestCredentialExport1PUXOptInsAndScope(t *testing.T) {
	f := newExportFixture(t)
	var buf bytes.Buffer
	opts := ExportOptions{Format: ExportFormat1PUX, IncludeSudoPasswords: true, IncludePrivateKeys: true}
	res, err := f.svc.Export(context.Background(), ExportScope{TargetID: f.targetA}, opts, "alice", &buf)
	if err != nil {
		t.Fatal(err)
	}
	// web-03 is deployment-inherited and excluded by the options.
	if res.Exported != 2 || res.Skipped != 1 {
		t.Fatalf("result: %+v", res)
	}
	data, _ := read1PUX(t, buf.Bytes())
	for _, it := range items1PUX(t, data) {
		switch it["overview"].(map[string]any)["title"] {
		case "web-01 (pve-lab)":
			if v, ok := sectionField(it, "sudo_password"); !ok || v["concealed"] != "sudo-secret" {
				t.Errorf("sudo password missing: %v", v)
			}
		case "web-02 (pve-lab)":
			if v, ok := sectionField(it, "private_key"); !ok || v["concealed"] != f.keyPEM {
				t.Errorf("private key missing")
			}
		default:
			t.Errorf("unexpected item %v", it["overview"])
		}
	}
}

func TestCredentialExportCSVIsPositionalAndDefused(t *testing.T) {
	f := newExportFixture(t)
	var buf bytes.Buffer
	res, err := f.svc.Export(context.Background(), ExportScope{}, ExportOptions{Format: ExportFormatCSV, IncludeDeploymentLogins: true, IncludeSudoPasswords: true}, "alice", &buf)
	if err != nil {
		t.Fatal(err)
	}
	// The key login cannot be represented in CSV and is counted.
	if res.Exported != 3 || res.Skipped != 2 || res.SkippedKeyLogin != 1 || !strings.HasSuffix(res.Filename, ".csv") || res.ContentType != "text/csv; charset=utf-8" {
		t.Fatalf("result: %+v", res)
	}
	if !bytes.Contains(buf.Bytes(), []byte("\r\n")) {
		t.Error("CSV should use CRLF line endings")
	}
	rows, err := csv.NewReader(bytes.NewReader(buf.Bytes())).ReadAll()
	if err != nil {
		t.Fatalf("CSV does not parse: %v", err)
	}
	if strings.Join(rows[0], "|") != "title|website|username|password|notes" {
		t.Fatalf("header: %v", rows[0])
	}
	if len(rows) != 4 {
		t.Fatalf("want 3 data rows, got %d", len(rows)-1)
	}
	for _, row := range rows[1:] {
		if len(row) != 5 {
			t.Fatalf("row width: %v", row)
		}
		switch row[0] {
		case "web-01 (pve-lab)":
			if row[1] != "ssh://10.0.0.11" || row[2] != "root" || row[3] != f.password || !strings.Contains(row[4], "sudo password: sudo-secret") {
				t.Errorf("web-01 row: %q", row)
			}
		case "db-01 (esxi-dc)":
			// Username is defused, the password is left exactly as stored.
			if row[2] != "'=admin" || row[3] != "=1+1" || row[1] != "" {
				t.Errorf("db-01 row: %q", row)
			}
		case "web-03 (pve-lab)":
			if row[2] != "forgemill" || row[3] != "deploy-pass" {
				t.Errorf("web-03 row: %q", row)
			}
		default:
			t.Errorf("unexpected row %q", row)
		}
	}
}

func TestCredentialExportRefusesEmptyBadFormatAndTooLarge(t *testing.T) {
	f := newExportFixture(t)
	var buf bytes.Buffer
	if _, err := f.svc.Export(context.Background(), ExportScope{VMIDs: []int64{f.bare}}, ExportOptions{Format: ExportFormat1PUX, IncludeDeploymentLogins: true}, "alice", &buf); !errors.Is(err, ErrExportEmpty) || buf.Len() != 0 {
		t.Fatalf("empty scope: err %v, %d bytes written", err, buf.Len())
	}
	if _, err := f.svc.Export(context.Background(), ExportScope{}, ExportOptions{Format: "xlsx"}, "alice", &buf); !errors.Is(err, ErrExportBadFormat) {
		t.Fatalf("bad format: %v", err)
	}
	old := maxExportVMs
	maxExportVMs = 2
	t.Cleanup(func() { maxExportVMs = old })
	if _, err := f.svc.Export(context.Background(), ExportScope{}, ExportOptions{Format: ExportFormat1PUX}, "alice", &buf); !errors.Is(err, ErrExportTooLarge) || buf.Len() != 0 {
		t.Fatalf("too large: err %v, %d bytes written", err, buf.Len())
	}
	// Narrowing to a target under the cap works.
	if _, err := f.svc.Export(context.Background(), ExportScope{TargetID: f.targetB}, ExportOptions{Format: ExportFormat1PUX}, "alice", &buf); err != nil {
		t.Fatalf("narrowed scope: %v", err)
	}
}

func TestCredentialExportHonoursCancellation(t *testing.T) {
	f := newExportFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var buf bytes.Buffer
	if _, err := f.svc.Export(ctx, ExportScope{}, ExportOptions{Format: ExportFormat1PUX}, "alice", &buf); !errors.Is(err, context.Canceled) || buf.Len() != 0 {
		t.Fatalf("cancelled: err %v, %d bytes", err, buf.Len())
	}
}

func TestCSVDefuse(t *testing.T) {
	for in, want := range map[string]string{"": "", "plain": "plain", "=cmd": "'=cmd", "+1": "'+1", "-1": "'-1", "@x": "'@x", "\tx": "'\tx"} {
		if got := csvDefuse(in); got != want {
			t.Errorf("csvDefuse(%q) = %q, want %q", in, got, want)
		}
	}
}
