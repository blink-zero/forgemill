// Package proxmox implements the Proxmox VE provider.
package proxmox

import (
	"sort"
	"errors"
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

	"github.com/forgemill/forgemill/internal/clock"
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
			VLANTagging:      true,
			NICAttach:        true,
			DiskAttach:       true,
		},
		DeployFields: []provider.DeployField{
			{Key: "datastore", Label: "Storage", Resource: "datastores"},
			{Key: "network", Label: "Bridge", Resource: "networks"},
		},
		NICAdapterTypes: nicAdapterTypes,
	})
}

// PV-P2: Regex for Proxmox API token format: user@realm!tokenid=uuid-secret
var apiTokenRe = regexp.MustCompile(`^.+@.+!.+=.+$`)

// Provider implements the provider.Provider interface for Proxmox VE.
// TargetHostKeyStore is the TOFU fingerprint store; the definition lives in
// the provider package so the service can wire it without importing this one.
type TargetHostKeyStore = provider.TargetHostKeyStore

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

	// nodeCache maps vmid -> node for the lifetime of one Connect/Disconnect
	// cycle, so a sync over N VMs costs one /cluster/resources listing
	// instead of one per operation (see nodeFor).
	nodeMu    sync.Mutex
	nodeCache map[string]string
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

// Provider opts in to TOFU wiring by the service (see provider.HostKeyTrusting).
var _ provider.HostKeyTrusting = (*Provider)(nil)

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
	p.nodeMu.Lock()
	p.nodeCache = map[string]string{}
	p.nodeMu.Unlock()

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
	p.nodeMu.Lock()
	p.nodeCache = nil
	p.nodeMu.Unlock()
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
	node := p.nodeFor(ctx, id)

	cfg, err := p.getVMConfig(ctx, node, id)
	if err != nil {
		return nil, err
	}
	name := cfg.Str("name")
	ostype := cfg.Str("ostype")
	cpus := cfg.Int("cores")
	if sockets := cfg.Int("sockets"); sockets > 0 {
		cpus *= sockets
	}

	return &provider.Template{
		ID:       id,
		Name:     name,
		Moref:    id,
		CPU:      cpus,
		MemoryMB: cfg.Int("memory"),
		OSType:   osTypeFromOSType(ostype),
		GuestID:  ostype,
	}, nil
}

func (p *Provider) GetTemplateDetail(ctx context.Context, id string) (*provider.TemplateDetail, error) {
	node := p.nodeFor(ctx, id)

	data, err := p.getVMConfig(ctx, node, id)
	if err != nil {
		return nil, err
	}

	// Basic template fields
	name := data.Str("name")
	cores := data.Int("cores")
	sockets := data.Int("sockets")
	if sockets > 0 {
		cores *= sockets
	}
	memory := data.Int("memory")
	ostype := data.Str("ostype")

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
	if cpuType := data.Str("cpu"); cpuType != "" {
		detail.CPUType = cpuType
	}

	// SCSI controller type
	if scsihw := data.Str("scsihw"); scsihw != "" {
		detail.SCSIType = scsihw
	}

	// Cloud-init drive detection
	for i := 0; i < 4; i++ {
		for _, bus := range []string{"ide", "scsi", "sata"} {
			key := fmt.Sprintf("%s%d", bus, i)
			if val := data.Str(key); strings.Contains(val, "cloudinit") {
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
		if val := data.Str(diskKey); val != "" && !strings.Contains(val, "cloudinit") {
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
		if val := data.Str(key); val != "" {
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
	if bios := data.Str("bios"); bios != "" {
		detail.Firmware = bios
	} else {
		detail.Firmware = "seabios"
	}

	// Annotation from description
	detail.Annotation = data.Str("description")

	// Hardware version: QEMU + machine type
	if machine := data.Str("machine"); machine != "" {
		detail.HardwareVer = "QEMU " + machine
	} else {
		detail.HardwareVer = "QEMU"
	}

	// Tools status from agent field
	agentVal := data.Int("agent")
	if agentVal == 1 {
		detail.ToolsStatus = "installed"
	} else {
		detail.ToolsStatus = "not installed"
	}

	return detail, nil
}

// buildNet0Config builds the Proxmox net0 device config string for a
// cloned VM. vlanTag <= 0 means untagged (no "tag=" segment) — Proxmox
// interprets an untagged net0 as a regular access-port NIC on the bridge.
func buildNet0Config(bridge string, vlanTag int) string {
	return buildNetConfig("virtio", bridge, vlanTag, false)
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
		configData.Set("net0", buildNet0Config(spec.Network, spec.VLANTag))
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

	// PV-P4: hardware first, cloud-init second — two PUTs. A failure in either
	// fails the deployment with Proxmox's reason and removes the clone;
	// completing "successfully" with the template's CPU/memory (or without
	// credentials) is never acceptable.
	hardwareCfg, cloudInitCfg := splitPostCloneConfig(configData)
	if err := p.applyVMConfig(ctx, vmNode, newIDStr, "hardware", hardwareCfg); err != nil {
		return nil, fmt.Errorf("configure VM %d after clone: %w; %s", newID, err, p.rollbackClone(ctx, newIDStr))
	}
	if err := p.applyVMConfig(ctx, vmNode, newIDStr, "cloud-init", cloudInitCfg); err != nil {
		return nil, fmt.Errorf("configure VM %d after clone: %w; %s", newID, err, p.rollbackClone(ctx, newIDStr))
	}

	// Resize disk if requested size differs from template. A failure here
	// means the VM exists with the template's disk — reported as a partial
	// deploy (the service keeps the VM and fails the deployment) rather than
	// logged and forgotten.
	if spec.DiskGB > 0 {
		disk, err := p.findDiskByIndex(ctx, vmNode, newIDStr, 0)
		if err != nil {
			return nil, &provider.PartialDeployError{VMID: newIDStr, Err: fmt.Errorf("resize disk to %d GB: find disk: %w", spec.DiskGB, err)}
		}
		resizeData := url.Values{
			"disk": {disk},
			"size": {fmt.Sprintf("%dG", spec.DiskGB)},
		}
		if err := p.doPut(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/resize", url.PathEscape(vmNode), newIDStr), resizeData); err != nil {
			return nil, &provider.PartialDeployError{VMID: newIDStr, Err: fmt.Errorf("resize disk %s to %d GB: %w", disk, spec.DiskGB, err)}
		}
		provider.Infof(ctx, "Resized disk after clone", "vmid", newID, "disk", disk, "size_gb", spec.DiskGB)
	}

	// PV-P6: Start VM after clone. A VM that does not start never runs
	// cloud-init, so this too is a partial deploy, not a warning.
	startBody, err := p.doPost(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/status/start", url.PathEscape(vmNode), newIDStr), nil)
	if err != nil {
		return nil, &provider.PartialDeployError{VMID: newIDStr, Err: fmt.Errorf("start VM: %w", err)}
	}
	if startUPID, err := extractUPID(startBody); err == nil {
		if err := p.awaitTask(ctx, startUPID, 60); err != nil {
			return nil, &provider.PartialDeployError{VMID: newIDStr, Err: fmt.Errorf("start VM: %w", err)}
		}
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
	node := p.nodeFor(ctx, vmID)

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
// hardStop is `qm stop`: an immediate power-off with no guest involvement,
// waited to completion. Used by DeleteVM; the user-facing PowerOff keeps its
// graceful ACPI attempt.
func (p *Provider) hardStop(ctx context.Context, node, vmID string) error {
	body, err := p.doPost(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/status/stop", url.PathEscape(node), url.PathEscape(vmID)), nil)
	if err != nil {
		return err
	}
	if upid, err := extractUPID(body); err == nil {
		return p.awaitTask(ctx, upid, 60)
	}
	return nil
}

func (p *Provider) PowerOff(ctx context.Context, vmID string) error {
	node := p.nodeFor(ctx, vmID)
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
	node := p.nodeFor(ctx, vmID)
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
	node := p.nodeFor(ctx, vmID)

	// A VM being destroyed is hard-stopped, not shut down gracefully: the
	// disk is about to be deleted so there is nothing for the guest to
	// preserve, and the ACPI path (90 s guest timeout, then a poll) is what
	// pushed destroys past API clients' timeouts (#207). This mirrors the
	// vSphere provider, which powers off and destroys. Best effort — if the
	// stop fails Proxmox will refuse the delete below and say why.
	if status, err := p.GetVMStatus(ctx, vmID); err == nil && status.PowerState != "poweredOff" {
		if err := p.hardStop(ctx, node, vmID); err != nil {
			provider.Warnf(ctx, "Destroy: hard stop failed, attempting delete anyway", "vmid", vmID, "error", err)
		}
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
		if err := clock.Sleep(ctx, time.Second); err != nil {
			return err
		}
	}
	return fmt.Errorf("task %s did not complete within %ds", upid, timeoutSec)
}

// PV-X2: Normalize power state to canonical values.
// PV-P10: Attempt guest agent IP retrieval.
func (p *Provider) GetVMStatus(ctx context.Context, vmID string) (*provider.VMStatus, error) {
	node := p.nodeFor(ctx, vmID)

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

	// PV-P10: Try guest agent for IP address and the real OS name
	if powerState == "running" {
		if ip := p.getGuestAgentIP(ctx, node, vmID); ip != "" {
			status.IPAddress = ip
		}
		status.GuestOS = p.getGuestAgentOS(ctx, node, vmID)
	}

	return status, nil
}

// getGuestAgentOS asks the QEMU guest agent what the guest is running
// ("Ubuntu 22.04.4 LTS"). Empty when there is no agent or it doesn't know.
func (p *Provider) getGuestAgentOS(ctx context.Context, node, vmID string) string {
	body, err := p.doGet(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/agent/get-osinfo", url.PathEscape(node), url.PathEscape(vmID)))
	if err != nil {
		return ""
	}
	var result struct {
		Data struct {
			Result struct {
				ID         string `json:"id"`
				Name       string `json:"name"`
				PrettyName string `json:"pretty-name"`
				VersionID  string `json:"version-id"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return ""
	}
	r := result.Data.Result
	if r.PrettyName != "" {
		return r.PrettyName
	}
	if r.Name != "" {
		return strings.TrimSpace(r.Name + " " + r.VersionID)
	}
	return r.ID
}

// ValidateDeploySpec is a no-op for Proxmox: unlike vCenter/ESXi, DeployVM
// passes Network/Datastore straight through as literal API field values
// (bridge=X, storage=Y) rather than resolving them via an inventory-path
// finder, so GetResources' Name-based list is already exactly what the
// real deploy uses — no separate resolver to fall out of sync with.
func (p *Provider) ValidateDeploySpec(ctx context.Context, spec *provider.DeploySpec) []error {
	return nil
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
	node := p.nodeFor(ctx, vmID)
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
	node := p.nodeFor(ctx, vmID)

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
	node := p.nodeFor(ctx, vmID)

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
	node := p.nodeFor(ctx, vmID)

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
	node := p.nodeFor(ctx, vmID)

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
	node := p.nodeFor(ctx, vmID)

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
	node := p.nodeFor(ctx, vmID)
	config, err := p.getVMConfig(ctx, node, vmID)
	if err != nil {
		return nil, err
	}
	return enumerateDisks(config), nil
}

func (p *Provider) ExpandDisk(ctx context.Context, vmID string, diskKey int, newSizeGB int) error {
	node := p.nodeFor(ctx, vmID)

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
	node := p.nodeFor(ctx, vmID)
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
			MaxDisk  int64  `json:"maxdisk"`
			Status   string `json:"status"`
			Node     string `json:"node"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode VMs: %w", err)
	}

	vms := []provider.VMInfo{}
	p.nodeMu.Lock()
	if p.nodeCache == nil {
		p.nodeCache = map[string]string{}
	}
	for _, vm := range result.Data {
		// The listing already tells us every VM's node — remember it so the
		// per-VM calls that follow a sync don't each re-fetch the cluster.
		p.nodeCache[strconv.Itoa(vm.VMID)] = vm.Node
		if vm.Template == 1 {
			continue
		}
		vms = append(vms, provider.VMInfo{
			ID:         strconv.Itoa(vm.VMID),
			Name:       vm.Name,
			PowerState: provider.NormalizePowerState(vm.Status),
			CPU:        vm.CPUs,
			MemoryMB:   int(vm.MaxMem / 1024 / 1024),
			DiskGB:     int(vm.MaxDisk / 1024 / 1024 / 1024),
			// GetVMStatus reports "linux" for every Proxmox guest (the API
			// exposes no guest id); mirror that so a sync fed from the
			// listing writes the same OS type it always has.
			GuestID: "linux",
			Host:    vm.Node,
		})
	}
	p.nodeMu.Unlock()
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
		return nil, newAPIError(statusCode, body)
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
		return nil, newAPIError(statusCode, body)
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
		return newAPIError(statusCode, body)
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

// nodeFor returns the node hosting vmID, consulting /cluster/resources at
// most once per unknown VM per connection. When the lookup fails it falls
// back to the connected node exactly as every caller did before, but says
// so — on a multi-node cluster that fallback sends the request to the
// wrong node and the real cause used to be discarded.
func (p *Provider) nodeFor(ctx context.Context, vmID string) string {
	p.nodeMu.Lock()
	if n, ok := p.nodeCache[vmID]; ok {
		p.nodeMu.Unlock()
		return n
	}
	p.nodeMu.Unlock()

	node, err := p.resolveVMNode(ctx, vmID)
	if err != nil {
		provider.Warnf(ctx, "Could not resolve the VM's node, using the connected node", "vmid", vmID, "node", p.node, "error", err)
		return p.node
	}
	return node
}

// PV-P15: Resolve which node a VM is running on via /cluster/resources.
// Every VM in the listing is cached, so a sync over the whole target pays
// for the listing once.
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
	found := ""
	p.nodeMu.Lock()
	if p.nodeCache == nil {
		p.nodeCache = map[string]string{}
	}
	for _, vm := range result.Data {
		p.nodeCache[strconv.Itoa(vm.VMID)] = vm.Node
		if vm.VMID == vmIDInt {
			found = vm.Node
		}
	}
	p.nodeMu.Unlock()
	if found == "" {
		return "", fmt.Errorf("VM %s not found in cluster", vmID)
	}
	return found, nil
}

// PV-P13: Detect OS type from Proxmox VM config.
func (p *Provider) detectOSType(ctx context.Context, node, vmID string) string {
	cfg, err := p.getVMConfig(ctx, node, vmID)
	if err != nil {
		return "linux"
	}
	return osTypeFromOSType(cfg.Str("ostype"))
}

// osTypeFromOSType maps a Proxmox ostype value (l26, win11, w2k22, ...) to
// Forgemill's coarse "linux" / "windows". Shared by every reader so the
// rule can't drift between them.
func osTypeFromOSType(ostype string) string {
	if strings.HasPrefix(ostype, "win") || strings.HasPrefix(ostype, "w") {
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
	config, err := p.getVMConfig(ctx, node, vmID)
	if err != nil {
		return "", err
	}
	for _, d := range enumerateDisks(config) {
		if d.Key == diskKey {
			return d.Label, nil
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

// apiError is a non-2xx answer from the Proxmox API with the reason Proxmox
// gave (its "message" plus any per-parameter "errors"), so a rejected
// request can be explained to the user instead of just "HTTP 400".
type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("proxmox request failed (HTTP %d)", e.Status)
	}
	return fmt.Sprintf("proxmox request failed (HTTP %d): %s", e.Status, e.Message)
}

// newAPIError extracts Proxmox's explanation from an error body of the form
// {"message":"Parameter verification failed.\n","errors":{"sshkeys":"invalid format"}}.
func newAPIError(status int, body []byte) error {
	var payload struct {
		Message string            `json:"message"`
		Errors  map[string]string `json:"errors"`
	}
	msg := ""
	if json.Unmarshal(body, &payload) == nil {
		msg = strings.TrimSpace(payload.Message)
		if len(payload.Errors) > 0 {
			keys := make([]string, 0, len(payload.Errors))
			for k := range payload.Errors {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			parts := make([]string, 0, len(keys))
			for _, k := range keys {
				parts = append(parts, k+": "+strings.TrimSpace(payload.Errors[k]))
			}
			if msg != "" {
				msg += " — "
			}
			msg += strings.Join(parts, "; ")
		}
	}
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	return &apiError{Status: status, Message: msg}
}

// isRetryableConfigError reports whether a failed config write is worth
// retrying: Proxmox still holding the clone lock (reported as 5xx and/or a
// "lock" message) can clear on its own; a 4xx parameter rejection cannot.
func isRetryableConfigError(err error) bool {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.Status >= 500 || strings.Contains(strings.ToLower(ae.Message), "lock")
	}
	return strings.Contains(strings.ToLower(err.Error()), "lock")
}

// hardwareConfigKeys are applied in their own PUT, before the cloud-init
// keys, so a rejected cloud-init value can no longer take the CPU/memory
// override down with it (and vice versa).
var hardwareConfigKeys = map[string]bool{"cores": true, "memory": true, "net0": true}

// splitPostCloneConfig divides the post-clone settings into the hardware
// PUT and the cloud-init PUT. Either may be empty.
func splitPostCloneConfig(all url.Values) (hardware, cloudInit url.Values) {
	hardware, cloudInit = url.Values{}, url.Values{}
	for k, v := range all {
		if hardwareConfigKeys[k] {
			hardware[k] = v
		} else {
			cloudInit[k] = v
		}
	}
	return hardware, cloudInit
}

// applyVMConfig writes one group of config keys to a freshly cloned VM,
// retrying while Proxmox reports the clone lock (up to ~30 s) and failing
// fast on anything it will never accept.
func (p *Provider) applyVMConfig(ctx context.Context, node, vmID, label string, data url.Values) error {
	if len(data) == 0 {
		return nil
	}
	path := fmt.Sprintf("/nodes/%s/qemu/%s/config", url.PathEscape(node), url.PathEscape(vmID))
	slog.Info("configuring VM after clone", "vmid", vmID, "node", node, "part", label, "config_keys", redactConfigKeys(data))
	const attempts = 10
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		err = p.doPut(ctx, path, data)
		if err == nil {
			return nil
		}
		if !isRetryableConfigError(err) || attempt == attempts {
			break
		}
		slog.Info("retrying VM config", "vmid", vmID, "part", label, "attempt", attempt+1, "error", err)
		if serr := clock.Sleep(ctx, 3*time.Second); serr != nil {
			return serr
		}
	}
	return fmt.Errorf("%s config: %w", label, err)
}

// rollbackClone removes a clone whose post-clone configuration failed, so a
// failed deployment doesn't leave a VM with the wrong shape (and no
// credentials) behind. Returns a human-readable note for the error message.
func (p *Provider) rollbackClone(ctx context.Context, vmID string) string {
	provider.Warnf(ctx, "Removing clone after failed post-clone configuration", "vmid", vmID)
	if err := p.DeleteVM(ctx, vmID); err != nil {
		provider.Errorf(ctx, "Rollback of the failed clone did not complete", "vmid", vmID, "error", err)
		return fmt.Sprintf("clone %s was left on the hypervisor (removal failed: %v)", vmID, err)
	}
	return fmt.Sprintf("clone %s has been removed", vmID)
}
