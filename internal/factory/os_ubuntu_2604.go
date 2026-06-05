package factory

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path"
	"strings"
	"time"
)

// This file is intentionally isolated from os_ubuntu.go. The boot commands
// and provisioner commands below are duplicated rather than shared so that
// any future divergence in Ubuntu 26.04's installer behaviour can be
// adjusted here without risking regressions on 22.04 / 24.04.

// ubuntu2604BootCommandHTTP is used when the installer config is served via HTTP.
// Used by: Proxmox (always), vSphere (fallback for non-nocloud installers).
// The {{ .HTTPIP }}:{{ .HTTPPort }} placeholders are resolved by Packer at runtime.
var ubuntu2604BootCommandHTTP = []string{
	"c<wait>",
	"linux /casper/vmlinuz --- autoinstall ds='nocloud-net;s=http://{{ .HTTPIP }}:{{ .HTTPPort }}/'",
	"<enter><wait>",
	"initrd /casper/initrd",
	"<enter><wait>",
	"boot",
	"<enter>",
}

// ubuntu2604BootCommandCD is used when the installer config is delivered via virtual CD-ROM.
// Used by: vSphere with nocloud CD mount (cidata label).
// Note: ds="nocloud" (no URL) tells cloud-init to read from attached CD.
var ubuntu2604BootCommandCD = []string{
	"<wait3s>c<wait3s>",
	`linux /casper/vmlinuz --- autoinstall ds="nocloud"`,
	"<enter><wait>",
	"initrd /casper/initrd",
	"<enter><wait>",
	"boot",
	"<enter>",
}

// ubuntu2604ProvisionerCmds are run by Packer's shell provisioner before converting to template.
// These prepare the VM for cloning: install cloud-init, clean up installer artifacts,
// reset machine-id, and lock the build account.
var ubuntu2604ProvisionerCmds = []string{
	// Ensure critical packages are installed
	"sudo apt-get update || true",
	"sudo apt-get install -y cloud-init cloud-initramfs-growroot || echo 'WARNING: apt install failed - skipping'",
	// Clean up networking for template (cloud-init will manage on clone)
	"sudo rm -f /etc/netplan/*.yaml",
	// Remove subiquity/installer configs that interfere with VMware datasource
	"sudo rm -f /etc/cloud/cloud.cfg.d/subiquity-disable-cloudinit-networking.cfg",
	"sudo rm -f /etc/cloud/cloud.cfg.d/99-installer.cfg",
	// Remove autoinstall boot params from GRUB
	"sudo sed -i 's/autoinstall ds=nocloud//g' /etc/default/grub",
	"sudo update-grub",
	// Reset cloud-init so it runs fresh on first clone boot
	"sudo cloud-init clean --logs || true",
	"sudo rm -rf /var/lib/cloud/",
	// Remove machine-id so each clone gets a unique one (important for DHCP)
	"sudo truncate -s 0 /etc/machine-id",
	"sudo rm -f /var/lib/dbus/machine-id",
	"sudo ln -s /etc/machine-id /var/lib/dbus/machine-id || true",
	// Lock the build account password
	"sudo passwd -l forgemill",
}

// ubuntu2604ChecksumURL is the SHA256SUMS file for the 26.04 release directory.
// Pulled out into a constant so the retry-aware fetcher below and the OS
// definition below share a single source of truth.
const ubuntu2604ChecksumURL = "https://releases.ubuntu.com/26.04/SHA256SUMS"

// fetchUbuntu2604Checksum is wired up as OSDefinition.CustomChecksumFetcher
// for the 26.04 entry below. It exists because releases.ubuntu.com is
// round-robin DNS across multiple mirror IPs, and during early 26.04
// release traffic at least one of the mirrors was regularly exceeding
// Go's default 10 s TLSHandshakeTimeout — causing builds to fail with
//
//   net/http: TLS handshake timeout
//
// even though curl from the same host completed the handshake (within
// curl's 30 s budget). See PR discussion for the curl evidence.
//
// Fix is two-fold:
//   1. Custom transport with TLSHandshakeTimeout = 25 s, giving slow-but-
//      working mirrors a real chance to finish.
//   2. Up to three attempts with exponential backoff so a failing IP gets
//      a chance to be replaced by Go's resolver on the next dial.
//
// The parsing logic mirrors engine.go's resolveChecksum() rather than
// calling into it — keeping this file fully isolated from the shared
// fetcher means any future tweak here cannot regress 22.04 / 24.04. If
// this approach proves stable, the same retry policy could later be
// lifted up into resolveChecksum() and the per-OS hook removed.
func fetchUbuntu2604Checksum(ctx context.Context, isoURL string) (string, error) {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   25 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   45 * time.Second,
	}

	isoFilename := path.Base(isoURL)
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		hash, err := fetchUbuntu2604ChecksumOnce(ctx, client, ubuntu2604ChecksumURL, isoFilename)
		if err == nil {
			return hash, nil
		}
		lastErr = err
		if attempt < 3 {
			// Exponential backoff: 2 s, 4 s. Adds enough time for transient
			// CDN edge issues to clear without dragging out the build too far.
			backoff := time.Duration(1<<attempt) * time.Second
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
	}
	return "", fmt.Errorf("ubuntu-2604 checksum fetch failed after 3 attempts: %w", lastErr)
}

func fetchUbuntu2604ChecksumOnce(ctx context.Context, client *http.Client, checksumURL, isoFilename string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return "", fmt.Errorf("create checksum request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch checksum: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum URL returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB max — SHA256SUMS files are tiny
	if err != nil {
		return "", fmt.Errorf("read checksum body: %w", err)
	}
	// Ubuntu's SHA256SUMS uses GNU coreutils style: "<hash> *<filename>".
	// Duplicated from engine.go resolveChecksum() to keep this file isolated.
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimPrefix(parts[1], "*")
		if name == isoFilename {
			return "sha256:" + parts[0], nil
		}
	}
	return "", errors.New("checksum for " + isoFilename + " not found in SHA256SUMS")
}

func init() {
	RegisterOSDefinition(OSDefinition{
		ID:             "ubuntu-2604",
		Name:           "Ubuntu 26.04 LTS (Resolute Raccoon)",
		Family:         "ubuntu",
		Version:        "26.04",
		Arch:           "amd64",
		ISOURLPattern:  "https://releases.ubuntu.com/26.04/ubuntu-26.04-live-server-amd64.iso",
		ISOChecksumURL: ubuntu2604ChecksumURL,
		GuestOSType:    "ubuntu64Guest",
		ProxmoxOSType:  "l26",
		MinDiskGB:      20,
		MinMemoryMB:    2048,
		MinCPU:         2,
		BootCommand:    ubuntu2604BootCommandHTTP, // Legacy field, kept for backward compat
		InstallMethod:  "autoinstall",
		// Extended fields (Phase 2)
		BootCommandCD:   ubuntu2604BootCommandCD,
		BootCommandHTTP: ubuntu2604BootCommandHTTP,
		ProvisionerCmds: ubuntu2604ProvisionerCmds,
		// Opt in to the retry-aware fetcher (see fetchUbuntu2604Checksum
		// docstring for why). 22.04 / 24.04 deliberately leave this nil
		// so they continue to use the unchanged engine.go path.
		CustomChecksumFetcher: fetchUbuntu2604Checksum,
	})
}
