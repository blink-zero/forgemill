package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	sshConnectTimeout = 30 * time.Second
	sshDefaultTimeout = 10 * time.Minute
	sshMaxTimeout     = 30 * time.Minute
	sshIdleTimeout    = 5 * time.Minute
	maxScriptSize     = 64 * 1024  // 64KB
	maxOutputSize     = 1024 * 1024 // 1MB
)

// HostKeyStore provides TOFU (Trust-On-First-Use) host key storage.
// If GetHostKeyFP returns "" the key is trusted and stored on first connect.
// On subsequent connects the stored fingerprint is verified.
type HostKeyStore interface {
	GetHostKeyFP(vmID int64) (string, error)
	SetHostKeyFP(vmID int64, fingerprint string) error
}

// ValidateActionScript validates a script for use as a saved action.
// Exported for use by API handlers.
func ValidateActionScript(script string) error {
	return validateScript(script)
}

// validateScript checks that a script is safe to execute.
func validateScript(script string) error {
	if len(script) == 0 {
		return fmt.Errorf("script is empty")
	}
	if len(script) > maxScriptSize {
		return fmt.Errorf("script exceeds maximum size of %d bytes", maxScriptSize)
	}
	if strings.ContainsRune(script, '\x00') {
		return fmt.Errorf("script contains null bytes")
	}
	return nil
}

// sshExecute connects to a host via SSH, executes a script, and streams output
// line-by-line via outputFn. Returns the exit code and any error.
//
// If hkStore is non-nil, TOFU host key verification is applied:
//   - First connection (no stored fingerprint): accept and store the key
//   - Subsequent connections: verify the key matches the stored fingerprint
func sshExecute(ctx context.Context, host string, port int, username, password, script, paramEnvBlock string, outputFn func(line string), hkStore HostKeyStore, vmID int64) (int, error) {
	if port == 0 {
		port = 22
	}

	// Build host key callback: TOFU when store is available, insecure fallback otherwise
	hostKeyCallback := ssh.InsecureIgnoreHostKey()
	if hkStore != nil && vmID > 0 {
		storedFP, err := hkStore.GetHostKeyFP(vmID)
		if err != nil {
			slog.Warn("tofu: failed to read stored host key, falling back to insecure", "vm_id", vmID, "error", err)
		} else {
			hostKeyCallback = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
				fp := ssh.FingerprintSHA256(key)
				if storedFP == "" {
					// First connection — trust and store
					if storeErr := hkStore.SetHostKeyFP(vmID, fp); storeErr != nil {
						slog.Warn("tofu: failed to store host key fingerprint", "vm_id", vmID, "error", storeErr)
					} else {
						slog.Info("tofu: stored host key on first connect", "vm_id", vmID, "fingerprint", fp)
					}
					return nil
				}
				// Subsequent connection — verify
				if fp != storedFP {
					return fmt.Errorf("SSH host key mismatch for VM %d (%s): expected %s, got %s — possible MitM or VM was rebuilt (reset host key from VM detail page)", vmID, hostname, storedFP, fp)
				}
				return nil
			}
		}
	}

	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: hostKeyCallback,
		Timeout:         sshConnectTimeout,
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	slog.Info("ssh connecting", "host", addr, "user", username)

	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return -1, fmt.Errorf("ssh connect: %w", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return -1, fmt.Errorf("ssh session: %w", err)
	}
	defer session.Close()

	// Security: No agent forwarding, no port forwarding
	// (not requesting any by default — just documenting the intent)

	// Set up pipes for stdout and stderr
	stdout, err := session.StdoutPipe()
	if err != nil {
		return -1, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return -1, fmt.Errorf("stderr pipe: %w", err)
	}

	// Wrap the script in sudo bash with strict mode via heredoc.
	// The forgemill user has NOPASSWD:ALL sudo, and most actions (apt-get,
	// systemctl, etc.) require root. Cloud-init runs as root natively, but
	// SSH sessions run as the deploy user, so sudo is needed.
	// Using heredoc avoids quoting issues with single/double quotes in scripts.
	wrappedScript := "sudo bash <<'FORGEMILL_SCRIPT'\nset -euo pipefail\nexport DEBIAN_FRONTEND=noninteractive\n" + paramEnvBlock + script + "\nFORGEMILL_SCRIPT"

	if err := session.Start(wrappedScript); err != nil {
		return -1, fmt.Errorf("ssh start: %w", err)
	}

	// Stream output in a goroutine, merging stdout and stderr
	outputDone := make(chan struct{})
	var totalOutput int
	truncated := false

	go func() {
		defer close(outputDone)
		combined := io.MultiReader(stdout, stderr)
		buf := make([]byte, 4096)
		var lineBuf bytes.Buffer
		for {
			n, readErr := combined.Read(buf)
			if n > 0 {
				for _, b := range buf[:n] {
					if totalOutput >= maxOutputSize && !truncated {
						truncated = true
						outputFn("\n[WARNING] Output truncated at 1MB limit")
						continue
					}
					if truncated {
						continue
					}
					totalOutput++
					if b == '\n' {
						outputFn(lineBuf.String())
						lineBuf.Reset()
					} else {
						lineBuf.WriteByte(b)
					}
				}
			}
			if readErr != nil {
				// Flush remaining line buffer
				if lineBuf.Len() > 0 && !truncated {
					outputFn(lineBuf.String())
				}
				break
			}
		}
	}()

	// Wait for command completion with context cancellation
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- session.Wait()
	}()

	select {
	case <-ctx.Done():
		// Context cancelled — attempt to signal remote process
		_ = session.Signal(ssh.SIGTERM)
		// Give it a moment to die, then close
		select {
		case <-waitDone:
		case <-time.After(5 * time.Second):
			_ = session.Signal(ssh.SIGKILL)
		}
		<-outputDone
		return -1, ctx.Err()

	case err := <-waitDone:
		<-outputDone
		if err != nil {
			if exitErr, ok := err.(*ssh.ExitError); ok {
				return exitErr.ExitStatus(), nil
			}
			return -1, fmt.Errorf("ssh wait: %w", err)
		}
		return 0, nil
	}
}
