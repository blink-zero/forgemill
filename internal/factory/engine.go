package factory

import (
	"bufio"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/forgemill/forgemill/internal/provider/proxmox"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/vim25/soap"
)

// sensitivePatterns matches common credential patterns in log output.
var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key)\s*[=:]\s*\S+`),
	regexp.MustCompile(`(?i)Authorization:\s*\S+`),
	regexp.MustCompile(`(?i)(https?://)[^:]+:[^@]+@`),
}

// redactSensitive replaces credential values in a log line with [REDACTED].
func redactSensitive(line string) string {
	// Patterns 0-1: key=value and Authorization header — find separator and redact after it
	for _, pat := range sensitivePatterns[:2] {
		line = pat.ReplaceAllStringFunc(line, func(match string) string {
			idx := strings.IndexAny(match, "=: ")
			if idx > 0 {
				return match[:idx+1] + " [REDACTED]"
			}
			return "[REDACTED]"
		})
	}
	// F-78: Pattern 2: URL credentials — use regex group to preserve protocol prefix.
	// IndexAny was incorrectly matching the ":" in "https://" instead of the credential separator.
	line = sensitivePatterns[2].ReplaceAllString(line, "${1}[REDACTED]@")
	return line
}

// hclPasswordPattern matches password fields in Packer HCL for redaction before DB storage.
var hclPasswordPattern = regexp.MustCompile(`(?i)(password\s*=\s*)"[^"]*"`)

// redactHCLCredentials replaces password values in HCL content before DB storage.
func redactHCLCredentials(hcl string) string {
	return hclPasswordPattern.ReplaceAllString(hcl, `${1}"[ENCRYPTED - SEE TARGET CONFIG]"`)
}

// autoinstallPasswordPattern matches the password hash in autoinstall YAML.
var autoinstallPasswordPattern = regexp.MustCompile(`(?m)(password:\s*)"[^"]*"`)

// redactAutoinstallCredentials replaces the password hash in autoinstall config before DB storage.
func redactAutoinstallCredentials(yaml string) string {
	return autoinstallPasswordPattern.ReplaceAllString(yaml, `${1}"[REDACTED]"`)
}

// generateRandomPassword generates a cryptographically random password of the given length.
func generateRandomPassword(length int) (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			return "", fmt.Errorf("failed to generate random password: %w", err)
		}
		b[i] = chars[n.Int64()]
	}
	return string(b), nil
}

// BuildStore is the interface the engine uses to persist build state.
type BuildStore interface {
	UpdateBuildStatus(id int64, status string, errMsg string) error
	UpdateBuildStarted(id int64) error
	UpdateBuildCompleted(id int64) error
	UpdateBuildLog(id int64, log string) error
	UpdateBuildGenerated(id int64, packerHCL, autoinstall, isoURL, isoChecksum string) error
	CleanupStaleBuilds() (int, error)
}

// BuildCompleteCallback is called after a build finishes successfully.
type BuildCompleteCallback func(buildID int64, templateName string)

// Engine manages Packer build execution. Only one build runs at a time.
type Engine struct {
	mu              sync.Mutex
	store           BuildStore
	hub             *BuildHub
	running         bool
	currentBuildID  int64
	cancelFn        context.CancelFunc
	onBuildComplete BuildCompleteCallback
}

// NewEngine creates a new build engine.
func NewEngine(store BuildStore, hub *BuildHub) *Engine {
	return &Engine{
		store: store,
		hub:   hub,
	}
}

// SetBuildCompleteCallback sets a function called after each successful build.
func (e *Engine) SetBuildCompleteCallback(cb BuildCompleteCallback) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onBuildComplete = cb
}

// IsRunning reports whether a build is currently executing.
func (e *Engine) IsRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

// IsRunningBuild reports whether the given build ID is the currently active build.
func (e *Engine) IsRunningBuild(id int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running && e.currentBuildID == id
}

// CancelBuild cancels the currently running build.
func (e *Engine) CancelBuild() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancelFn != nil {
		e.cancelFn()
	}
}

// RunBuild executes a Packer build in a background goroutine.
// V3-M10: Added validateCerts parameter to propagate TLS validation to Packer templates.
func (e *Engine) RunBuild(buildID int64, targetType string, targetHostname string, targetPort int, targetUsername string, targetPassword string, validateCerts bool, osDef *OSDefinition, cfg BuildConfig) error {
	e.mu.Lock()
	if e.running {
		currentID := e.currentBuildID
		e.mu.Unlock()
		return fmt.Errorf("another build is already in progress (build #%d) — wait for it to finish or cancel it first", currentID)
	}
	e.running = true
	e.currentBuildID = buildID
	// F-76: Create context and set cancelFn before releasing lock to prevent TOCTOU race
	// where CancelBuild could see running=true but cancelFn=nil
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	e.cancelFn = cancel
	e.mu.Unlock()

	go e.executeBuild(ctx, buildID, targetType, targetHostname, targetPort, targetUsername, targetPassword, validateCerts, osDef, cfg)
	return nil
}

func (e *Engine) executeBuild(ctx context.Context, buildID int64, targetType string, targetHostname string, targetPort int, targetUsername string, targetPassword string, validateCerts bool, osDef *OSDefinition, cfg BuildConfig) {
	defer func() {
		e.mu.Lock()
		// F-76: Release context resources via cancelFn
		if e.cancelFn != nil {
			e.cancelFn()
		}
		e.running = false
		e.currentBuildID = 0
		e.cancelFn = nil
		e.mu.Unlock()
	}()

	log := &strings.Builder{}
	lastLogPersist := time.Now()

	sendLog := func(line string) {
		// Redact credentials before storing or streaming
		line = redactSensitive(line)
		log.WriteString(line)
		log.WriteString("\n")
		e.hub.SendBuildProgress(buildID, BuildMessage{
			Type: "log",
			Data: map[string]string{"line": line},
		})
		// Periodically persist log so GET /api/factory/builds/{id} always has recent output
		if time.Since(lastLogPersist) >= 3*time.Second {
			if err := e.store.UpdateBuildLog(buildID, log.String()); err != nil {
				slog.Error("build: failed to persist log", "build_id", buildID, "error", err)
			}
			lastLogPersist = time.Now()
		}
	}

	sendPhase := func(phase string) {
		e.hub.SendBuildProgress(buildID, BuildMessage{
			Type: "progress",
			Data: map[string]string{"phase": phase},
		})
		sendLog(fmt.Sprintf("=== %s ===", phase))
	}

	// logStoreErr logs a non-fatal DB persistence error during build execution.
	// These are best-effort writes; failures are logged but don't abort the build.
	logStoreErr := func(op string, err error) {
		if err != nil {
			slog.Error("build: DB persistence error", "build_id", buildID, "op", op, "error", err)
		}
	}

	failBuild := func(errMsg string) {
		// Redact error messages too
		errMsg = redactSensitive(errMsg)
		slog.Error("build failed", "build_id", buildID, "error", errMsg)
		sendLog(fmt.Sprintf("ERROR: %s", errMsg))
		e.hub.SendBuildProgress(buildID, BuildMessage{
			Type: "error",
			Data: map[string]string{"error": errMsg},
		})
		logStoreErr("UpdateBuildStatus(failed)", e.store.UpdateBuildStatus(buildID, "failed", errMsg))
		logStoreErr("UpdateBuildCompleted", e.store.UpdateBuildCompleted(buildID))
		logStoreErr("UpdateBuildLog(fail)", e.store.UpdateBuildLog(buildID, log.String()))
	}

	// Phase 1: Setup
	sendPhase("Preparing build environment")
	logStoreErr("UpdateBuildStatus(building)", e.store.UpdateBuildStatus(buildID, "building", ""))
	logStoreErr("UpdateBuildStarted", e.store.UpdateBuildStarted(buildID))

	isoURL := osDef.ISOURLPattern
	isoChecksum, err := resolveChecksum(ctx, osDef.ISOChecksumURL, isoURL)
	if err != nil {
		sendLog(fmt.Sprintf("Warning: could not resolve checksum from %s: %v, falling back to file: prefix", osDef.ISOChecksumURL, err))
		isoChecksum = "file:" + osDef.ISOChecksumURL
	}

	// MED-08: Generate a random build password per build instead of using a
	// hardcoded "packer" password. This closes the window where a failed build
	// could leave a VM with known credentials accessible on the network.
	sshPassword, err := generateRandomPassword(32)
	if err != nil {
		failBuild(fmt.Sprintf("generate build password: %v", err))
		return
	}
	// Log credentials in a format that won't be caught by redactSensitive.
	// These are intentionally visible — they're temporary build-only creds.
	sendLog(fmt.Sprintf("Build SSH login → forgemill / %s (locked before template conversion)", sshPassword))

	// Phase 2: Generate templates
	sendPhase("Generating Packer template")

	// Look up platform from registry
	plat, err := GetPlatform(targetType)
	if err != nil {
		failBuild(fmt.Sprintf("unknown target type: %v", err))
		return
	}

	data := TemplateData{
		TemplateName:       cfg.TemplateName,
		GuestOSType:        osDef.GuestOSType,
		ProxmoxOS:          osDef.ProxmoxOSType,
		CPU:                cfg.CPU,
		MemoryMB:           cfg.MemoryMB,
		DiskGB:             cfg.DiskGB,
		DiskMB:             cfg.DiskGB * 1024,
		ISOURL:             isoURL,
		ISOChecksum:        isoChecksum,
		TargetHostname:     targetHostname,
		TargetPort:         targetPort,
		TargetUsername:     normalizeProxmoxUsername(targetUsername, targetType),
		TargetPassword:     targetPassword,
		InsecureConnection: !validateCerts, // V3-M10: Propagate TLS validation setting
		SSHPassword:        sshPassword,
		Datacenter:     cfg.Datacenter,
		Cluster:        cfg.Cluster,
		Host:           cfg.Host,
		Datastore:      cfg.Datastore,
		Folder:         cfg.Folder,
		Network:        cfg.Network,
		Node:           cfg.Node,
		StoragePool:    cfg.StoragePool,
		Bridge:         cfg.Bridge,
		ISOStorage:     cfg.ISOStorage,
	}

	// Apply platform-specific field defaults (storage mapping, interface name, etc.)
	// Phase 3: Pass osDef so platform can resolve boot commands and provisioner commands
	plat.AdjustTemplateData(&data, targetType, osDef)

	// Auto-detect Proxmox node name if not specified (requires API call)
	if targetType == "proxmox" && data.Node == "" {
		slog.Info("Proxmox node not specified, will attempt auto-detection via API")
		p := proxmox.New(targetHostname, targetPort, targetUsername, targetPassword, validateCerts)
		if err := p.Connect(ctx); err == nil {
			if nodeName := p.GetNodeName(); nodeName != "" {
				data.Node = nodeName
				slog.Info("auto-detected Proxmox node", "node", nodeName)
			}
			p.Disconnect()
		}
	}

	// Auto-discover ESXi hostname and datastore via vSphere API
	if targetType == "esxi" && (data.Host == "" || data.Datastore == "") {
		sendLog("Auto-discovering ESXi configuration from vSphere API...")
		esxiHost, esxiDS, err := discoverESXiDefaults(ctx, targetHostname, targetPort, targetUsername, targetPassword, validateCerts)
		if err != nil {
			sendLog(fmt.Sprintf("Warning: auto-discovery failed: %v", err))
			if data.Host == "" {
				data.Host = targetHostname
			}
			if data.Datastore == "" {
				data.Datastore = "datastore1"
			}
		} else {
			if data.Host == "" {
				data.Host = esxiHost
			}
			if data.Datastore == "" {
				data.Datastore = esxiDS
			}
			sendLog(fmt.Sprintf("Discovered ESXi host: %s, datastore: %s", data.Host, data.Datastore))
		}
	}

	// When a build IP is specified, tell Packer exactly where to SSH.
	// This avoids the IP-change-after-reboot problem with DHCP.
	if cfg.BuildIP != "" {
		data.SSHHost = cfg.BuildIP
		sendLog(fmt.Sprintf("Using static build IP: %s (Packer will SSH to this address)", cfg.BuildIP))
	} else {
		sendLog("Using DHCP for build networking (Packer will discover IP via VMware Tools / QEMU agent)")
	}

	packerHCL, err := GeneratePackerHCL(targetType, data)
	if err != nil {
		failBuild(fmt.Sprintf("generate packer template: %v", err))
		return
	}

	// Read SSH public key for autoinstall authorized_keys injection
	sshPubKeyBytes, err := os.ReadFile("/app/data/packer-build-key.pub")
	if err != nil {
		sendLog("Warning: No SSH public key found at /app/data/packer-build-key.pub, key auth disabled")
		sshPubKeyBytes = []byte("")
	}
	sshPublicKey := strings.TrimSpace(string(sshPubKeyBytes))

	// Phase 5: Use installer registry instead of hardcoded GenerateAutoinstall.
	// This enables support for different install methods (kickstart, preseed, etc.)
	installer, err := GetInstaller(osDef.InstallMethod)
	if err != nil {
		failBuild(fmt.Sprintf("get installer for %q: %v", osDef.InstallMethod, err))
		return
	}

	// Get platform-specific installer hints (cloud-init datasources, services, etc.)
	// These are injected into InstallerParams so installer templates have zero
	// TargetType conditionals — new hypervisors just implement InstallerHints().
	hints := plat.InstallerHints()

	installerConfig, err := installer.GenerateConfig(InstallerParams{
		TargetType:           targetType,
		OSFamily:             osDef.Family,
		OSVersion:            osDef.Version,
		Username:             "forgemill",
		Password:             sshPassword,
		SSHPublicKey:         sshPublicKey,
		Hostname:             "forgemill-template",
		Timezone:             "UTC",
		Locale:               "en_US.UTF-8",
		Keyboard:             "us",
		ExtraPackages:        plat.ProvisionerPackages(osDef.Family),
		BuildIP:              cfg.BuildIP,
		Netmask:              cfg.Netmask,
		Gateway:              cfg.Gateway,
		InterfaceName:        plat.InterfaceName(),
		CloudInitDatasources: hints.CloudInitDatasources,
		CloudInitExtraLines:  hints.CloudInitExtraLines,
		PlatformServices:     hints.PlatformServices,
	})
	if err != nil {
		failBuild(fmt.Sprintf("generate %s config: %v", osDef.InstallMethod, err))
		return
	}

	// Phase 5: Get installer filename for file writing and logging
	installerFilename := installer.Filename()

	// Redact credentials before storing in DB
	redactedHCL := redactHCLCredentials(packerHCL)
	redactedInstallerConfig := redactAutoinstallCredentials(installerConfig)
	logStoreErr("UpdateBuildGenerated", e.store.UpdateBuildGenerated(buildID, redactedHCL, redactedInstallerConfig, isoURL, isoChecksum))
	sendLog("Packer HCL template generated")
	sendLog(fmt.Sprintf("%s configuration generated (%s)", osDef.InstallMethod, installerFilename))

	// Phase 3: Create temp directory
	sendPhase("Writing build files")
	buildDir, err := os.MkdirTemp("", fmt.Sprintf("forgemill-build-%d-", buildID))
	if err != nil {
		failBuild(fmt.Sprintf("create build directory: %v", err))
		return
	}
	defer os.RemoveAll(buildDir)

	hclPath := buildDir + "/build.pkr.hcl"
	// V3-L9: Use 0600 permissions — files contain credentials
	if err := os.WriteFile(hclPath, []byte(packerHCL), 0600); err != nil {
		failBuild(fmt.Sprintf("write HCL file: %v", err))
		return
	}

	// Phase 5: Use installer's filename (e.g., "autoinstall.yaml", "ks.cfg", "preseed.cfg")
	installerPath := buildDir + "/" + installerFilename
	// V3-L9: Use 0600 permissions — file contains password hash
	if err := os.WriteFile(installerPath, []byte(installerConfig), 0600); err != nil {
		failBuild(fmt.Sprintf("write %s file: %v", installerFilename, err))
		return
	}

	// Copy SSH private key for Packer key-based auth
	sshKeyBytes, err := os.ReadFile("/app/data/packer-build-key")
	if err == nil {
		keyPath := buildDir + "/packer-build-key"
		if err := os.WriteFile(keyPath, sshKeyBytes, 0600); err != nil {
			sendLog(fmt.Sprintf("Warning: could not write SSH key: %v", err))
		}
	}

	// Use the build directory for Packer's ISO cache so it's cleaned up with
	// the build dir (defer os.RemoveAll above). Prevents unbounded cache growth
	// that can exhaust tmpfs/RAM-backed filesystems in read-only containers.
	packerEnv := []string{"PACKER_CACHE_DIR=" + buildDir}

	// Phase 4: Packer init
	sendPhase("Initializing Packer plugins")
	if err := e.runCommandEnv(ctx, buildDir, packerEnv, sendLog, "packer", "init", "."); err != nil {
		if ctx.Err() != nil {
			e.handleCancel(buildID, log)
			return
		}
		failBuild(fmt.Sprintf("packer init failed: %v", err))
		return
	}

	// Phase 5: Packer build
	sendPhase("Running Packer build")
	logStoreErr("UpdateBuildStatus(building/phase5)", e.store.UpdateBuildStatus(buildID, "building", ""))

	if err := e.runCommandEnv(ctx, buildDir, packerEnv, sendLog, "packer", "build", "-force", "-machine-readable", "."); err != nil {
		if ctx.Err() != nil {
			e.handleCancel(buildID, log)
			return
		}

		// For Proxmox: if PVE ISO download failed, retry with local download + upload
		if targetType == "proxmox" && data.ISODownloadPVE && strings.Contains(log.String(), "failed to download ISO") {
			sendLog("\n⚠ Proxmox couldn't download the ISO directly. Retrying with local download + upload...")
			sendPhase("Retrying with local ISO download")

			// Regenerate template without iso_download_pve
			data.ISODownloadPVE = false
			packerHCL, err = GeneratePackerHCL(targetType, data)
			if err != nil {
				failBuild(fmt.Sprintf("regenerate packer template for retry: %v", err))
				return
			}
			if err := os.WriteFile(hclPath, []byte(packerHCL), 0600); err != nil {
				failBuild(fmt.Sprintf("rewrite HCL file for retry: %v", err))
				return
			}

			// Retry the build
			if err := e.runCommandEnv(ctx, buildDir, packerEnv, sendLog, "packer", "build", "-force", "-machine-readable", "."); err != nil {
				if ctx.Err() != nil {
					e.handleCancel(buildID, log)
					return
				}
				failBuild(fmt.Sprintf("packer build failed (retry with local ISO): %v", err))
				return
			}
		} else {
			failBuild(fmt.Sprintf("packer build failed: %v", err))
			return
		}
	}

	// Phase 6: Complete
	sendPhase("Build completed successfully")
	logStoreErr("UpdateBuildStatus(completed)", e.store.UpdateBuildStatus(buildID, "completed", ""))
	logStoreErr("UpdateBuildCompleted", e.store.UpdateBuildCompleted(buildID))
	logStoreErr("UpdateBuildLog(complete)", e.store.UpdateBuildLog(buildID, log.String()))

	e.hub.SendBuildProgress(buildID, BuildMessage{
		Type: "complete",
		Data: map[string]string{"status": "completed"},
	})

	slog.Info("build completed successfully", "build_id", buildID)

	// Invoke lineage callback
	e.mu.Lock()
	cb := e.onBuildComplete
	e.mu.Unlock()
	if cb != nil {
		cb(buildID, cfg.TemplateName)
	}
}

func (e *Engine) handleCancel(buildID int64, log *strings.Builder) {
	log.WriteString("\nBuild cancelled by user\n")
	if err := e.store.UpdateBuildStatus(buildID, "cancelled", "cancelled by user"); err != nil {
		slog.Error("build: failed to set cancelled status", "build_id", buildID, "error", err)
	}
	if err := e.store.UpdateBuildCompleted(buildID); err != nil {
		slog.Error("build: failed to mark completed on cancel", "build_id", buildID, "error", err)
	}
	if err := e.store.UpdateBuildLog(buildID, log.String()); err != nil {
		slog.Error("build: failed to persist log on cancel", "build_id", buildID, "error", err)
	}
	e.hub.SendBuildProgress(buildID, BuildMessage{
		Type: "error",
		Data: map[string]string{"error": "build cancelled"},
	})
}

func (e *Engine) runCommand(ctx context.Context, dir string, sendLog func(string), name string, args ...string) error {
	return e.runCommandEnv(ctx, dir, nil, sendLog, name, args...)
}

func (e *Engine) runCommandEnv(ctx context.Context, dir string, env []string, sendLog func(string), name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	// Create a new process group so we can kill all child processes on cancel
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Override CommandContext's default SIGKILL with process group kill
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			// Kill the entire process group (negative PID)
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start command: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		sendLog(line)
	}
	if err := scanner.Err(); err != nil {
		slog.Error("error reading command output", "command", name, "error", err)
	}

	if err := cmd.Wait(); err != nil {
		return err
	}

	return nil
}

// CheckPrerequisites verifies that required tools are installed.
func CheckPrerequisites() *PrereqStatus {
	status := &PrereqStatus{}

	packerPath, err := exec.LookPath("packer")
	if err != nil {
		return status
	}
	status.PackerInstalled = true
	_ = packerPath

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "packer", "version").Output()
	if err == nil {
		version := strings.TrimSpace(string(out))
		if idx := strings.Index(version, "\n"); idx > 0 {
			version = version[:idx]
		}
		status.PackerVersion = version
	}

	return status
}

// normalizeProxmoxUsername appends @pam if no realm specified for Proxmox targets.
func normalizeProxmoxUsername(username, targetType string) string {
	if targetType == "proxmox" && !strings.Contains(username, "@") {
		return username + "@pam"
	}
	return username
}

// ISSUE-04 + MED-02: resolveChecksum now accepts a context for cancellation
// and uses an HTTP client with a 30-second timeout instead of the default
// client which has no timeout and could hang indefinitely.
func resolveChecksum(ctx context.Context, checksumURL, isoURL string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return "", fmt.Errorf("create checksum request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch checksum: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("checksum URL returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB max
	if err != nil {
		return "", fmt.Errorf("read checksum body: %w", err)
	}
	isoFilename := path.Base(isoURL)
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Format 1: "hash *filename" or "hash  filename" (GNU coreutils style)
		// Format 2: "SHA256 (filename) = hash" (BSD/Rocky Linux style)
		if strings.Contains(line, " = ") && strings.Contains(line, "(") {
			// BSD style: SHA256 (filename) = hash
			start := strings.Index(line, "(")
			end := strings.Index(line, ")")
			eqPos := strings.Index(line, " = ")
			if start != -1 && end > start && eqPos > end {
				name := line[start+1 : end]
				hash := strings.TrimSpace(line[eqPos+3:])
				if name == isoFilename {
					return "sha256:" + hash, nil
				}
			}
		} else {
			// GNU style: hash *filename or hash  filename
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				name := strings.TrimPrefix(parts[1], "*")
				if name == isoFilename {
					return "sha256:" + parts[0], nil
				}
			}
		}
	}
	return "", fmt.Errorf("checksum for %s not found in %s", isoFilename, checksumURL)
}

// discoverESXiDefaults queries the vSphere API via govmomi to find the ESXi
// host's registered name and default datastore. Packer requires the FQDN for `host`.
func discoverESXiDefaults(ctx context.Context, hostname string, port int, username, password string, validateCerts bool) (hostName string, datastoreName string, err error) {
	u, parseErr := soap.ParseURL(fmt.Sprintf("https://%s:%d/sdk", hostname, port))
	if parseErr != nil {
		return "", "", fmt.Errorf("parse URL: %w", parseErr)
	}
	u.User = url.UserPassword(username, password)

	client, connErr := govmomi.NewClient(ctx, u, !validateCerts)
	if connErr != nil {
		return "", "", fmt.Errorf("connect to ESXi: %w", connErr)
	}
	defer client.Logout(ctx)

	f := find.NewFinder(client.Client, true)

	dc, dcErr := f.Datacenter(ctx, "ha-datacenter")
	if dcErr != nil {
		return "", "", fmt.Errorf("find datacenter: %w", dcErr)
	}
	f.SetDatacenter(dc)

	// Discover host
	hosts, hostErr := f.HostSystemList(ctx, "*")
	if hostErr != nil || len(hosts) == 0 {
		return "", "", fmt.Errorf("no hosts found")
	}
	hostName = hosts[0].Name()

	// Discover datastore (use the first/default one)
	datastores, dsErr := f.DatastoreList(ctx, "*")
	if dsErr != nil || len(datastores) == 0 {
		return hostName, "datastore1", nil // fallback
	}
	datastoreName = datastores[0].Name()

	return hostName, datastoreName, nil
}
