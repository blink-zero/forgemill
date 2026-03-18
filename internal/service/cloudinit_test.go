package service

import (
	"strings"
	"testing"

	"github.com/forgemill/forgemill/internal/db/models"
)

func TestMergeScripts(t *testing.T) {
	base := "#cloud-config\nusers:\n  - name: forgemill\n    lock_passwd: false\n    passwd: $6$hash\n    shell: /bin/bash\nchpasswd:\n  expire: false\nssh_pwauth: true\n"
	
	actions := []models.Action{
		{ID: 1, Script: `{"runcmd":["apt-get update -y"]}`},
		{ID: 2, Script: `{"packages":["qemu-guest-agent"],"runcmd":["systemctl enable --now qemu-guest-agent"]}`},
	}
	
	result := mergeCloudConfigs(base, actions)
	
	if !strings.HasPrefix(result, "#cloud-config") {
		t.Error("missing #cloud-config header")
	}
	if !strings.Contains(result, "forgemill") {
		t.Error("lost user config")
	}
	if !strings.Contains(result, "qemu-guest-agent") {
		t.Error("missing package from action")
	}
	if !strings.Contains(result, "apt-get update") {
		t.Error("missing runcmd from action")
	}
	if !strings.Contains(result, "ssh_pwauth") {
		t.Error("lost ssh_pwauth")
	}
}

func TestMergeScriptsNoActions(t *testing.T) {
	base := "#cloud-config\nusers:\n  - name: forgemill\n"
	result := mergeCloudConfigs(base, nil)
	if result != base {
		t.Error("should return base unchanged when no actions")
	}
}

func TestBuildFullCloudInitUserdata(t *testing.T) {
	actions := []models.Action{
		{ID: 1, Script: `{"packages":["htop"],"runcmd":["echo hello"]}`},
	}
	result := buildFullCloudInitUserdata("$6$hash", "plain", "ssh-ed25519 AAAA", "test-vm", actions)
	if !strings.Contains(result, "htop") {
		t.Error("missing package")
	}
	if !strings.Contains(result, "echo hello") {
		t.Error("missing runcmd")
	}
	if !strings.Contains(result, "forgemill") {
		t.Error("missing user")
	}
	if !strings.Contains(result, "ssh-ed25519") {
		t.Error("missing ssh key")
	}
	if !strings.Contains(result, "hostname: test-vm") {
		t.Error("missing hostname")
	}
	if !strings.Contains(result, "manage_etc_hosts: true") {
		t.Error("missing manage_etc_hosts")
	}
}

func TestValidateActionScript(t *testing.T) {
	// Valid script
	if err := ValidateActionScript("#!/bin/bash\necho hello"); err != nil {
		t.Errorf("valid script rejected: %v", err)
	}
	// Empty script
	if err := ValidateActionScript(""); err == nil {
		t.Error("should reject empty script")
	}
	// Script with null bytes
	if err := ValidateActionScript("echo hello\x00world"); err == nil {
		t.Error("should reject script with null bytes")
	}
}
