package factory

import (
	"strings"
	"testing"
)

// TestAutoinstallNoTargetTypeConditionals verifies the autoinstall template
// produces correct platform-specific output via InstallerHints without any
// TargetType conditionals in the template itself.
func TestAutoinstallNoTargetTypeConditionals(t *testing.T) {
	tests := []struct {
		name       string
		targetType string
		wantDS     string   // expected datasource_list value
		wantExtra  []string // expected extra cloud.cfg lines
		notWant    []string // lines that must NOT appear
	}{
		{
			name:       "proxmox",
			targetType: "proxmox",
			wantDS:     "datasource_list: [NoCloud, ConfigDrive, None]",
			wantExtra:  nil,
			notWant:    []string{"disable_vmware_customization", "VMware"},
		},
		{
			name:       "vcenter",
			targetType: "vcenter",
			wantDS:     "datasource_list: [VMware, OVF, None]",
			wantExtra:  []string{"disable_vmware_customization: false"},
			notWant:    []string{"NoCloud"},
		},
		{
			name:       "esxi",
			targetType: "esxi",
			wantDS:     "datasource_list: [VMware, OVF, None]",
			wantExtra:  []string{"disable_vmware_customization: false"},
			notWant:    []string{"NoCloud"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := GenerateAutoinstall(tt.targetType, "testpass", "ssh-rsa AAAA", "10.0.0.50", "255.255.255.0", "10.0.0.1")
			if err != nil {
				t.Fatalf("GenerateAutoinstall(%s): %v", tt.targetType, err)
			}

			if !strings.Contains(output, tt.wantDS) {
				t.Errorf("want datasource_list containing %q, not found in output", tt.wantDS)
			}

			for _, extra := range tt.wantExtra {
				if !strings.Contains(output, extra) {
					t.Errorf("want extra line %q, not found in output", extra)
				}
			}

			for _, bad := range tt.notWant {
				if strings.Contains(output, bad) {
					t.Errorf("output must NOT contain %q for target %s", bad, tt.targetType)
				}
			}

			// Verify apt hardening is present
			if !strings.Contains(output, "updates: security") {
				t.Error("missing 'updates: security'")
			}
			if !strings.Contains(output, "fallback: offline-install") {
				t.Error("missing apt fallback: offline-install")
			}
			if !strings.Contains(output, "geoip: false") {
				t.Error("missing apt geoip: false")
			}

			// Verify NO TargetType conditionals leaked into output
			if strings.Contains(output, "eq .TargetType") {
				t.Error("template conditional leaked into output")
			}
		})
	}
}

// TestKickstartNoTargetTypeConditionals verifies the kickstart template
// produces correct platform-specific output via InstallerHints.
func TestKickstartNoTargetTypeConditionals(t *testing.T) {
	tests := []struct {
		name       string
		targetType string
		wantDS     string
		wantSvc    []string
		wantPkg    string
		notWantPkg string
	}{
		{
			name:       "proxmox",
			targetType: "proxmox",
			wantDS:     "datasource_list: [NoCloud, ConfigDrive, None]",
			wantSvc:    []string{"qemu-guest-agent"},
			wantPkg:    "qemu-guest-agent",
			notWantPkg: "open-vm-tools",
		},
		{
			name:       "vcenter",
			targetType: "vcenter",
			wantDS:     "datasource_list: [VMware, OVF, None]",
			wantSvc:    []string{"vmtoolsd"},
			wantPkg:    "open-vm-tools",
			notWantPkg: "qemu-guest-agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plat, err := GetPlatform(tt.targetType)
			if err != nil {
				t.Fatalf("GetPlatform(%s): %v", tt.targetType, err)
			}
			hints := plat.InstallerHints()

			inst, err := GetInstaller("kickstart")
			if err != nil {
				t.Fatalf("GetInstaller(kickstart): %v", err)
			}

			output, err := inst.GenerateConfig(InstallerParams{
				TargetType:           tt.targetType,
				OSFamily:             "rhel",
				OSVersion:            "9",
				Username:             "forgemill",
				Password:             "testpass",
				Hostname:             "forgemill-template",
				Timezone:             "UTC",
				Locale:               "en_US.UTF-8",
				Keyboard:             "us",
				ExtraPackages:        plat.ProvisionerPackages("rhel"),
				InterfaceName:        plat.InterfaceName(),
				CloudInitDatasources: hints.CloudInitDatasources,
				CloudInitExtraLines:  hints.CloudInitExtraLines,
				PlatformServices:     hints.PlatformServices,
			})
			if err != nil {
				t.Fatalf("GenerateConfig: %v", err)
			}

			if !strings.Contains(output, tt.wantDS) {
				t.Errorf("want %q in output", tt.wantDS)
			}

			for _, svc := range tt.wantSvc {
				if !strings.Contains(output, "systemctl enable "+svc) {
					t.Errorf("want service enable for %q", svc)
				}
			}

			if !strings.Contains(output, tt.wantPkg) {
				t.Errorf("want package %q in output", tt.wantPkg)
			}

			if strings.Contains(output, tt.notWantPkg) {
				t.Errorf("output must NOT contain package %q for target %s", tt.notWantPkg, tt.targetType)
			}
		})
	}
}

// TestInstallerHintsInterface verifies all registered platforms implement InstallerHints.
func TestInstallerHintsInterface(t *testing.T) {
	for _, targetType := range []string{"proxmox", "vcenter", "esxi"} {
		plat, err := GetPlatform(targetType)
		if err != nil {
			t.Fatalf("GetPlatform(%s): %v", targetType, err)
		}
		hints := plat.InstallerHints()
		if hints.CloudInitDatasources == "" {
			t.Errorf("platform %s returned empty CloudInitDatasources", targetType)
		}
		if len(hints.PlatformServices) == 0 {
			t.Errorf("platform %s returned no PlatformServices", targetType)
		}
	}
}
