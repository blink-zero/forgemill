package migrations

var v29UpdatePackagesScript = `#!/bin/bash
set -euo pipefail

echo "=== Update System Packages ==="
echo ""

# Detect OS family
if [ -f /etc/os-release ]; then
    . /etc/os-release
else
    echo "ERROR: Cannot detect OS (no /etc/os-release)"
    exit 1
fi

echo "Detected OS: ${PRETTY_NAME:-$ID}"
echo ""

case "${ID_LIKE:-$ID}" in
    *debian*|*ubuntu*|ubuntu|debian)
        export DEBIAN_FRONTEND=noninteractive
        echo ">>> Running apt-get update..."
        apt-get update -y
        echo ""
        echo ">>> Running apt-get upgrade..."
        apt-get upgrade -y
        echo ""
        echo ">>> Running apt-get autoremove..."
        apt-get autoremove -y
        ;;
    *rhel*|*fedora*|*centos*|rocky|almalinux)
        if command -v dnf &>/dev/null; then
            echo ">>> Running dnf upgrade..."
            dnf upgrade -y
            echo ""
            echo ">>> Running dnf autoremove..."
            dnf autoremove -y
        elif command -v yum &>/dev/null; then
            echo ">>> Running yum update..."
            yum update -y
        else
            echo "ERROR: Neither dnf nor yum found"
            exit 1
        fi
        ;;
    *suse*)
        echo ">>> Running zypper update..."
        zypper --non-interactive update
        ;;
    *)
        echo "ERROR: Unsupported OS family: ${ID_LIKE:-$ID}"
        echo "Supported: Debian/Ubuntu, RHEL/Rocky/CentOS/Fedora/AlmaLinux, SUSE"
        exit 1
        ;;
esac

echo ""
echo "=== System packages updated successfully ==="
`

var v29InstallDockerScript = `#!/bin/bash
set -euo pipefail

echo "=== Install Docker Engine ==="
echo ""

# Detect OS family
if [ -f /etc/os-release ]; then
    . /etc/os-release
else
    echo "ERROR: Cannot detect OS (no /etc/os-release)"
    exit 1
fi

echo "Detected OS: ${PRETTY_NAME:-$ID}"
echo ""

# Check if Docker is already installed
if command -v docker &>/dev/null; then
    echo "Docker is already installed: $(docker --version)"
    echo "Ensuring service is enabled..."
    systemctl enable --now docker
    exit 0
fi

# The official get.docker.com script supports:
# Ubuntu, Debian, CentOS, Fedora, RHEL, SLES, Raspbian
echo ">>> Installing Docker via official install script..."
curl -fsSL https://get.docker.com | sh

echo ""
echo ">>> Enabling Docker service..."
systemctl enable --now docker

echo ""
echo "Docker installed: $(docker --version)"
echo "=== Docker installation complete ==="
`

var v29CollectVMInfoScript = `#!/bin/bash
set -euo pipefail

echo "=== VM System Information ==="
echo ""

# OS Info
echo "--- OS ---"
if [ -f /etc/os-release ]; then
    . /etc/os-release
    echo "Name:    ${PRETTY_NAME:-$ID}"
    echo "ID:      $ID"
    echo "Version: ${VERSION_ID:-unknown}"
    echo "Family:  ${ID_LIKE:-$ID}"
else
    echo "OS: $(uname -s) $(uname -r)"
fi
echo ""

# Hostname & Uptime
echo "--- Host ---"
echo "Hostname: $(hostname -f 2>/dev/null || hostname)"
echo "Uptime:   $(uptime -p 2>/dev/null || uptime)"
echo "Kernel:   $(uname -r)"
echo ""

# CPU
echo "--- CPU ---"
echo "Cores:  $(nproc)"
CPUMODEL=$(grep -m1 'model name' /proc/cpuinfo 2>/dev/null | cut -d: -f2 | xargs || echo 'unknown')
echo "Model:  $CPUMODEL"
echo ""

# Memory
echo "--- Memory ---"
free -h 2>/dev/null || head -3 /proc/meminfo
echo ""

# Disk
echo "--- Disk ---"
df -h / /home 2>/dev/null | head -5
echo ""

# Network
echo "--- Network ---"
ip -4 addr show 2>/dev/null | grep -E "inet |^[0-9]" | head -10
echo ""
GATEWAY=$(ip route show default 2>/dev/null | awk '{print $3}' | head -1)
DNS=$(grep nameserver /etc/resolv.conf 2>/dev/null | awk '{print $2}' | tr '\n' ' ')
echo "Default Gateway: ${GATEWAY:-unknown}"
echo "DNS Servers:     ${DNS:-unknown}"
echo ""

# Timezone
echo "--- Timezone ---"
TZ=$(timedatectl show -p Timezone --value 2>/dev/null || cat /etc/timezone 2>/dev/null || echo 'unknown')
NTP=$(timedatectl show -p NTPSynchronized --value 2>/dev/null || echo 'unknown')
echo "Timezone: $TZ"
echo "NTP Sync: $NTP"
echo ""

# Services
echo "--- Key Services ---"
for svc in docker sshd fail2ban ufw firewalld node_exporter rsyslog; do
    if systemctl is-enabled "$svc" &>/dev/null; then
        status=$(systemctl is-active "$svc" 2>/dev/null || echo "unknown")
        echo "$svc: $status"
    fi
done
echo ""

echo "=== Collection complete ==="
`

var v30SetTimezoneScript = `#!/bin/bash
set -euo pipefail

TIMEZONE="${PARAM_TIMEZONE:-UTC}"

echo "=== Set Timezone ==="
echo ""

# Validate timezone exists
if ! timedatectl list-timezones 2>/dev/null | grep -qx "$TIMEZONE"; then
    echo "ERROR: Invalid timezone '$TIMEZONE'"
    echo ""
    echo "Examples: UTC, Australia/Sydney, America/New_York, Europe/London"
    echo "Run 'timedatectl list-timezones' on a VM to see all options."
    exit 1
fi

echo ">>> Setting timezone to $TIMEZONE..."
timedatectl set-timezone "$TIMEZONE"

echo ""
CURRENT=$(timedatectl show -p Timezone --value 2>/dev/null || cat /etc/timezone 2>/dev/null || echo 'unknown')
echo "Timezone set to: $CURRENT"
echo "Local time:      $(date)"
echo ""
echo "=== Done ==="
`
