package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
)

// Credential export: every managed VM's SSH login, as a file 1Password
// imports natively (1PUX) or as a generic CSV. The handler owns the gate
// (admin, interactive session, password re-entry, synchronous audit); this
// service owns what goes in the file. See docs/design/credential-export.md.

const (
	ExportFormat1PUX = "1pux"
	ExportFormatCSV  = "csv"
)

// maxExportVMs bounds one export. A fleet larger than this exports per
// target; the cap keeps the in-memory archive small and the audit entry
// meaningful. A variable so tests can lower it.
var maxExportVMs = 5000

var (
	ErrExportTooLarge  = errors.New("export scope exceeds the per-request limit; narrow it to a target")
	ErrExportEmpty     = errors.New("no VM in the selected scope has a stored login")
	ErrExportBadFormat = errors.New("unknown export format")
)

// ExportScope selects which VMs an export or preview covers. Zero value
// means all VMs; TargetID or VMIDs narrow it (TargetID wins if both set).
type ExportScope struct {
	TargetID int64
	VMIDs    []int64
}

// ExportOptions are the opt-ins. Sudo passwords and private keys are off
// by default; deployment-inherited logins are on, because for a VM that
// was never given an explicit credential they are the login.
type ExportOptions struct {
	Format                  string
	IncludeSudoPasswords    bool
	IncludePrivateKeys      bool
	IncludeDeploymentLogins bool
}

// ExportPreview is what the Settings tab shows before asking for a
// password. Counts only; nothing is decrypted to produce it.
type ExportPreview struct {
	TotalVMs        int                   `json:"total_vms"`
	Exportable      int                   `json:"exportable"`
	PasswordLogins  int                   `json:"password_logins"`
	KeyLogins       int                   `json:"key_logins"`
	WithSudo        int                   `json:"with_sudo_password"`
	FromDeployment  int                   `json:"from_deployment"`
	SkippedNoLogin  int                   `json:"skipped_no_login"`
	ByTarget        []ExportPreviewTarget `json:"by_target"`
	PerRequestLimit int                   `json:"per_request_limit"`
}

type ExportPreviewTarget struct {
	TargetID   int64  `json:"target_id"`
	Name       string `json:"name"`
	Exportable int    `json:"exportable"`
}

// ExportResult describes what was written.
type ExportResult struct {
	Format      string
	Filename    string
	ContentType string
	Exported    int
	// Skipped counts VMs in scope with no login to export, plus (CSV only)
	// key logins, which CSV cannot carry.
	Skipped         int
	SkippedKeyLogin int
	// VMIDs lists what was exported, for the audit entry.
	VMIDs []int64
}

// CredentialExportService builds the export. It reads logins through the
// same resolution the executor uses (explicit credential first, then the
// deployment's), so what is exported is exactly what Forgemill would log in
// with.
type CredentialExportService struct {
	db      *db.DB
	enc     Encryptor
	version string
	now     func() time.Time
}

func NewCredentialExportService(database *db.DB, enc Encryptor, version string) *CredentialExportService {
	return &CredentialExportService{db: database, enc: enc, version: version, now: time.Now}
}

// vmsInScope lists managed VMs in scope with their targets, in a stable
// order.
func (s *CredentialExportService) vmsInScope(scope ExportScope) ([]models.ManagedVM, map[int64]models.Target, error) {
	all, err := s.db.ListManagedVMs()
	if err != nil {
		return nil, nil, fmt.Errorf("list VMs: %w", err)
	}
	targets, err := s.db.ListTargets()
	if err != nil {
		return nil, nil, fmt.Errorf("list targets: %w", err)
	}
	names := make(map[int64]models.Target, len(targets))
	for _, t := range targets {
		names[t.ID] = t
	}
	var want map[int64]bool
	if scope.TargetID == 0 && len(scope.VMIDs) > 0 {
		want = make(map[int64]bool, len(scope.VMIDs))
		for _, id := range scope.VMIDs {
			want[id] = true
		}
	}
	out := all[:0:0]
	for _, vm := range all {
		if scope.TargetID != 0 && vm.TargetID != scope.TargetID {
			continue
		}
		if want != nil && !want[vm.ID] {
			continue
		}
		out = append(out, vm)
	}
	return out, names, nil
}

// Preview counts what an export with these options would contain.
func (s *CredentialExportService) Preview(scope ExportScope, opts ExportOptions) (*ExportPreview, error) {
	vms, names, err := s.vmsInScope(scope)
	if err != nil {
		return nil, err
	}
	meta, err := s.db.ListVMCredentialMeta()
	if err != nil {
		return nil, fmt.Errorf("list credential metadata: %w", err)
	}
	deps, err := s.db.ListDeploymentsWithInitialCredentials()
	if err != nil {
		return nil, fmt.Errorf("list deployment credentials: %w", err)
	}
	p := &ExportPreview{TotalVMs: len(vms), PerRequestLimit: maxExportVMs}
	perTarget := map[int64]int{}
	for _, vm := range vms {
		if m, ok := meta[vm.ID]; ok {
			p.Exportable++
			perTarget[vm.TargetID]++
			if m.Kind == models.CredentialKindPrivateKey {
				p.KeyLogins++
			} else {
				p.PasswordLogins++
			}
			if m.HasSudoPassword {
				p.WithSudo++
			}
			continue
		}
		if opts.IncludeDeploymentLogins && vm.DeploymentID != nil && deps[*vm.DeploymentID] {
			p.Exportable++
			perTarget[vm.TargetID]++
			p.PasswordLogins++
			p.FromDeployment++
			continue
		}
		p.SkippedNoLogin++
	}
	p.ByTarget = []ExportPreviewTarget{}
	for id, n := range perTarget {
		p.ByTarget = append(p.ByTarget, ExportPreviewTarget{TargetID: id, Name: names[id].Name, Exportable: n})
	}
	sort.Slice(p.ByTarget, func(i, j int) bool { return p.ByTarget[i].Name < p.ByTarget[j].Name })
	return p, nil
}

// exportItem is one VM's login, resolved. It lives only for the duration
// of the write.
type exportItem struct {
	vm     models.ManagedVM
	target string
	// hypervisor is the target type (vcenter, esxi, proxmox); vm.Platform
	// is the guest OS family and goes in the notes instead.
	hypervisor string
	username   string
	password   string
	key        string
	sudo       string
	kind       string
	source     string
	setAt      *time.Time
	setBy      *int64
}

// Export writes the file to w and reports what it contained. It fails
// before writing anything if the scope is too large or empty, so the caller
// can still answer with a proper status; w is expected to be a buffer the
// handler sends only after the audit entry is on disk.
func (s *CredentialExportService) Export(ctx context.Context, scope ExportScope, opts ExportOptions, actor string, w io.Writer) (*ExportResult, error) {
	if opts.Format != ExportFormat1PUX && opts.Format != ExportFormatCSV {
		return nil, ErrExportBadFormat
	}
	if s.enc == nil {
		return nil, fmt.Errorf("encryption not available")
	}
	vms, names, err := s.vmsInScope(scope)
	if err != nil {
		return nil, err
	}
	if len(vms) > maxExportVMs {
		return nil, ErrExportTooLarge
	}
	res := &ExportResult{Format: opts.Format}
	var items []exportItem
	for i := range vms {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		vm := vms[i]
		r, err := resolveVMCredentials(s.db, s.enc, &vm)
		if errors.Is(err, ErrNoCredentials) {
			res.Skipped++
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("vm %d (%s): %w", vm.ID, vm.VMName, err)
		}
		if r.Source == CredentialSourceDeployment && !opts.IncludeDeploymentLogins {
			res.Skipped++
			continue
		}
		if r.Kind == models.CredentialKindPrivateKey && opts.Format == ExportFormatCSV {
			res.Skipped++
			res.SkippedKeyLogin++
			continue
		}
		it := exportItem{vm: vm, target: names[vm.TargetID].Name, hypervisor: names[vm.TargetID].Type, username: r.Username, kind: r.Kind, source: r.Source, setAt: r.SetAt}
		if r.Kind == models.CredentialKindPrivateKey {
			if opts.IncludePrivateKeys {
				it.key = r.PrivateKey
			}
		} else {
			it.password = r.Password
		}
		if opts.IncludeSudoPasswords && r.HasSudoPassword {
			it.sudo = r.SudoPassword
		}
		items = append(items, it)
		res.VMIDs = append(res.VMIDs, vm.ID)
	}
	if len(items) == 0 {
		return nil, ErrExportEmpty
	}
	res.Exported = len(items)
	stamp := s.now().UTC()
	switch opts.Format {
	case ExportFormat1PUX:
		res.ContentType = "application/zip"
		res.Filename = "forgemill-credentials-" + stamp.Format("20060102T1504Z") + ".1pux"
		err = s.write1PUX(w, items, actor, stamp)
	case ExportFormatCSV:
		res.ContentType = "text/csv; charset=utf-8"
		res.Filename = "forgemill-credentials-" + stamp.Format("20060102T1504Z") + ".csv"
		err = s.writeCSV(w, items, actor, stamp)
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

// --- 1PUX ------------------------------------------------------------------
//
// Layout per https://support.1password.com/1pux-format/: a zip holding
// export.attributes, export.data and a files/ directory. One vault, one
// Login item (category 001) per VM.

type puxAttributes struct {
	Version     int    `json:"version"`
	Description string `json:"description"`
	CreatedAt   int64  `json:"createdAt"`
}

type puxField struct {
	Value       string `json:"value"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Designation string `json:"designation"`
}

type puxSectionField struct {
	Title string         `json:"title"`
	ID    string         `json:"id"`
	Value map[string]any `json:"value"`
}

type puxSection struct {
	Title  string            `json:"title"`
	Name   string            `json:"name"`
	Fields []puxSectionField `json:"fields"`
}

type puxItem struct {
	UUID         string `json:"uuid"`
	FavIndex     int    `json:"favIndex"`
	CreatedAt    int64  `json:"createdAt"`
	UpdatedAt    int64  `json:"updatedAt"`
	State        string `json:"state"`
	CategoryUUID string `json:"categoryUuid"`
	Details      struct {
		LoginFields     []puxField   `json:"loginFields"`
		NotesPlain      string       `json:"notesPlain"`
		Sections        []puxSection `json:"sections"`
		PasswordHistory []any        `json:"passwordHistory"`
	} `json:"details"`
	Overview struct {
		Title    string   `json:"title"`
		Subtitle string   `json:"subtitle"`
		URL      string   `json:"url"`
		URLs     []puxURL `json:"urls"`
		Tags     []string `json:"tags"`
	} `json:"overview"`
}

type puxURL struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

func (s *CredentialExportService) write1PUX(w io.Writer, items []exportItem, actor string, stamp time.Time) error {
	zw := zip.NewWriter(w)
	attrs, err := zw.Create("export.attributes")
	if err != nil {
		return err
	}
	if err := json.NewEncoder(attrs).Encode(puxAttributes{Version: 3, Description: "Forgemill credential export", CreatedAt: stamp.Unix()}); err != nil {
		return err
	}
	data, err := zw.Create("export.data")
	if err != nil {
		return err
	}
	vaultUUID, err := puxUUID()
	if err != nil {
		return err
	}
	accountUUID, err := puxUUID()
	if err != nil {
		return err
	}
	// The document is one JSON object; items are encoded one at a time so
	// no slice of every plaintext is ever built.
	head := fmt.Sprintf(`{"accounts":[{"attrs":{"accountName":"Forgemill","name":"Forgemill","avatar":"","email":"","uuid":%q,"domain":""},"vaults":[{"attrs":{"uuid":%q,"desc":"Exported from Forgemill %s","avatar":"","name":"Forgemill","type":"U"},"items":[`, accountUUID, vaultUUID, s.version)
	if _, err := io.WriteString(data, head); err != nil {
		return err
	}
	for i, it := range items {
		item, err := s.puxItemFor(it, actor, stamp)
		if err != nil {
			return err
		}
		b, err := json.Marshal(item)
		if err != nil {
			return err
		}
		if i > 0 {
			if _, err := data.Write([]byte(",")); err != nil {
				return err
			}
		}
		if _, err := data.Write(b); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(data, "]}]}]}"); err != nil {
		return err
	}
	if _, err := zw.Create("files/"); err != nil {
		return err
	}
	return zw.Close()
}

func (s *CredentialExportService) puxItemFor(it exportItem, actor string, stamp time.Time) (*puxItem, error) {
	id, err := puxUUID()
	if err != nil {
		return nil, err
	}
	item := &puxItem{UUID: id, State: "active", CategoryUUID: "001", CreatedAt: stamp.Unix(), UpdatedAt: stamp.Unix()}
	if it.setAt != nil {
		item.CreatedAt = it.setAt.Unix()
		item.UpdatedAt = it.setAt.Unix()
	}
	item.Overview.Title = exportTitle(it)
	item.Overview.Tags = []string{"forgemill"}
	if it.target != "" {
		item.Overview.Tags = append(item.Overview.Tags, it.target)
	}
	if it.hypervisor != "" {
		item.Overview.Tags = append(item.Overview.Tags, it.hypervisor)
	}
	if it.vm.IPAddress != "" {
		u := "ssh://" + it.vm.IPAddress
		item.Overview.URL = u
		item.Overview.URLs = []puxURL{{Label: "", URL: u}}
	} else {
		item.Overview.URLs = []puxURL{}
	}
	item.Details.LoginFields = []puxField{{Value: it.username, Name: "username", Type: "T", Designation: "username"}}
	if it.kind != models.CredentialKindPrivateKey {
		item.Details.LoginFields = append(item.Details.LoginFields, puxField{Value: it.password, Name: "password", Type: "P", Designation: "password"})
	}
	item.Details.NotesPlain = exportNotes(it, s.version, actor, stamp)
	item.Details.PasswordHistory = []any{}

	fields := []puxSectionField{}
	if it.sudo != "" {
		fields = append(fields, puxSectionField{Title: "Sudo password", ID: "sudo_password", Value: map[string]any{"concealed": it.sudo}})
	}
	if it.key != "" {
		fields = append(fields, puxSectionField{Title: "Private key", ID: "private_key", Value: map[string]any{"concealed": it.key}})
	}
	if it.vm.HostKeyFP != "" {
		fields = append(fields, puxSectionField{Title: "Host key fingerprint", ID: "host_key_fp", Value: map[string]any{"string": it.vm.HostKeyFP}})
	}
	fields = append(fields,
		puxSectionField{Title: "VM ref", ID: "vm_ref", Value: map[string]any{"string": it.vm.VMRef}},
		puxSectionField{Title: "Login source", ID: "source", Value: map[string]any{"string": it.source}},
		puxSectionField{Title: "Login kind", ID: "kind", Value: map[string]any{"string": it.kind}},
	)
	if it.setAt != nil {
		fields = append(fields, puxSectionField{Title: "Set at", ID: "set_at", Value: map[string]any{"string": it.setAt.UTC().Format(time.RFC3339)}})
	}
	item.Details.Sections = []puxSection{{Title: "Forgemill", Name: "forgemill", Fields: fields}}
	return item, nil
}

// puxUUID returns a 26-character id from the alphabet 1Password uses for
// its own; uniqueness only matters within the file.
func puxUUID() (string, error) {
	const alphabet = "23456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, 26)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b), nil
}

func exportTitle(it exportItem) string {
	if it.target != "" {
		return it.vm.VMName + " (" + it.target + ")"
	}
	return it.vm.VMName
}

func exportNotes(it exportItem, version, actor string, stamp time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Exported from Forgemill %s on %s by %s.\n", version, stamp.Format(time.RFC3339), actor)
	fmt.Fprintf(&b, "VM id %d", it.vm.ID)
	if it.vm.VMRef != "" {
		fmt.Fprintf(&b, ", ref %s", it.vm.VMRef)
	}
	if it.hypervisor != "" {
		fmt.Fprintf(&b, ", hypervisor %s", it.hypervisor)
	}
	if it.vm.OSType != "" {
		fmt.Fprintf(&b, ", OS %s", it.vm.OSType)
	} else if it.vm.Platform != "" {
		fmt.Fprintf(&b, ", OS family %s", it.vm.Platform)
	}
	fmt.Fprintf(&b, ". Login source: %s.", it.source)
	if it.kind == models.CredentialKindPrivateKey && it.key == "" {
		b.WriteString(" Key login; the private key was not included in this export.")
	}
	return b.String()
}

// --- CSV -------------------------------------------------------------------
//
// 1Password's CSV importer is positional and header-blind, so the column
// order is fixed: title, website, username, password, notes.

func (s *CredentialExportService) writeCSV(w io.Writer, items []exportItem, actor string, stamp time.Time) error {
	cw := csv.NewWriter(w)
	cw.UseCRLF = true
	if err := cw.Write([]string{"title", "website", "username", "password", "notes"}); err != nil {
		return err
	}
	for _, it := range items {
		website := ""
		if it.vm.IPAddress != "" {
			website = "ssh://" + it.vm.IPAddress
		}
		notes := exportNotes(it, s.version, actor, stamp)
		if it.sudo != "" {
			notes += "\nsudo password: " + it.sudo
		}
		if it.vm.HostKeyFP != "" {
			notes += "\nhost key fingerprint: " + it.vm.HostKeyFP
		}
		// Titles and notes are defused against spreadsheet formula
		// injection; the password column is never altered, because a
		// mangled password is worse than a formula warning in a tool this
		// file should not be opened in anyway.
		row := []string{csvDefuse(exportTitle(it)), website, csvDefuse(it.username), it.password, csvDefuse(notes)}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// csvDefuse prefixes a cell that a spreadsheet would evaluate as a formula.
func csvDefuse(cell string) string {
	if cell == "" {
		return cell
	}
	switch cell[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + cell
	}
	return cell
}

// ExportToBuffer is Export into a fresh buffer; the handler sends it only
// after the audit entry is persisted.
func (s *CredentialExportService) ExportToBuffer(ctx context.Context, scope ExportScope, opts ExportOptions, actor string) (*bytes.Buffer, *ExportResult, error) {
	var buf bytes.Buffer
	res, err := s.Export(ctx, scope, opts, actor, &buf)
	if err != nil {
		return nil, nil, err
	}
	return &buf, res, nil
}
