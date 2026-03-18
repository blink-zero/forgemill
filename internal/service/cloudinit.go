package service

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/forgemill/forgemill/internal/db/models"
	"gopkg.in/yaml.v3"
)

// cloudConfig represents the subset of cloud-init cloud-config keys that
// actions may contribute. Only these keys are merged from action fragments.
type cloudConfig struct {
	Packages   []string          `yaml:"packages,omitempty"   json:"packages,omitempty"`
	Runcmd     []string          `yaml:"runcmd,omitempty"     json:"runcmd,omitempty"`
	WriteFiles []cloudWriteFile  `yaml:"write_files,omitempty" json:"write_files,omitempty"`
}

type cloudWriteFile struct {
	Path        string `yaml:"path"                  json:"path"`
	Content     string `yaml:"content"               json:"content"`
	Permissions string `yaml:"permissions,omitempty"  json:"permissions,omitempty"`
	Owner       string `yaml:"owner,omitempty"        json:"owner,omitempty"`
}

// mergeCloudConfigs takes a base cloud-config YAML string (the credential
// injection userdata) and a list of actions, and produces a merged
// cloud-config YAML string. The merge logic:
//   - packages: union (deduplicated)
//   - runcmd: concatenated in order (base first, then each action)
//   - write_files: concatenated
//
// All other keys in the base YAML are preserved as-is.
func mergeCloudConfigs(base string, actions []models.Action) string {
	if len(actions) == 0 {
		return base
	}

	// Parse base YAML into a generic map to preserve all keys.
	baseMap := make(map[string]interface{})
	cleanBase := strings.TrimPrefix(strings.TrimSpace(base), "#cloud-config")
	cleanBase = strings.TrimSpace(cleanBase)
	if cleanBase != "" {
		if err := yaml.Unmarshal([]byte(cleanBase), &baseMap); err != nil {
			slog.Error("failed to parse base cloud-config", "error", err)
			return base
		}
	}

	// Collect merged packages, runcmd, and write_files from base.
	pkgSet := make(map[string]bool)
	var mergedPkgs []string
	var mergedRuncmd []interface{}
	var mergedWriteFiles []interface{}

	// Extract existing packages from base.
	if p, ok := baseMap["packages"]; ok {
		if pkgs, ok := p.([]interface{}); ok {
			for _, pkg := range pkgs {
				if s, ok := pkg.(string); ok && !pkgSet[s] {
					pkgSet[s] = true
					mergedPkgs = append(mergedPkgs, s)
				}
			}
		}
	}
	// Extract existing runcmd from base.
	if r, ok := baseMap["runcmd"]; ok {
		if cmds, ok := r.([]interface{}); ok {
			mergedRuncmd = append(mergedRuncmd, cmds...)
		}
	}
	// Extract existing write_files from base.
	if w, ok := baseMap["write_files"]; ok {
		if files, ok := w.([]interface{}); ok {
			mergedWriteFiles = append(mergedWriteFiles, files...)
		}
	}

	// Merge each action's cloud_config fragment.
	for _, action := range actions {
		var fragment cloudConfig
		if err := json.Unmarshal([]byte(action.Script), &fragment); err != nil {
			slog.Error("failed to parse action cloud_config", "action_id", action.ID, "error", err)
			continue
		}

		for _, pkg := range fragment.Packages {
			if !pkgSet[pkg] {
				pkgSet[pkg] = true
				mergedPkgs = append(mergedPkgs, pkg)
			}
		}
		for _, cmd := range fragment.Runcmd {
			mergedRuncmd = append(mergedRuncmd, cmd)
		}
		for _, wf := range fragment.WriteFiles {
			mergedWriteFiles = append(mergedWriteFiles, wf)
		}
	}

	// Write merged keys back to the base map.
	if len(mergedPkgs) > 0 {
		baseMap["packages"] = mergedPkgs
	}
	if len(mergedRuncmd) > 0 {
		baseMap["runcmd"] = mergedRuncmd
	}
	if len(mergedWriteFiles) > 0 {
		baseMap["write_files"] = mergedWriteFiles
	}

	out, err := yaml.Marshal(baseMap)
	if err != nil {
		slog.Error("failed to marshal merged cloud-config", "error", err)
		return base
	}
	return "#cloud-config\n" + string(out)
}

// buildFullCloudInitUserdata generates a complete cloud-config string with
// credential injection and merged action fragments. This is used when actions
// are selected, producing a single userdata blob for both VMware and Proxmox.
func buildFullCloudInitUserdata(passwordHash, plainPassword, sshPublicKey, hostname string, actions []models.Action) string {
	var b strings.Builder
	b.WriteString("#cloud-config\n")
	if hostname != "" {
		b.WriteString(fmt.Sprintf("hostname: %s\n", hostname))
		b.WriteString("manage_etc_hosts: true\n")
	}
	b.WriteString("users:\n")
	b.WriteString("  - name: forgemill\n")
	b.WriteString("    lock_passwd: false\n")
	b.WriteString(fmt.Sprintf("    passwd: %s\n", passwordHash))
	b.WriteString("    shell: /bin/bash\n")
	b.WriteString("    sudo: ALL=(ALL) NOPASSWD:ALL\n")
	b.WriteString("    groups: sudo\n")
	if sshPublicKey != "" {
		b.WriteString("    ssh_authorized_keys:\n")
		b.WriteString(fmt.Sprintf("      - %s\n", sshPublicKey))
	}
	// Use chpasswd module as belt-and-suspenders for password setting.
	// The users.passwd field works for new users, but on some cloud-init versions
	// it doesn't reliably update existing users (e.g. when the template already has
	// the 'forgemill' user with a locked password from the build phase).
	// chpasswd.users always works regardless of user pre-existence.
	b.WriteString("chpasswd:\n")
	b.WriteString("  expire: false\n")
	if plainPassword != "" {
		b.WriteString("  users:\n")
		b.WriteString(fmt.Sprintf("    - name: forgemill\n"))
		b.WriteString(fmt.Sprintf("      password: %s\n", plainPassword))
		b.WriteString("      type: text\n")
	}
	b.WriteString("ssh_pwauth: true\n")

	base := b.String()
	merged := mergeCloudConfigs(base, actions)

	// SEC-002: Append credential cleanup commands AFTER all action runcmds.
	// This ensures cloud-init seed data (which contains plaintext passwords)
	// is removed from hypervisor-accessible storage after first boot completes.
	merged = appendCleanupRuncmd(merged)

	return merged
}

// appendCleanupRuncmd appends credential cleanup runcmd entries to the end
// of a cloud-config YAML string, after all other runcmd entries.
func appendCleanupRuncmd(cloudConfig string) string {
	cleanupCmds := []string{
		// VMware: clear guestinfo userdata (contains plaintext password)
		`if command -v vmware-rpctool >/dev/null 2>&1; then vmware-rpctool "info-set guestinfo.userdata  " && vmware-rpctool "info-set guestinfo.userdata.encoding  "; fi`,
		// All platforms: remove cloud-init seed data from disk
		`cloud-init clean --seed 2>/dev/null || true`,
	}

	// Parse, append, re-marshal to ensure correct YAML structure
	baseMap := make(map[string]interface{})
	clean := strings.TrimPrefix(strings.TrimSpace(cloudConfig), "#cloud-config")
	clean = strings.TrimSpace(clean)
	if clean != "" {
		if err := yaml.Unmarshal([]byte(clean), &baseMap); err != nil {
			slog.Error("failed to parse cloud-config for cleanup injection", "error", err)
			return cloudConfig
		}
	}

	var runcmd []interface{}
	if existing, ok := baseMap["runcmd"]; ok {
		if cmds, ok := existing.([]interface{}); ok {
			runcmd = append(runcmd, cmds...)
		}
	}
	for _, cmd := range cleanupCmds {
		runcmd = append(runcmd, cmd)
	}
	baseMap["runcmd"] = runcmd

	out, err := yaml.Marshal(baseMap)
	if err != nil {
		slog.Error("failed to marshal cloud-config with cleanup", "error", err)
		return cloudConfig
	}
	return "#cloud-config\n" + string(out)
}

// ValidateActionCloudConfig validates that a cloud_config JSON string only
// contains allowed keys (packages, runcmd, write_files) and is valid JSON.
func ValidateActionCloudConfig(raw string) error {
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	allowed := map[string]bool{"packages": true, "runcmd": true, "write_files": true}
	for key := range parsed {
		if !allowed[key] {
			return fmt.Errorf("disallowed key %q; only packages, runcmd, write_files are allowed", key)
		}
	}

	// Validate types of each key.
	if raw, ok := parsed["packages"]; ok {
		var pkgs []string
		if err := json.Unmarshal(raw, &pkgs); err != nil {
			return fmt.Errorf("packages must be an array of strings: %w", err)
		}
	}
	if raw, ok := parsed["runcmd"]; ok {
		var cmds []string
		if err := json.Unmarshal(raw, &cmds); err != nil {
			return fmt.Errorf("runcmd must be an array of strings: %w", err)
		}
	}
	if raw, ok := parsed["write_files"]; ok {
		var files []cloudWriteFile
		if err := json.Unmarshal(raw, &files); err != nil {
			return fmt.Errorf("write_files must be an array of objects with path and content: %w", err)
		}
		for _, f := range files {
			if f.Path == "" || f.Content == "" {
				return fmt.Errorf("write_files entries must have path and content")
			}
		}
	}

	return nil
}
