package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
)

const (
	maxConcurrentPerVM = 3
	maxConcurrentTotal = 10
	defaultTimeoutSecs = 600 // 10 minutes
	minTimeoutSecs     = 10
	maxTimeoutSecs     = 1800 // 30 minutes
)

// ExecutionHub is the interface for streaming execution output via WebSocket.
type ExecutionHub interface {
	SendOutput(executionID int64, msg any)
}

type ExecutorService struct {
	db        *db.DB
	targets   *TargetService
	encryptor Encryptor
	hub       ExecutionHub
	notifier  *NotificationService
	mu        sync.Mutex
	cancels   map[int64]context.CancelFunc
}

func NewExecutorService(db *db.DB, targets *TargetService, enc Encryptor, hub ExecutionHub) *ExecutorService {
	return &ExecutorService{
		db:        db,
		targets:   targets,
		encryptor: enc,
		hub:       hub,
		cancels:   make(map[int64]context.CancelFunc),
	}
}

// SetNotificationService wires the in-app notification service. Optional.
func (s *ExecutorService) SetNotificationService(n *NotificationService) {
	s.notifier = n
}

// notifyCompleted posts a bell notification for a finished (or failed) execution.
// Called at the end of runExecution once the final status is known.
func (s *ExecutorService) notifyCompleted(execID int64) {
	if s.notifier == nil {
		return
	}
	exec, err := s.db.GetExecution(execID)
	if err != nil || exec == nil {
		return
	}
	s.notifier.NotifyExecutionCompleted(exec)
}

type ExecuteRequest struct {
	ActionID        *int64            `json:"action_id,omitempty"`
	Script          string            `json:"script,omitempty"`
	TimeoutS        int               `json:"timeout_seconds,omitempty"`
	ParameterValues map[string]string `json:"parameter_values,omitempty"`
}

// Execute starts an action execution on a VM. It resolves credentials,
// builds the script, and spawns a goroutine for the SSH execution.
func (s *ExecutorService) Execute(ctx context.Context, vmID int64, req ExecuteRequest, userID int64) (*models.ActionExecution, error) {
	// Validate request: exactly one of action_id or script
	if req.ActionID != nil && req.Script != "" {
		return nil, fmt.Errorf("provide either action_id or script, not both")
	}
	if req.ActionID == nil && req.Script == "" {
		return nil, fmt.Errorf("action_id or script is required")
	}

	// Resolve timeout
	timeout := defaultTimeoutSecs
	if req.TimeoutS > 0 {
		timeout = req.TimeoutS
	}
	if timeout < minTimeoutSecs {
		timeout = minTimeoutSecs
	}
	if timeout > maxTimeoutSecs {
		timeout = maxTimeoutSecs
	}

	// Concurrency checks
	vmCount, err := s.db.CountRunningExecutionsForVM(vmID)
	if err != nil {
		return nil, fmt.Errorf("check vm concurrency: %w", err)
	}
	if vmCount >= maxConcurrentPerVM {
		return nil, fmt.Errorf("maximum %d concurrent executions per VM reached", maxConcurrentPerVM)
	}
	totalCount, err := s.db.CountRunningExecutionsTotal()
	if err != nil {
		return nil, fmt.Errorf("check total concurrency: %w", err)
	}
	if totalCount >= maxConcurrentTotal {
		return nil, fmt.Errorf("maximum %d concurrent executions system-wide reached", maxConcurrentTotal)
	}

	// Resolve VM and check preconditions
	vm, err := s.db.GetManagedVM(vmID)
	if err != nil {
		return nil, fmt.Errorf("VM not found: %w", err)
	}
	if vm.PowerState != "poweredOn" && vm.PowerState != "running" {
		return nil, fmt.Errorf("VM must be powered on to execute actions")
	}
	if vm.IPAddress == "" {
		return nil, fmt.Errorf("VM has no IP address — is it powered on?")
	}

	// Resolve the catalogue action first: a draft is refused before any
	// credentials are touched, so the reason is never masked.
	var action *models.Action // the catalogue action being run, nil for ad-hoc scripts
	if req.ActionID != nil {
		action, err = s.db.GetAction(*req.ActionID)
		if err != nil {
			return nil, fmt.Errorf("action not found: %w", err)
		}
		if action.Status == models.ActionStatusDraft {
			return nil, fmt.Errorf("%w: %q is a draft — review and publish it before running it", ErrActionDraft, action.Name)
		}
	}

	// Resolve credentials: an explicitly set per-VM login wins, otherwise the
	// deployment's initial credentials.
	creds, err := resolveVMCredentials(s.db, s.encryptor, vm)
	if err != nil {
		if errors.Is(err, ErrNoCredentials) {
			return nil, fmt.Errorf("%w — set SSH credentials on the VM page (adopted and registered VMs have none until you do)", err)
		}
		return nil, err
	}
	username := creds.Username
	auth := sshAuth{Password: creds.Password, PrivateKey: creds.PrivateKey, SudoPassword: creds.SudoPassword}

	// Resolve script
	var script string
	var actionName string
	var actionID *int64
	var paramEnvBlock string

	if action != nil {
		if err := validateScript(action.Script); err != nil {
			return nil, fmt.Errorf("invalid action script: %w", err)
		}
		script = action.Script
		actionName = action.Name
		actionID = req.ActionID

		// Validate and build parameter env vars
		if len(action.Parameters) > 0 {
			envBlock, err := validateAndBuildParams(action.Parameters, req.ParameterValues)
			if err != nil {
				return nil, err
			}
			paramEnvBlock = envBlock
		}
	} else {
		if err := validateScript(req.Script); err != nil {
			return nil, fmt.Errorf("invalid script: %w", err)
		}
		script = req.Script
		actionName = "Ad-hoc script"
	}

	// Build storage-safe parameter values (redact passwords) from the action
	// loaded above — no second lookup.
	var storedParamValues map[string]string
	if len(req.ParameterValues) > 0 && req.ActionID != nil {
		storedParamValues = make(map[string]string, len(req.ParameterValues))
		passwordParams := map[string]bool{}
		if action != nil {
			for _, p := range action.Parameters {
				if p.Type == "password" {
					passwordParams[p.Name] = true
				}
			}
		}
		for k, v := range req.ParameterValues {
			if passwordParams[k] {
				storedParamValues[k] = "***"
			} else {
				storedParamValues[k] = v
			}
		}
	}

	// Create execution record
	exec := &models.ActionExecution{
		VMID:            vmID,
		ActionID:        actionID,
		ActionName:      actionName,
		Script:          script,
		Status:          "pending",
		ParameterValues: storedParamValues,
		CreatedBy:       userID,
	}
	if err := s.db.CreateExecution(exec); err != nil {
		return nil, fmt.Errorf("create execution: %w", err)
	}

	// Spawn execution goroutine
	execCtx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	s.mu.Lock()
	s.cancels[exec.ID] = cancel
	s.mu.Unlock()

	go s.runExecution(execCtx, cancel, exec.ID, vmID, vm.IPAddress, username, auth, script, paramEnvBlock)

	return exec, nil
}

// dbHostKeyStore adapts *db.DB to the HostKeyStore interface for TOFU verification.
type dbHostKeyStore struct{ db *db.DB }

func (s *dbHostKeyStore) GetHostKeyFP(vmID int64) (string, error) {
	return s.db.GetManagedVMHostKeyFP(vmID)
}
func (s *dbHostKeyStore) SetHostKeyFP(vmID int64, fp string) error {
	return s.db.UpdateManagedVMHostKeyFP(vmID, fp)
}

// runExecution performs the actual SSH execution in a goroutine.
func (s *ExecutorService) runExecution(ctx context.Context, cancel context.CancelFunc, execID, vmID int64, host, username string, auth sshAuth, script, paramEnvBlock string) {
	defer cancel()
	defer func() {
		s.mu.Lock()
		delete(s.cancels, execID)
		s.mu.Unlock()
	}()

	// Mark as running
	if err := s.db.UpdateExecutionStatus(execID, "running", nil, ""); err != nil {
		slog.Error("failed to mark execution running", "execution_id", execID, "error", err)
		return
	}
	s.sendWSStatus(execID, "running", nil)

	var outputBuf []byte

	outputFn := func(line string) {
		// Send to WebSocket
		s.hub.SendOutput(execID, map[string]interface{}{
			"type": "output",
			"data": map[string]string{
				"line": line,
			},
		})
		// Accumulate for DB storage (with limit)
		newLine := line + "\n"
		if len(outputBuf)+len(newLine) <= maxOutputSize {
			outputBuf = append(outputBuf, newLine...)
		}
	}

	exitCode, err := sshExecute(ctx, host, 22, username, auth, script, paramEnvBlock, outputFn, &dbHostKeyStore{s.db}, vmID, execID)

	// Clear the secret from the stack (best effort — Go GC may have already copied)
	auth = sshAuth{}
	_ = auth

	output := string(outputBuf)

	if err != nil {
		// Check if cancelled
		if ctx.Err() == context.Canceled {
			s.db.UpdateExecutionStatus(execID, "cancelled", nil, output)
			s.sendWSStatus(execID, "cancelled", nil)
			slog.Info("execution cancelled", "execution_id", execID)
			s.notifyCompleted(execID)
			return
		}
		if ctx.Err() == context.DeadlineExceeded {
			s.db.UpdateExecutionStatus(execID, "failed", nil, output+"\n[TIMEOUT] Execution timed out")
			s.sendWSError(execID, "execution timed out")
			slog.Info("execution timed out", "execution_id", execID)
			s.notifyCompleted(execID)
			return
		}
		msg := err.Error()
		var sudoErr *SudoError
		if errors.As(err, &sudoErr) {
			// The login worked; sudo is the problem. Say so in plain words and
			// leave a trace on the VM so it is visible without opening the run.
			msg = sudoErr.Message()
			if vm, vmErr := s.db.GetManagedVM(vmID); vmErr == nil {
				vmEventSink{db: s.db, vmID: vm.ID, targetID: vm.TargetID}.Event("warn", "Action could not run: "+msg)
			}
		}
		s.db.UpdateExecutionStatus(execID, "failed", nil, output+"\n[ERROR] "+msg)
		s.sendWSError(execID, msg)
		slog.Error("execution failed", "execution_id", execID, "error", err)
		s.notifyCompleted(execID)
		return
	}

	status := "completed"
	if exitCode != 0 {
		status = "failed"
	}
	s.db.UpdateExecutionStatus(execID, status, &exitCode, output)
	s.sendWSStatus(execID, status, &exitCode)
	slog.Info("execution finished", "execution_id", execID, "exit_code", exitCode, "status", status)
	s.notifyCompleted(execID)
}

// Cancel cancels a running execution.
func (s *ExecutorService) Cancel(executionID int64) error {
	s.mu.Lock()
	cancel, ok := s.cancels[executionID]
	s.mu.Unlock()

	if !ok {
		return fmt.Errorf("execution not running or already completed")
	}

	cancel()
	return nil
}

// GetExecution returns a single execution by ID.
func (s *ExecutorService) GetExecution(id int64) (*models.ActionExecution, error) {
	return s.db.GetExecution(id)
}

// ListVMExecutions returns all executions for a VM.
func (s *ExecutorService) ListVMExecutions(vmID int64) ([]models.ActionExecution, error) {
	return s.db.ListVMExecutions(vmID)
}

func (s *ExecutorService) sendWSStatus(execID int64, status string, exitCode *int) {
	data := map[string]interface{}{
		"status": status,
	}
	if exitCode != nil {
		data["exit_code"] = *exitCode
	}
	s.hub.SendOutput(execID, map[string]interface{}{
		"type": "status",
		"data": data,
	})
}

func (s *ExecutorService) sendWSError(execID int64, message string) {
	s.hub.SendOutput(execID, map[string]interface{}{
		"type": "error",
		"data": map[string]string{
			"message": message,
		},
	})
}

var numberRegexp = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

// validateAndBuildParams validates parameter values against definitions and
// returns a block of shell export statements to inject before the script.
func validateAndBuildParams(params []models.ActionParameter, values map[string]string) (string, error) {
	var exports strings.Builder
	for _, p := range params {
		val, provided := values[p.Name]
		if !provided || val == "" {
			if p.Required {
				if p.Default != "" {
					val = p.Default
				} else {
					return "", fmt.Errorf("required parameter %q is missing", p.Name)
				}
			} else {
				val = p.Default
			}
		}

		// Type validation
		switch p.Type {
		case "number":
			if val != "" && !numberRegexp.MatchString(val) {
				return "", fmt.Errorf("parameter %q must be a number", p.Name)
			}
		case "boolean":
			if val != "" && val != "true" && val != "false" {
				return "", fmt.Errorf("parameter %q must be \"true\" or \"false\"", p.Name)
			}
		case "select":
			if val != "" {
				found := false
				for _, opt := range p.Options {
					if opt == val {
						found = true
						break
					}
				}
				if !found {
					return "", fmt.Errorf("parameter %q value %q not in allowed options", p.Name, val)
				}
			}
		}

		exports.WriteString("export PARAM_")
		exports.WriteString(p.Name)
		exports.WriteString("=")
		exports.WriteString(shellEscape(val))
		exports.WriteString("\n")
	}
	return exports.String(), nil
}

// shellEscape wraps a value in single quotes, escaping any embedded single
// quotes with the '\” idiom to prevent shell injection.
func shellEscape(val string) string {
	return "'" + strings.ReplaceAll(val, "'", `'\''`) + "'"
}

// unused import guard
var _ = strconv.Itoa

// ErrActionDraft: a draft action was asked to run. Drafts are never runnable.
var ErrActionDraft = errors.New("draft actions cannot run")
