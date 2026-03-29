// Package proxmox implements the Proxmox VE provider.
package proxmox

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/forgemill/forgemill/internal/provider"
)

func init() {
	// Register Proxmox provider with port normalization
	provider.RegisterProvider("proxmox", func(hostname string, port int, username, password string, validateCerts bool) provider.Provider {
		// Normalize port: default Proxmox API is 8006, not 443
		if port == 443 || port == 0 {
			port = 8006
		}
		return New(hostname, port, username, password, validateCerts)
	})
	provider.RegisterMetadata("proxmox", &provider.ProviderMetadata{
		Name:        "Proxmox VE",
		Description: "Proxmox Virtual Environment — open-source virtualisation platform with KVM/QEMU and LXC support.",
		Icon:        "proxmox",
		Defaults: provider.ProviderDefaults{
			Port:                8006,
			Username:            "root@pam",
			NamePlaceholder:     "Proxmox Node 01",
			HostnamePlaceholder: "pve01.example.com",
		},
		Hints: map[string]string{
			"port":     "Default: 8006 (Proxmox API)",
			"username": "Format: user@realm (e.g. root@pam, admin@pve)",
		},
		Features: provider.ProviderFeatures{
			Folders:          false,
			Clusters:         false,
			DiskProvisioning: false,
			LinkedClones:     true,
		},
		DeployFields: []provider.DeployField{
			{Key: "datastore", Label: "Storage", Resource: "datastores"},
			{Key: "network", Label: "Bridge", Resource: "networks"},
		},
	})
}

// PV-P2: Regex for Proxmox API token format: user@realm!tokenid=uuid-secret
var apiTokenRe = regexp.MustCompile(`^.+@.+!.+=.+$`)

// Provider implements the provider.Provider interface for Proxmox VE.
// TargetHostKeyStore provides TOFU (Trust-On-First-Use) SSH host key storage for targets.
type TargetHostKeyStore interface {
	GetTargetSSHHostKeyFP(targetID int64) (string, error)
	UpdateTargetSSHHostKeyFP(targetID int64, fingerprint string) error
}

type Provider struct {
	hostname      string
	port          int
	username      string
	password      string
	node          string
	validateCerts bool

	baseURL    string
	httpClient *http.Client

	// Ticket-based auth (protected by mu)
	mu        sync.RWMutex
	ticket    string
	csrfToken string

	// PV-P2: Explicit flag for API token auth (detected by regex or explicit config)
	useAPIToken bool

	// SSH TOFU support (optional)
	targetID   int64
	hkStore    TargetHostKeyStore
}

// normalizeUsername appends @pam if no realm is specified (Proxmox requires user@realm format)
func normalizeUsername(username string) string {
	if !strings.Contains(username, "@") {
		return username + "@pam"
	}
	return username
}

func New(hostname string, port int, username, password string, validateCerts bool) *Provider {
	if port == 0 {
		port = 8006
	}
	p := &Provider{
		hostname:      hostname,
		port:          port,
		username:      normalizeUsername(username),
		password:      password,
		validateCerts: validateCerts,
		baseURL:       fmt.Sprintf("https://%s:%d/api2/json", hostname, port),
	}

	// PV-P2: Detect API token auth via regex instead of fragile string heuristics
	p.useAPIToken = apiTokenRe.MatchString(password)

	p.httpClient = &http.Client{
		Timeout: 120 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: !validateCerts,
			},
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}
	return p
}

// SetNode configures which Proxmox node to use. If not set, it is auto-detected.
func (p *Provider) SetNode(node string) {
	p.mu.Lock()
	p.node = node
	p.mu.Unlock()
}

// SetTOFU configures Trust-On-First-Use SSH host key verification for this target.
// If set, SSH connections to the Proxmox host will verify/store the host key fingerprint.
func (p *Provider) SetTOFU(targetID int64, store TargetHostKeyStore) {
	p.targetID = targetID
	p.hkStore = store
}

// GetNodeName returns the resolved node name (call after Connect).
func (p *Provider) GetNodeName() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.node
}

func (p *Provider) Connect(ctx context.Context) error {
	// API token auth does not require ticket
	if p.useAPIToken {
		return p.resolveNode(ctx)
	}

	// Ticket-based auth
	data := url.Values{
		"username": {p.username},
		"password": {p.password},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/access/ticket", strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("proxmox auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("proxmox auth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
		// V5-M4: Log raw response server-side, return generic error to caller
		slog.Debug("proxmox auth failed", "status", resp.StatusCode, "body", string(body))
		return fmt.Errorf("proxmox authentication failed (HTTP %d)", resp.StatusCode)
	}

	var result struct {
		Data struct {
			Ticket              string `json:"ticket"`
			CSRFPreventionToken string `json:"CSRFPreventionToken"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode auth response: %w", err)
	}
	if result.Data.Ticket == "" {
		return fmt.Errorf("proxmox auth: empty ticket returned")
	}

	p.mu.Lock()
	p.ticket = result.Data.Ticket
	p.csrfToken = result.Data.CSRFPreventionToken
	p.mu.Unlock()

	return p.resolveNode(ctx)
}

func (p *Provider) resolveNode(ctx context.Context) error {
	p.mu.RLock()
	hasNode := p.node != ""
	p.mu.RUnlock()
	if hasNode {
		return nil
	}

	body, err := p.doGet(ctx, "/nodes")
	if err != nil {
		return fmt.Errorf("list nodes: %w", err)
	}

	var result struct {
		Data []struct {
			Node   string `json:"node"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("decode nodes: %w", err)
	}
	if len(result.Data) == 0 {
		return fmt.Errorf("no nodes found on proxmox cluster")
	}

	// Pick first online node
	for _, n := range result.Data {
		if n.Status == "online" {
			p.mu.Lock()
			p.node = n.Node
			p.mu.Unlock()
			slog.Debug("proxmox node auto-detected", "node", p.node)
			return nil
		}
	}
	p.mu.Lock()
	p.node = result.Data[0].Node
	p.mu.Unlock()
	return nil
}

func (p *Provider) Disconnect() error {
	p.mu.Lock()
	p.ticket = ""
	p.csrfToken = ""
	p.mu.Unlock()
	if p.httpClient != nil {
		p.httpClient.CloseIdleConnections()
	}
	return nil
}

func (p *Provider) TestConnection(ctx context.Context) error {
	// Use a temporary provider to avoid corrupting the existing connection state
	tmp := New(p.hostname, p.port, p.username, p.password, p.validateCerts)
	if p.node != "" {
		tmp.SetNode(p.node)
	}
	if err := tmp.Connect(ctx); err != nil {
		return err
	}
	defer tmp.Disconnect()
	// Verify by fetching node status
	_, err := tmp.doGet(ctx, fmt.Sprintf("/nodes/%s/status", url.PathEscape(tmp.node)))
	return err
}

// PV-P11: ListTemplates queries ALL cluster nodes via /cluster/resources for completeness.
func (p *Provider) ListTemplates(ctx context.Context) ([]provider.Template, error) {
	body, err := p.doGet(ctx, "/cluster/resources?type=vm")
	if err != nil {
		return nil, fmt.Errorf("list VMs: %w", err)
	}

	var result struct {
		Data []struct {
			VMID     int    `json:"vmid"`
			Name     string `json:"name"`
			Template int    `json:"template"`
			CPUs     int    `json:"maxcpu"`
			MaxMem   int64  `json:"maxmem"`
			MaxDisk  int64  `json:"maxdisk"`
			Status   string `json:"status"`
			Node     string `json:"node"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode VMs: %w", err)
	}

	templates := []provider.Template{}
	for _, vm := range result.Data {
		if vm.Template != 1 {
			continue
		}
		t := provider.Template{
			ID:       strconv.Itoa(vm.VMID),
			Name:     vm.Name,
			Moref:    strconv.Itoa(vm.VMID),
			CPU:      vm.CPUs,
			MemoryMB: int(vm.MaxMem / 1024 / 1024),
			DiskGB:   int(vm.MaxDisk / 1024 / 1024 / 1024),
		}
		// PV-P13: Detect OS type from config
		osType := p.detectOSType(ctx, vm.Node, strconv.Itoa(vm.VMID))
		t.OSType = osType
		templates = append(templates, t)
	}
	return templates, nil
}

func (p *Provider) GetTemplate(ctx context.Context, id string) (*provider.Template, error) {
	// PV-P15: Determine which node the VM is on
	node, err := p.resolveVMNode(ctx, id)
	if err != nil {
		node = p.node // fallback
	}

	body, err := p.doGet(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/config", url.PathEscape(node), url.PathEscape(id)))
	if err != nil {
		return nil, fmt.Errorf("get VM config: %w", err)
	}

	var result struct {
		Data struct {
			Name    string `json:"name"`
			Cores   int    `json:"cores"`
			Sockets int    `json:"sockets"`
			Memory  int    `json:"memory"`
			OSType  string `json:"ostype"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode VM config: %w", err)
	}

	cpus := result.Data.Cores
	if result.Data.Sockets > 0 {
		cpus *= result.Data.Sockets
	}

	// PV-P13: Detect OS type from ostype field
	osType := "linux"
	if strings.HasPrefix(result.Data.OSType, "win") || strings.HasPrefix(result.Data.OSType, "w") {
		osType = "windows"
	}

	return &provider.Template{
		ID:       id,
		Name:     result.Data.Name,
		Moref:    id,
		CPU:      cpus,
		MemoryMB: result.Data.Memory,
		OSType:   osType,
		GuestID:  result.Data.OSType,
	}, nil
}

func (p *Provider) GetTemplateDetail(ctx context.Context, id string) (*provider.TemplateDetail, error) {
	node, err := p.resolveVMNode(ctx, id)
	if err != nil {
		node = p.node
	}

	body, err := p.doGet(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/config", url.PathEscape(node), url.PathEscape(id)))
	if err != nil {
		return nil, fmt.Errorf("get VM config: %w", err)
	}

	var result struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode VM config: %w", err)
	}

	data := result.Data

	// Basic template fields
	name, _ := data["name"].(string)
	cores := intFromJSON(data["cores"])
	sockets := intFromJSON(data["sockets"])
	if sockets > 0 {
		cores *= sockets
	}
	memory := intFromJSON(data["memory"])
	ostype, _ := data["ostype"].(string)

	osType := "linux"
	if strings.HasPrefix(ostype, "win") || strings.HasPrefix(ostype, "w") {
		osType = "windows"
	}

	detail := &provider.TemplateDetail{
		Template: provider.Template{
			ID:       id,
			Name:     name,
			Moref:    id,
			CPU:      cores,
			MemoryMB: memory,
			OSType:   osType,
			GuestID:  ostype,
		},
		Platform: "proxmox",
		Node:     node,
	}

	// CPU type (e.g. "host", "kvm64", "x86-64-v2-AES")
	if cpuType, ok := data["cpu"].(string); ok && cpuType != "" {
		detail.CPUType = cpuType
	}

	// SCSI controller type
	if scsihw, ok := data["scsihw"].(string); ok && scsihw != "" {
		detail.SCSIType = scsihw
	}

	// Cloud-init drive detection
	for i := 0; i < 4; i++ {
		for _, bus := range []string{"ide", "scsi", "sata"} {
			key := fmt.Sprintf("%s%d", bus, i)
			if val, ok := data[key].(string); ok && strings.Contains(val, "cloudinit") {
				detail.CloudInit = true
				break
			}
		}
		if detail.CloudInit {
			break
		}
	}

	// Datastore + disk size + format: parse from scsi0/virtio0/ide0/sata0 disk fields
	for _, diskKey := range []string{"scsi0", "virtio0", "ide0", "sata0"} {
		if val, ok := data[diskKey].(string); ok && val != "" && !strings.Contains(val, "cloudinit") {
			parts := strings.SplitN(val, ":", 2)
			if len(parts) == 2 {
				detail.Datastore = parts[0]
				// Parse disk size from value like "local-lvm:vm-102-disk-0,size=20G"
				for _, param := range strings.Split(parts[1], ",") {
					param = strings.TrimSpace(param)
					if strings.HasPrefix(param, "size=") {
						sizeStr := strings.TrimPrefix(param, "size=")
						sizeStr = strings.TrimRight(sizeStr, "GgMmTt")
						if sz, err := strconv.Atoi(sizeStr); err == nil {
							detail.DiskGB = sz
						}
					}
					if strings.HasPrefix(param, "format=") {
						detail.DiskFormat = strings.TrimPrefix(param, "format=")
					}
				}
				// Detect format from volume name if not explicit
				if detail.DiskFormat == "" {
					volPart := strings.SplitN(parts[1], ",", 2)[0]
					if strings.HasSuffix(volPart, ".qcow2") {
						detail.DiskFormat = "qcow2"
					} else if strings.HasSuffix(volPart, ".raw") || strings.HasSuffix(volPart, ".img") {
						detail.DiskFormat = "raw"
					} else if strings.Contains(detail.Datastore, "lvm") {
						detail.DiskFormat = "raw (LVM)"
					}
				}
				break
			}
		}
	}

	// Networks: parse net0, net1, etc for bridge names
	networks := []string{}
	for i := 0; i < 8; i++ {
		key := fmt.Sprintf("net%d", i)
		if val, ok := data[key].(string); ok && val != "" {
			for _, part := range strings.Split(val, ",") {
				part = strings.TrimSpace(part)
				if strings.HasPrefix(part, "bridge=") {
					networks = append(networks, strings.TrimPrefix(part, "bridge="))
				}
			}
		}
	}
	detail.Networks = networks

	// Firmware
	if bios, ok := data["bios"].(string); ok && bios != "" {
		detail.Firmware = bios
	} else {
		detail.Firmware = "seabios"
	}

	// Annotation from description
	if desc, ok := data["description"].(string); ok {
		detail.Annotation = desc
	}

	// Hardware version: QEMU + machine type
	if machine, ok := data["machine"].(string); ok && machine != "" {
		detail.HardwareVer = "QEMU " + machine
	} else {
		detail.HardwareVer = "QEMU"
	}

	// Tools status from agent field
	agentVal := intFromJSON(data["agent"])
	if agentVal == 1 {
		detail.ToolsStatus = "installed"
	} else {
		detail.ToolsStatus = "not installed"
	}

	return detail, nil
}

// intFromJSON extracts an int from a JSON value that may be float64 or string.
func intFromJSON(v interface{}) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case string:
		n, _ := strconv.Atoi(val)
		return n
	}
	return 0
}

func (p *Provider) DeployVM(ctx context.Context, spec *provider.DeploySpec) (*provider.DeployResult, error) {
	// Find the template VMID by name
	tmplID, tmplNode, err := p.findVMIDByName(ctx, spec.TemplateName)
	if err != nil {
		return nil, fmt.Errorf("find template %q: %w", spec.TemplateName, err)
	}

	// Get next available VMID
	newID, err := p.nextVMID(ctx)
	if err != nil {
		return nil, fmt.Errorf("get next VMID: %w", err)
	}

	// Clone template
	data := url.Values{
		"newid": {strconv.Itoa(newID)},
		"name":  {spec.VMName},
	}
	// PV-P5: Support linked clones
	if spec.LinkedClone {
		data.Set("full", "0")
	} else {
		data.Set("full", "1")
	}
	if spec.Datastore != "" {
		data.Set("storage", spec.Datastore)
	}

	body, err := p.doPost(ctx, fmt.Sprintf("/nodes/%s/qemu/%d/clone", url.PathEscape(tmplNode), tmplID), data)
	if err != nil {
		return nil, fmt.Errorf("clone VM: %w", err)
	}

	upid, err := extractUPID(body)
	if err != nil {
		return nil, err
	}

	// Wait for clone to complete before configuring
	if err := p.awaitTask(ctx, upid, 300); err != nil {
		return nil, fmt.Errorf("clone task failed: %w", err)
	}

	newIDStr := strconv.Itoa(newID)
	// PV-P15: Determine which node the new VM landed on
	vmNode, err := p.resolveVMNode(ctx, newIDStr)
	if err != nil {
		vmNode = tmplNode // fallback to template's node
	}

	// PV-P4: Apply cloud-init and hardware config after clone
	configData := url.Values{}
	if spec.CPU > 0 {
		configData.Set("cores", strconv.Itoa(spec.CPU))
	}
	if spec.MemoryMB > 0 {
		configData.Set("memory", strconv.Itoa(spec.MemoryMB))
	}
	if spec.Network != "" {
		configData.Set("net0", fmt.Sprintf("virtio,bridge=%s", spec.Network))
	}

	// When UserDataOverride is set (actions selected), upload a snippet and
	// use cicustom instead of inline ciuser/cipassword. The merged userdata
	// already contains credentials + action commands.
	if spec.UserDataOverride != "" {
		snippetName := fmt.Sprintf("%s-userdata.yml", spec.VMName)
		if err := p.uploadSnippet(ctx, vmNode, "local", snippetName, spec.UserDataOverride); err != nil {
			// Security-first: do NOT silently fall back. If actions were requested
			// but can't be applied, the user would think they're protected (e.g. UFW)
			// when they're not. Fail loudly instead.
			return nil, fmt.Errorf("failed to upload cloud-init snippet for post-deploy actions: %w (ensure Proxmox host is SSH-accessible)", err)
		} else {
			configData.Set("cicustom", fmt.Sprintf("user=local:snippets/%s", snippetName))
			// With cicustom, set ipconfig0 for network but skip ciuser/cipassword/sshkeys
			if spec.IPAddress != "" {
				cidr := "24"
				if spec.Netmask != "" {
					cidr = netmaskToCIDR(spec.Netmask)
				}
				ipConfig := fmt.Sprintf("ip=%s/%s", spec.IPAddress, cidr)
				if spec.Gateway != "" {
					ipConfig += fmt.Sprintf(",gw=%s", spec.Gateway)
				}
				configData.Set("ipconfig0", ipConfig)
			} else {
				configData.Set("ipconfig0", "ip=dhcp")
			}
			if spec.Hostname != "" {
				configData.Set("name", spec.Hostname)
			}
			if spec.DomainName != "" {
				configData.Set("searchdomain", spec.DomainName)
			}
			if len(spec.DNS) > 0 {
				configData.Set("nameserver", strings.Join(spec.DNS, " "))
			}
			goto applyConfig
		}
	}

	// Cloud-init settings (standard inline approach when no actions)
	if spec.IPAddress != "" {
		cidr := "24"
		if spec.Netmask != "" {
			cidr = netmaskToCIDR(spec.Netmask)
		}
		ipConfig := fmt.Sprintf("ip=%s/%s", spec.IPAddress, cidr)
		if spec.Gateway != "" {
			ipConfig += fmt.Sprintf(",gw=%s", spec.Gateway)
		}
		configData.Set("ipconfig0", ipConfig)
	} else {
		// Default to DHCP when no static IP is specified.
		// Without ipconfig0, Proxmox cloud-init may leave the NIC unconfigured.
		configData.Set("ipconfig0", "ip=dhcp")
	}
	// F-96: Set hostname in cloud-init config (was missing — only DNS was set)
	if spec.Hostname != "" {
		configData.Set("name", spec.Hostname)
	}
	if spec.DomainName != "" {
		configData.Set("searchdomain", spec.DomainName)
	}
	if len(spec.DNS) > 0 {
		configData.Set("nameserver", strings.Join(spec.DNS, " "))
	}
	// BUG-03: Proxmox cipassword expects a plaintext password — it passes
	// the value directly to cloud-init which hashes it internally. Sending
	// a pre-hashed $6$ string would make the literal hash the password.
	// Use PlainPassword when available; fall back to PasswordHash for
	// backwards compatibility with older callers.
	if spec.PlainPassword != "" {
		configData.Set("ciuser", "forgemill")
		configData.Set("cipassword", spec.PlainPassword)
	} else if spec.PasswordHash != "" {
		configData.Set("ciuser", "forgemill")
		configData.Set("cipassword", spec.PasswordHash)
	}
	if spec.SSHPublicKey != "" {
		configData.Set("sshkeys", url.QueryEscape(spec.SSHPublicKey))
	}

applyConfig:

	if len(configData) > 0 {
		configPath := fmt.Sprintf("/nodes/%s/qemu/%s/config", url.PathEscape(vmNode), newIDStr)
		slog.Info("configuring VM after clone", "vmid", newID, "node", vmNode, "config_keys", redactConfigKeys(configData))
		// Retry up to 5 times with 3s delay — Proxmox may still hold a lock after clone completes
		var configErr error
		for attempt := 0; attempt < 5; attempt++ {
			if attempt > 0 {
				slog.Info("retrying VM config", "vmid", newID, "attempt", attempt+1)
				time.Sleep(3 * time.Second)
			}
			configErr = p.doPut(ctx, configPath, configData)
			if configErr == nil {
				slog.Info("VM configured successfully", "vmid", newID)
				break
			}
		}
		if configErr != nil {
			slog.Error("failed to configure VM after clone (all retries exhausted)", "vmid", newID, "error", configErr)
		}
	}

	// Resize disk if requested size differs from template
	if spec.DiskGB > 0 {
		disk, err := p.findDiskByIndex(ctx, vmNode, newIDStr, 0)
		if err != nil {
			slog.Warn("failed to find disk for resize", "vmid", newID, "error", err)
		} else {
			resizeData := url.Values{
				"disk": {disk},
				"size": {fmt.Sprintf("%dG", spec.DiskGB)},
			}
			if err := p.doPut(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/resize", url.PathEscape(vmNode), newIDStr), resizeData); err != nil {
				slog.Warn("failed to resize disk after clone", "vmid", newID, "size_gb", spec.DiskGB, "error", err)
			}
		}
	}

	// PV-P6: Start VM after clone
	startBody, err := p.doPost(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/status/start", url.PathEscape(vmNode), newIDStr), nil)
	if err != nil {
		slog.Warn("failed to start VM after clone", "vmid", newID, "error", err)
	} else if startUPID, err := extractUPID(startBody); err == nil {
		_ = p.awaitTask(ctx, startUPID, 60)
	}

	result := &provider.DeployResult{
		TaskID: upid,
		VMID:   newIDStr,
	}
	return result, nil
}

// PV-X3: Map Proxmox task states to canonical provider constants.
// PV-P16: Parse node from UPID for task routing.
func (p *Provider) GetDeployProgress(ctx context.Context, taskID string) (*provider.Progress, error) {
	node := nodeFromUPID(taskID)
	if node == "" {
		node = p.node
	}

	body, err := p.doGet(ctx, fmt.Sprintf("/nodes/%s/tasks/%s/status", url.PathEscape(node), url.PathEscape(taskID)))
	if err != nil {
		return nil, fmt.Errorf("get task status: %w", err)
	}

	var result struct {
		Data struct {
			Status     string `json:"status"`
			ExitStatus string `json:"exitstatus"`
			PID        int    `json:"pid"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode task status: %w", err)
	}

	progress := &provider.Progress{}
	switch result.Data.Status {
	case "running":
		progress.State = provider.ProgressStateRunning
		progress.Percent = 50
		progress.Message = "Clone in progress..."
	case "stopped":
		if result.Data.ExitStatus == "OK" {
			progress.State = provider.ProgressStateSuccess
			progress.Percent = 100
			progress.Message = "Deployment completed"
		} else {
			progress.State = provider.ProgressStateError
			progress.Message = fmt.Sprintf("Task failed: %s", result.Data.ExitStatus)
		}
	default:
		progress.State = provider.ProgressStateQueued
		progress.Message = "Waiting..."
	}
	return progress, nil
}

func (p *Provider) PowerOn(ctx context.Context, vmID string) error {
	node, err := p.resolveVMNode(ctx, vmID)
	if err != nil {
		node = p.node
	}

	// Check if VM is suspended (paused) — Proxmox returns "VM already running" for /status/start
	status, err := p.GetVMStatus(ctx, vmID)
	if err == nil && status.PowerState == "suspended" {
		slog.Info("VM is suspended, using resume instead of start", "vmID", vmID)
		body, err := p.doPost(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/status/resume", url.PathEscape(node), url.PathEscape(vmID)), nil)
		if err != nil {
			return err
		}
		if upid, err := extractUPID(body); err == nil {
			return p.awaitTask(ctx, upid, 60)
		}
		return nil
	}

	slog.Debug("starting VM", "vmID", vmID)
	body, err := p.doPost(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/status/start", url.PathEscape(node), url.PathEscape(vmID)), nil)
	if err != nil {
		return err
	}
	if upid, err := extractUPID(body); err == nil {
		return p.awaitTask(ctx, upid, 60)
	}
	return nil
}

// PV-P7: Graceful ACPI shutdown first, then hard stop fallback.
func (p *Provider) PowerOff(ctx context.Context, vmID string) error {
	node, err := p.resolveVMNode(ctx, vmID)
	if err != nil {
		node = p.node
	}
	// Try graceful ACPI shutdown first
	data := url.Values{"timeout": {"90"}}
	body, err := p.doPost(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/status/shutdown", url.PathEscape(node), url.PathEscape(vmID)), data)
	if err != nil {
		// Fall back to hard stop
		slog.Info("ACPI shutdown failed, falling back to hard stop", "vmID", vmID, "error", err)
		body, err = p.doPost(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/status/stop", url.PathEscape(node), url.PathEscape(vmID)), nil)
		if err != nil {
			return err
		}
	}
	if upid, err := extractUPID(body); err == nil {
		return p.awaitTask(ctx, upid, 120)
	}
	return nil
}

func (p *Provider) Restart(ctx context.Context, vmID string) error {
	node, err := p.resolveVMNode(ctx, vmID)
	if err != nil {
		node = p.node
	}
	// Try graceful ACPI reboot first (requires guest agent / ACPI support)
	body, err := p.doPost(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/status/reboot", url.PathEscape(node), url.PathEscape(vmID)), nil)
	if err == nil {
		if upid, uErr := extractUPID(body); uErr == nil {
			if tErr := p.awaitTask(ctx, upid, 60); tErr == nil {
				return nil
			} else {
				slog.Info("ACPI reboot timed out, falling back to hard stop+start", "vmID", vmID, "error", tErr)
			}
		}
	} else {
		slog.Info("ACPI reboot failed, falling back to hard stop+start", "vmID", vmID, "error", err)
	}

	// Fallback: hard stop then start
	stopBody, err := p.doPost(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/status/stop", url.PathEscape(node), url.PathEscape(vmID)), nil)
	if err != nil {
		return fmt.Errorf("hard stop during restart fallback: %w", err)
	}
	if upid, err := extractUPID(stopBody); err == nil {
		if err := p.awaitTask(ctx, upid, 60); err != nil {
			return fmt.Errorf("hard stop task failed during restart fallback: %w", err)
		}
	}

	startBody, err := p.doPost(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/status/start", url.PathEscape(node), url.PathEscape(vmID)), nil)
	if err != nil {
		return fmt.Errorf("start during restart fallback: %w", err)
	}
	if upid, err := extractUPID(startBody); err == nil {
		return p.awaitTask(ctx, upid, 60)
	}
	return nil
}

func (p *Provider) DeleteVM(ctx context.Context, vmID string) error {
	node, err := p.resolveVMNode(ctx, vmID)
	if err != nil {
		node = p.node
	}
	// PV-P7: Graceful shutdown before deletion
	_ = p.PowerOff(ctx, vmID)
	// Poll until stopped or timeout
	for i := 0; i < 30; i++ {
		status, err := p.GetVMStatus(ctx, vmID)
		if err != nil {
			break
		}
		if status.PowerState == "stopped" {
			break
		}
		time.Sleep(1 * time.Second)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, p.baseURL+fmt.Sprintf("/nodes/%s/qemu/%s", url.PathEscape(node), url.PathEscape(vmID)), nil)
	if err != nil {
		return fmt.Errorf("create delete request: %w", err)
	}
	p.setAuth(req)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete VM: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if resp.StatusCode >= 300 {
		slog.Debug("proxmox delete VM failed", "status", resp.StatusCode, "body", string(body))
		return fmt.Errorf("delete VM failed (HTTP %d)", resp.StatusCode)
	}

	// Await the delete task UPID to ensure deletion completes
	upid, err := extractUPID(body)
	if err != nil {
		// Some Proxmox versions may not return a UPID for delete; treat as success
		slog.Debug("proxmox delete VM: could not extract UPID, assuming synchronous completion", "error", err)
		return nil
	}
	return p.awaitTask(ctx, upid, 60)
}

// awaitTask polls a Proxmox task UPID until it completes or the timeout (seconds) is reached.
// PV-P16: Parses the UPID to route task status queries to the correct node.
func (p *Provider) awaitTask(ctx context.Context, upid string, timeoutSec int) error {
	node := nodeFromUPID(upid)
	if node == "" {
		node = p.node
	}

	for i := 0; i < timeoutSec; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		body, err := p.doGet(ctx, fmt.Sprintf("/nodes/%s/tasks/%s/status", url.PathEscape(node), url.PathEscape(upid)))
		if err != nil {
			return fmt.Errorf("poll task status: %w", err)
		}
		var result struct {
			Data struct {
				Status     string `json:"status"`
				ExitStatus string `json:"exitstatus"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("decode task status: %w", err)
		}
		if result.Data.Status == "stopped" {
			if result.Data.ExitStatus == "OK" {
				return nil
			}
			return fmt.Errorf("task failed: %s", result.Data.ExitStatus)
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("task %s did not complete within %ds", upid, timeoutSec)
}

// PV-X2: Normalize power state to canonical values.
// PV-P10: Attempt guest agent IP retrieval.
func (p *Provider) GetVMStatus(ctx context.Context, vmID string) (*provider.VMStatus, error) {
	node, err := p.resolveVMNode(ctx, vmID)
	if err != nil {
		node = p.node
	}

	body, err := p.doGet(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/status/current", url.PathEscape(node), url.PathEscape(vmID)))
	if err != nil {
		return nil, fmt.Errorf("get VM status: %w", err)
	}

	var result struct {
		Data struct {
			Status    string  `json:"status"`
			QMPStatus string  `json:"qmpstatus"`
			Name      string  `json:"name"`
			CPUs      int     `json:"cpus"`
			MaxMem    int64   `json:"maxmem"`
			MaxDisk   int64   `json:"maxdisk"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode VM status: %w", err)
	}

	powerState := result.Data.Status
	if result.Data.QMPStatus != "" {
		powerState = result.Data.QMPStatus
	}

	status := &provider.VMStatus{
		PowerState: provider.NormalizePowerState(powerState),
		HostName:   result.Data.Name,
		CPU:        result.Data.CPUs,
		MemoryMB:   int(result.Data.MaxMem / 1024 / 1024),
		DiskGB:     int(result.Data.MaxDisk / 1024 / 1024 / 1024),
		GuestID:    "linux", // Proxmox doesn't expose guest ID in status
	}

	// PV-P10: Try guest agent for IP address
	if powerState == "running" {
		if ip := p.getGuestAgentIP(ctx, node, vmID); ip != "" {
			status.IPAddress = ip
		}
	}

	return status, nil
}

// PV-P14: Use cluster-level /storage for resource discovery instead of node-specific.
func (p *Provider) GetResources(ctx context.Context) (*provider.Resources, error) {
	resources := &provider.Resources{
		Datastores:    []provider.ResourceItem{},
		Networks:      []provider.ResourceItem{},
		Folders:       []provider.ResourceItem{},
		Clusters:      []provider.ResourceItem{},
		Datacenters:   []provider.ResourceItem{},
		ResourcePools: []provider.ResourceItem{},
		Platform:      "proxmox",
		Defaults:      map[string]string{"node": p.node},
	}

	// PV-P14: Cluster-level storage
	storageBody, err := p.doGet(ctx, "/storage")
	if err == nil {
		var storageResult struct {
			Data []struct {
				Storage string `json:"storage"`
				Type    string `json:"type"`
			} `json:"data"`
		}
		if json.Unmarshal(storageBody, &storageResult) == nil {
			for _, s := range storageResult.Data {
				resources.Datastores = append(resources.Datastores, provider.ResourceItem{
					Name: s.Storage,
					ID:   s.Storage,
				})
				// ISO-capable storage: only directory, NFS, CIFS, etc. — not LVM/ZFS block storage
				switch s.Type {
				case "dir", "nfs", "cifs", "glusterfs", "cephfs", "btrfs":
					resources.ISOStorages = append(resources.ISOStorages, provider.ResourceItem{
						Name: s.Storage,
						ID:   s.Storage,
					})
				}
			}
		}
	}

	// Networks from current node
	networkBody, err := p.doGet(ctx, fmt.Sprintf("/nodes/%s/network", url.PathEscape(p.node)))
	if err == nil {
		var networkResult struct {
			Data []struct {
				Iface  string `json:"iface"`
				Type   string `json:"type"`
				Active int    `json:"active"`
			} `json:"data"`
		}
		if json.Unmarshal(networkBody, &networkResult) == nil {
			for _, n := range networkResult.Data {
				if n.Type == "bridge" {
					resources.Networks = append(resources.Networks, provider.ResourceItem{
						Name: n.Iface,
						ID:   n.Iface,
					})
				}
			}
		}
	}

	// List all nodes as datacenters
	nodesBody, err := p.doGet(ctx, "/nodes")
	if err == nil {
		var nodesResult struct {
			Data []struct {
				Node   string `json:"node"`
				Status string `json:"status"`
			} `json:"data"`
		}
		if json.Unmarshal(nodesBody, &nodesResult) == nil {
			for _, n := range nodesResult.Data {
				resources.Datacenters = append(resources.Datacenters, provider.ResourceItem{
					Name: n.Node,
					ID:   n.Node,
				})
			}
		}
	} else {
		resources.Datacenters = append(resources.Datacenters, provider.ResourceItem{
			Name: p.node,
			ID:   p.node,
		})
	}

	// Resource pools
	poolsBody, err := p.doGet(ctx, "/pools")
	if err == nil {
		var poolsResult struct {
			Data []struct {
				PoolID  string `json:"poolid"`
				Comment string `json:"comment"`
			} `json:"data"`
		}
		if json.Unmarshal(poolsBody, &poolsResult) == nil {
			for _, rp := range poolsResult.Data {
				resources.ResourcePools = append(resources.ResourcePools, provider.ResourceItem{
					Name: rp.PoolID,
					ID:   rp.PoolID,
				})
			}
		}
	}

	// Populate smart defaults with first available resource of each type.
	// NOTE: Don't default datastore for Proxmox — omitting "storage" on clone
	// uses the template's current storage, which is always correct. Defaulting
	// to the first storage (e.g. "local" dir type) breaks clones to block storage.
	if len(resources.Networks) > 0 {
		resources.Defaults["network"] = resources.Networks[0].Name
	}

	return resources, nil
}

func (p *Provider) Suspend(ctx context.Context, vmID string) error {
	node, err := p.resolveVMNode(ctx, vmID)
	if err != nil {
		node = p.node
	}
	body, err := p.doPost(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/status/suspend", url.PathEscape(node), url.PathEscape(vmID)), nil)
	if err != nil {
		return err
	}
	if upid, err := extractUPID(body); err == nil {
		return p.awaitTask(ctx, upid, 60)
	}
	return nil
}

// PV-P8: Snapshot operations await task completion via UPID.
func (p *Provider) ListSnapshots(ctx context.Context, vmID string) ([]provider.Snapshot, error) {
	node, err := p.resolveVMNode(ctx, vmID)
	if err != nil {
		node = p.node
	}

	body, err := p.doGet(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/snapshot", url.PathEscape(node), url.PathEscape(vmID)))
	if err != nil {
		return nil, fmt.Errorf("list snapshots: %w", err)
	}

	var result struct {
		Data []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Snaptime    int64  `json:"snaptime"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode snapshots: %w", err)
	}

	snapshots := []provider.Snapshot{}
	for _, s := range result.Data {
		if s.Name == "current" {
			continue
		}
		snapshots = append(snapshots, provider.Snapshot{
			Ref:         s.Name,
			Name:        s.Name,
			Description: s.Description,
			Created:     time.Unix(s.Snaptime, 0).UTC().Format(time.RFC3339),
		})
	}
	return snapshots, nil
}

// PV-P8: CreateSnapshot awaits task completion.
func (p *Provider) CreateSnapshot(ctx context.Context, vmID string, name string, description string, memory bool) error {
	node, err := p.resolveVMNode(ctx, vmID)
	if err != nil {
		node = p.node
	}

	data := url.Values{
		"snapname":    {name},
		"description": {description},
	}
	if memory {
		data.Set("vmstate", "1")
	}
	body, err := p.doPost(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/snapshot", url.PathEscape(node), url.PathEscape(vmID)), data)
	if err != nil {
		return fmt.Errorf("create snapshot: %w", err)
	}
	if upid, err := extractUPID(body); err == nil {
		return p.awaitTask(ctx, upid, 120)
	}
	return nil
}

// PV-P8: RevertSnapshot awaits task completion.
func (p *Provider) RevertSnapshot(ctx context.Context, vmID string, snapshotRef string) error {
	node, err := p.resolveVMNode(ctx, vmID)
	if err != nil {
		node = p.node
	}

	body, err := p.doPost(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/snapshot/%s/rollback", url.PathEscape(node), url.PathEscape(vmID), url.PathEscape(snapshotRef)), nil)
	if err != nil {
		return fmt.Errorf("revert snapshot: %w", err)
	}
	if upid, err := extractUPID(body); err == nil {
		return p.awaitTask(ctx, upid, 120)
	}
	return nil
}

// PV-P8: DeleteSnapshot awaits task completion.
func (p *Provider) DeleteSnapshot(ctx context.Context, vmID string, snapshotRef string) error {
	node, err := p.resolveVMNode(ctx, vmID)
	if err != nil {
		node = p.node
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, p.baseURL+fmt.Sprintf("/nodes/%s/qemu/%s/snapshot/%s", url.PathEscape(node), url.PathEscape(vmID), url.PathEscape(snapshotRef)), nil)
	if err != nil {
		return fmt.Errorf("create delete request: %w", err)
	}
	p.setAuth(req)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete snapshot: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if resp.StatusCode >= 300 {
		slog.Debug("proxmox delete snapshot failed", "status", resp.StatusCode, "body", string(body))
		return fmt.Errorf("delete snapshot failed (HTTP %d)", resp.StatusCode)
	}
	if upid, err := extractUPID(body); err == nil {
		return p.awaitTask(ctx, upid, 120)
	}
	return nil
}

func (p *Provider) ResizeVM(ctx context.Context, vmID string, cpu int, memoryMB int) error {
	node, err := p.resolveVMNode(ctx, vmID)
	if err != nil {
		node = p.node
	}

	data := url.Values{}
	if cpu > 0 {
		data.Set("cores", strconv.Itoa(cpu))
	}
	if memoryMB > 0 {
		data.Set("memory", strconv.Itoa(memoryMB))
	}
	return p.doPut(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/config", url.PathEscape(node), url.PathEscape(vmID)), data)
}

// PV-P9: Discover actual disk interface name instead of assuming scsi{n}.
func (p *Provider) ListDisks(ctx context.Context, vmID string) ([]provider.Disk, error) {
	node, err := p.resolveVMNode(ctx, vmID)
	if err != nil {
		node = p.node
	}

	body, err := p.doGet(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/config", url.PathEscape(node), url.PathEscape(vmID)))
	if err != nil {
		return nil, fmt.Errorf("get VM config: %w", err)
	}
	var result struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	prefixes := []string{"scsi", "virtio", "ide", "sata"}
	var disks []provider.Disk
	idx := 0
	for _, prefix := range prefixes {
		for i := 0; i < 30; i++ {
			key := fmt.Sprintf("%s%d", prefix, i)
			val, ok := result.Data[key]
			if !ok {
				continue
			}
			// Parse size from "local:vm-100-disk-0,size=32G" style values
			sizeGB := 0
			if s, ok := val.(string); ok {
				for _, part := range strings.Split(s, ",") {
					if strings.HasPrefix(part, "size=") {
						sizeStr := strings.TrimPrefix(part, "size=")
						sizeStr = strings.TrimSuffix(sizeStr, "G")
						if n, err := strconv.Atoi(sizeStr); err == nil {
							sizeGB = n
						}
					}
				}
			}
			disks = append(disks, provider.Disk{
				Key:    idx,
				Label:  key,
				SizeGB: sizeGB,
			})
			idx++
		}
	}
	return disks, nil
}

func (p *Provider) ExpandDisk(ctx context.Context, vmID string, diskKey int, newSizeGB int) error {
	node, err := p.resolveVMNode(ctx, vmID)
	if err != nil {
		node = p.node
	}

	disk, err := p.findDiskByIndex(ctx, node, vmID, diskKey)
	if err != nil {
		return fmt.Errorf("find disk: %w", err)
	}

	data := url.Values{
		"disk": {disk},
		"size": {fmt.Sprintf("%dG", newSizeGB)},
	}
	return p.doPut(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/resize", url.PathEscape(node), url.PathEscape(vmID)), data)
}

func (p *Provider) GetConsoleURL(ctx context.Context, vmID string) (string, error) {
	// F-97: Resolve VM's actual node instead of using p.node directly
	node, err := p.resolveVMNode(ctx, vmID)
	if err != nil {
		node = p.node // fallback
	}
	return fmt.Sprintf("https://%s:%d/?console=kvm&novnc=1&vmid=%s&node=%s", p.hostname, p.port, url.QueryEscape(vmID), url.QueryEscape(node)), nil
}

// PV-P12: ListVMs queries ALL cluster nodes via /cluster/resources for completeness.
func (p *Provider) ListVMs(ctx context.Context) ([]provider.VMInfo, error) {
	body, err := p.doGet(ctx, "/cluster/resources?type=vm")
	if err != nil {
		return nil, fmt.Errorf("list VMs: %w", err)
	}

	var result struct {
		Data []struct {
			VMID     int    `json:"vmid"`
			Name     string `json:"name"`
			Template int    `json:"template"`
			CPUs     int    `json:"maxcpu"`
			MaxMem   int64  `json:"maxmem"`
			Status   string `json:"status"`
			Node     string `json:"node"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode VMs: %w", err)
	}

	vms := []provider.VMInfo{}
	for _, vm := range result.Data {
		if vm.Template == 1 {
			continue
		}
		vms = append(vms, provider.VMInfo{
			ID:         strconv.Itoa(vm.VMID),
			Name:       vm.Name,
			PowerState: provider.NormalizePowerState(vm.Status),
			CPU:        vm.CPUs,
			MemoryMB:   int(vm.MaxMem / 1024 / 1024),
		})
	}
	return vms, nil
}

// --- HTTP helpers ---

// PV-P1: Fixed API token auth header to use correct format.
func (p *Provider) setAuth(req *http.Request) {
	if p.useAPIToken {
		// Proxmox API token auth: Authorization: PVEAPIToken=user@realm!tokenid=secret
		// The password field contains the full token string in user@realm!tokenid=secret format
		req.Header.Set("Authorization", "PVEAPIToken="+p.password)
		return
	}
	p.mu.RLock()
	ticket := p.ticket
	csrf := p.csrfToken
	p.mu.RUnlock()
	if ticket != "" {
		req.AddCookie(&http.Cookie{Name: "PVEAuthCookie", Value: ticket})
		req.Header.Set("CSRFPreventionToken", csrf)
	}
}

// PV-P3: doGet with auto re-auth on 401 for ticket-based auth.
func (p *Provider) doGet(ctx context.Context, path string) ([]byte, error) {
	body, statusCode, err := p.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	// PV-P3: Re-authenticate on 401 for ticket auth
	p.mu.RLock()
	hasTicket := p.ticket != ""
	p.mu.RUnlock()
	if statusCode == http.StatusUnauthorized && !p.useAPIToken && hasTicket {
		slog.Debug("proxmox ticket expired, re-authenticating")
		if err := p.Connect(ctx); err != nil {
			return nil, fmt.Errorf("re-auth failed: %w", err)
		}
		body, statusCode, err = p.doRequest(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
	}
	if statusCode >= 300 {
		slog.Debug("proxmox GET failed", "path", path, "status", statusCode, "body", string(body))
		return nil, fmt.Errorf("proxmox request failed (HTTP %d)", statusCode)
	}
	return body, nil
}

func (p *Provider) doPost(ctx context.Context, path string, data url.Values) ([]byte, error) {
	var bodyReader io.Reader
	contentType := ""
	if data != nil {
		bodyReader = strings.NewReader(data.Encode())
		contentType = "application/x-www-form-urlencoded"
	}

	body, statusCode, err := p.doRequestWithBody(ctx, http.MethodPost, path, bodyReader, contentType)
	if err != nil {
		return nil, err
	}
	// PV-P3: Re-authenticate on 401 for ticket auth
	p.mu.RLock()
	hasTicket := p.ticket != ""
	p.mu.RUnlock()
	if statusCode == http.StatusUnauthorized && !p.useAPIToken && hasTicket {
		slog.Debug("proxmox ticket expired, re-authenticating")
		if err := p.Connect(ctx); err != nil {
			return nil, fmt.Errorf("re-auth failed: %w", err)
		}
		if data != nil {
			bodyReader = strings.NewReader(data.Encode())
		}
		body, statusCode, err = p.doRequestWithBody(ctx, http.MethodPost, path, bodyReader, contentType)
		if err != nil {
			return nil, err
		}
	}
	if statusCode >= 300 {
		slog.Debug("proxmox POST failed", "path", path, "status", statusCode, "body", string(body))
		return nil, fmt.Errorf("proxmox request failed (HTTP %d)", statusCode)
	}
	return body, nil
}

func (p *Provider) doPut(ctx context.Context, path string, data url.Values) error {
	var bodyReader io.Reader
	contentType := ""
	if data != nil {
		bodyReader = strings.NewReader(data.Encode())
		contentType = "application/x-www-form-urlencoded"
	}

	body, statusCode, err := p.doRequestWithBody(ctx, http.MethodPut, path, bodyReader, contentType)
	if err != nil {
		return err
	}
	// F-98: Re-authenticate on 401 for ticket auth (matching doGet/doPost behavior)
	p.mu.RLock()
	hasTicket := p.ticket != ""
	p.mu.RUnlock()
	if statusCode == http.StatusUnauthorized && !p.useAPIToken && hasTicket {
		slog.Debug("proxmox ticket expired, re-authenticating")
		if err := p.Connect(ctx); err != nil {
			return fmt.Errorf("re-auth failed: %w", err)
		}
		if data != nil {
			bodyReader = strings.NewReader(data.Encode())
		}
		body, statusCode, err = p.doRequestWithBody(ctx, http.MethodPut, path, bodyReader, contentType)
		if err != nil {
			return err
		}
	}
	if statusCode >= 300 {
		slog.Debug("proxmox PUT failed", "path", path, "status", statusCode, "body", string(body))
		return fmt.Errorf("proxmox request failed (HTTP %d)", statusCode)
	}
	return nil
}

func (p *Provider) doRequest(ctx context.Context, method, path string, bodyReader io.Reader) ([]byte, int, error) {
	return p.doRequestWithBody(ctx, method, path, bodyReader, "")
}

func (p *Provider) doRequestWithBody(ctx context.Context, method, path string, bodyReader io.Reader, contentType string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, bodyReader)
	if err != nil {
		return nil, 0, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	p.setAuth(req)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// --- Helper functions ---

// PV-P15: Resolve which node a VM is running on via /cluster/resources.
func (p *Provider) resolveVMNode(ctx context.Context, vmID string) (string, error) {
	body, err := p.doGet(ctx, "/cluster/resources?type=vm")
	if err != nil {
		return "", err
	}

	var result struct {
		Data []struct {
			VMID int    `json:"vmid"`
			Node string `json:"node"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	vmIDInt, err := strconv.Atoi(vmID)
	if err != nil {
		return "", fmt.Errorf("invalid vmid: %w", err)
	}
	for _, vm := range result.Data {
		if vm.VMID == vmIDInt {
			return vm.Node, nil
		}
	}
	return "", fmt.Errorf("VM %s not found in cluster", vmID)
}

// PV-P13: Detect OS type from Proxmox VM config.
func (p *Provider) detectOSType(ctx context.Context, node, vmID string) string {
	body, err := p.doGet(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/config", url.PathEscape(node), url.PathEscape(vmID)))
	if err != nil {
		return "linux"
	}
	var result struct {
		Data struct {
			OSType string `json:"ostype"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &result) != nil {
		return "linux"
	}
	if strings.HasPrefix(result.Data.OSType, "win") || strings.HasPrefix(result.Data.OSType, "w") {
		return "windows"
	}
	return "linux"
}

// PV-P10: Get IP address from guest agent.
func (p *Provider) getGuestAgentIP(ctx context.Context, node, vmID string) string {
	body, err := p.doGet(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/agent/network-get-interfaces", url.PathEscape(node), url.PathEscape(vmID)))
	if err != nil {
		return ""
	}
	var result struct {
		Data struct {
			Result []struct {
				Name          string `json:"name"`
				IPAddresses   []struct {
					IPAddressType string `json:"ip-address-type"`
					IPAddress     string `json:"ip-address"`
				} `json:"ip-addresses"`
			} `json:"result"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &result) != nil {
		return ""
	}
	for _, iface := range result.Data.Result {
		if iface.Name == "lo" || iface.Name == "lo0" {
			continue
		}
		for _, addr := range iface.IPAddresses {
			if addr.IPAddressType == "ipv4" && addr.IPAddress != "127.0.0.1" {
				return addr.IPAddress
			}
		}
	}
	return ""
}

// uploadSnippet uploads a cloud-init snippet file to Proxmox storage.
// Uses the Proxmox storage upload API endpoint for snippet content type.
func (p *Provider) uploadSnippet(ctx context.Context, node, storage, filename, content string) error {
	// Proxmox storage upload API does NOT support snippet content type
	// (only iso, vztmpl, import). Write snippet directly via SSH instead.
	sshUser := p.username
	if idx := strings.Index(sshUser, "@"); idx >= 0 {
		sshUser = sshUser[:idx] // root@pam → root
	}

	// Build host key callback: TOFU when store is available, insecure fallback with warning
	hostKeyCallback := ssh.InsecureIgnoreHostKey()
	if p.hkStore != nil && p.targetID > 0 {
		storedFP, err := p.hkStore.GetTargetSSHHostKeyFP(p.targetID)
		if err != nil {
			slog.Warn("proxmox-ssh: failed to read stored host key, using insecure fallback", "target_id", p.targetID, "error", err)
		} else {
			hostKeyCallback = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
				fp := ssh.FingerprintSHA256(key)
				if storedFP == "" {
					// First connection — trust and store (TOFU)
					if storeErr := p.hkStore.UpdateTargetSSHHostKeyFP(p.targetID, fp); storeErr != nil {
						slog.Warn("proxmox-ssh: failed to store host key fingerprint", "target_id", p.targetID, "error", storeErr)
					} else {
						slog.Info("proxmox-ssh: stored host key on first connect (TOFU)", "target_id", p.targetID, "fingerprint", fp)
					}
					return nil
				}
				// Subsequent connection — verify
				if fp != storedFP {
					return fmt.Errorf("SSH host key mismatch for target %d (%s): expected %s, got %s — possible MITM or host was rebuilt", p.targetID, hostname, storedFP, fp)
				}
				return nil
			}
		}
	} else {
		slog.Warn("proxmox-ssh: TOFU not configured, using insecure host key verification", "hostname", p.hostname)
	}

	config := &ssh.ClientConfig{
		User: sshUser,
		Auth: []ssh.AuthMethod{
			ssh.Password(p.password),
		},
		HostKeyCallback: hostKeyCallback,
		Timeout:         10 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", p.hostname, 22)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("ssh connect to proxmox host: %w", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh session: %w", err)
	}
	defer session.Close()

	// Resolve storage path. Default Proxmox dir storage path is /var/lib/vz.
	storagePath := "/var/lib/vz"

	// Sanitise filename to prevent shell injection. Only allow safe characters
	// even though VMName is validated upstream — defense in depth.
	safeFilename := sanitiseSnippetFilename(filename)
	if safeFilename == "" {
		return fmt.Errorf("invalid snippet filename after sanitisation: %q", filename)
	}

	// Ensure snippets directory exists and write the file.
	// Use shell-quoted filename to prevent injection via any remaining edge cases.
	cmd := fmt.Sprintf("mkdir -p %s/snippets && cat > %s/snippets/'%s'", storagePath, storagePath, safeFilename)
	session.Stdin = strings.NewReader(content)
	output, err := session.CombinedOutput(cmd)
	if err != nil {
		return fmt.Errorf("write snippet via ssh: %w (output: %s)", err, string(output))
	}

	slog.Info("uploaded cloud-init snippet to proxmox via SSH", "node", node, "storage", storage, "filename", filename)
	return nil
}

// PV-P9: Find the actual disk interface name by index.
func (p *Provider) findDiskByIndex(ctx context.Context, node, vmID string, diskKey int) (string, error) {
	body, err := p.doGet(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/config", url.PathEscape(node), url.PathEscape(vmID)))
	if err != nil {
		return "", err
	}
	var config map[string]interface{}
	var result struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	if err := json.Unmarshal(result.Data, &config); err != nil {
		return "", err
	}

	// Look for disk interfaces in order: scsi, virtio, ide, sata
	prefixes := []string{"scsi", "virtio", "ide", "sata"}
	idx := 0
	for _, prefix := range prefixes {
		for i := 0; i < 30; i++ {
			key := fmt.Sprintf("%s%d", prefix, i)
			if _, ok := config[key]; ok {
				if idx == diskKey {
					return key, nil
				}
				idx++
			}
		}
	}
	// Fallback to scsi{diskKey} if not found by index
	return fmt.Sprintf("scsi%d", diskKey), nil
}

// PV-P11: findVMIDByName searches across all cluster nodes.
func (p *Provider) findVMIDByName(ctx context.Context, name string) (int, string, error) {
	body, err := p.doGet(ctx, "/cluster/resources?type=vm")
	if err != nil {
		return 0, "", err
	}

	var result struct {
		Data []struct {
			VMID     int    `json:"vmid"`
			Name     string `json:"name"`
			Template int    `json:"template"`
			Node     string `json:"node"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, "", err
	}

	for _, vm := range result.Data {
		if vm.Name == name && vm.Template == 1 {
			return vm.VMID, vm.Node, nil
		}
	}
	return 0, "", fmt.Errorf("template %q not found", name)
}

func (p *Provider) nextVMID(ctx context.Context) (int, error) {
	body, err := p.doGet(ctx, "/cluster/nextid")
	if err != nil {
		return 0, err
	}

	var result struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	id, err := strconv.Atoi(result.Data)
	if err != nil {
		return 0, fmt.Errorf("parse next VMID %q: %w", result.Data, err)
	}
	return id, nil
}

func extractUPID(body []byte) (string, error) {
	var result struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode UPID: %w", err)
	}
	if result.Data == "" {
		return "", fmt.Errorf("empty UPID returned")
	}
	return result.Data, nil
}

// PV-P16: Parse node name from a Proxmox UPID string.
// Format: UPID:node:pid:pstart:starttime:type:id:user@realm:
func nodeFromUPID(upid string) string {
	parts := strings.Split(upid, ":")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

// sanitiseSnippetFilename strips any characters that are not alphanumeric,
// hyphens, underscores, or dots. Prevents shell injection when the filename
// is used in SSH commands, even though VMName is validated upstream.
var safeFilenameRe = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

func sanitiseSnippetFilename(name string) string {
	safe := safeFilenameRe.ReplaceAllString(name, "")
	// Prevent path traversal
	safe = strings.ReplaceAll(safe, "..", "")
	if safe == "" || safe == "." {
		return ""
	}
	return safe
}

// redactConfigKeys returns a URL-encoded config string with sensitive values redacted.
func redactConfigKeys(data url.Values) string {
	redacted := make(url.Values)
	for k, v := range data {
		switch k {
		case "cipassword", "sshkeys":
			redacted[k] = []string{"[REDACTED]"}
		default:
			redacted[k] = v
		}
	}
	return redacted.Encode()
}

// netmaskToCIDR converts a dotted-decimal netmask to CIDR prefix length.
func netmaskToCIDR(mask string) string {
	parts := strings.Split(mask, ".")
	if len(parts) != 4 {
		return "24"
	}
	bits := 0
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return "24"
		}
		for n > 0 {
			bits += n & 1
			n >>= 1
		}
	}
	return strconv.Itoa(bits)
}
