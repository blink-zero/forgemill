package migrations

var v28SecurityHardeningScript = `#!/bin/bash
set -euo pipefail

. /etc/os-release
FAMILY="${ID_LIKE:-$ID}"
echo "=== Security Hardening ==="
echo "Detected OS: $PRETTY_NAME (family: $FAMILY)"
echo ""

# Apply SSH config setting — handles both main sshd_config and sshd_config.d/ drop-ins
apply_sshd_setting() {
  local key="$1" value="$2"
  local main_conf="/etc/ssh/sshd_config"
  local drop_in_dir="/etc/ssh/sshd_config.d"

  # Check if main config uses Include for drop-in directory
  if grep -qE "^\s*Include\s+.*sshd_config\.d" "$main_conf" 2>/dev/null && [ -d "$drop_in_dir" ]; then
    # Use a drop-in file for clean separation
    echo "${key} ${value}" >> "${drop_in_dir}/99-forgemill-hardening.conf"
  else
    # Edit main config directly
    if grep -qE "^\s*#?\s*${key}\b" "$main_conf" 2>/dev/null; then
      sed -i "s/^#\?\s*${key}.*/${key} ${value}/" "$main_conf"
    else
      echo "${key} ${value}" >> "$main_conf"
    fi
  fi
}

# Clear drop-in if we're using it (fresh start)
if grep -qE "^\s*Include\s+.*sshd_config\.d" /etc/ssh/sshd_config 2>/dev/null && [ -d /etc/ssh/sshd_config.d ]; then
  rm -f /etc/ssh/sshd_config.d/99-forgemill-hardening.conf
fi

# Disable root SSH login
echo ">>> Disabling root SSH login..."
apply_sshd_setting "PermitRootLogin" "no"
echo "DONE: PermitRootLogin no"

# Disable password authentication
echo ">>> Disabling SSH password authentication..."
apply_sshd_setting "PasswordAuthentication" "no"
echo "DONE: PasswordAuthentication no"

# Disable empty passwords
echo ">>> Disabling empty passwords..."
apply_sshd_setting "PermitEmptyPasswords" "no"
echo "DONE: PermitEmptyPasswords no"

# Install and configure firewall
case "$FAMILY" in
  *debian*|*ubuntu*)
    echo ">>> Installing UFW..."
    apt-get update -y -qq
    apt-get install -y -qq ufw
    ufw default deny incoming
    ufw default allow outgoing
    ufw allow ssh
    ufw --force enable
    echo "DONE: UFW enabled with SSH allowed"
    ;;
  *rhel*|*fedora*|*centos*)
    echo ">>> Installing firewalld..."
    dnf install -y -q firewalld || yum install -y -q firewalld
    systemctl enable --now firewalld
    firewall-cmd --permanent --add-service=ssh
    firewall-cmd --reload
    echo "DONE: firewalld enabled with SSH allowed"
    ;;
  *)
    echo "WARN: Unknown OS family, skipping firewall setup"
    ;;
esac

# Install and configure fail2ban
echo ">>> Installing fail2ban..."
case "$FAMILY" in
  *debian*|*ubuntu*)
    apt-get install -y -qq fail2ban
    ;;
  *rhel*|*fedora*|*centos*)
    dnf install -y -q epel-release 2>/dev/null || yum install -y -q epel-release 2>/dev/null || true
    dnf install -y -q fail2ban || yum install -y -q fail2ban
    ;;
esac

cat > /etc/fail2ban/jail.d/sshd.local << 'JAIL'
[sshd]
enabled = true
port = ssh
filter = sshd
maxretry = 5
findtime = 600
bantime = 3600
JAIL

systemctl enable --now fail2ban
echo "DONE: fail2ban enabled with SSH jail"

# Restart sshd
echo ">>> Restarting SSH daemon..."
systemctl restart sshd 2>/dev/null || systemctl restart ssh 2>/dev/null
echo "DONE: sshd restarted"

echo ""
echo "=== Summary ==="
echo "  PermitRootLogin:        no"
echo "  PasswordAuthentication: no"
echo "  PermitEmptyPasswords:   no"
echo "  Firewall:               enabled (SSH allowed)"
echo "  fail2ban:               enabled (SSH jail, 5 retries/10min, 1hr ban)"
`

var v28NetworkValidationScript = `#!/bin/bash
set -uo pipefail

. /etc/os-release
echo "=== Network Connectivity Validation ==="
echo "OS: $PRETTY_NAME"
echo ""

PASS=0
FAIL=0

check() {
  local name="$1"
  shift
  if "$@" > /dev/null 2>&1; then
    echo "PASS: $name"
    PASS=$((PASS + 1))
  else
    echo "FAIL: $name"
    FAIL=$((FAIL + 1))
  fi
}

# Gateway reachability
GW=$(ip route | grep default | awk '{print $3}' | head -1)
if [ -n "$GW" ]; then
  check "Default gateway ($GW) reachable" ping -c 2 -W 3 "$GW"
else
  echo "FAIL: No default gateway found"
  FAIL=$((FAIL + 1))
fi

# DNS resolution — use first available tool
dns_resolve() {
  local target="$1"
  if command -v host > /dev/null 2>&1; then
    host "$target"
  elif command -v dig > /dev/null 2>&1; then
    dig +short "$target"
  elif command -v nslookup > /dev/null 2>&1; then
    nslookup "$target"
  elif command -v getent > /dev/null 2>&1; then
    getent hosts "$target"
  else
    echo "No DNS lookup tool available" >&2
    return 1
  fi
}
check "DNS resolution (google.com)" dns_resolve google.com
check "DNS resolution (localhost hostname)" hostname -f

# NTP sync
if command -v timedatectl > /dev/null 2>&1; then
  NTP_SYNCED=$(timedatectl show -p NTPSynchronized --value 2>/dev/null || echo "N/A")
  if [ "$NTP_SYNCED" = "yes" ]; then
    echo "PASS: NTP synchronized"
    PASS=$((PASS + 1))
  else
    echo "FAIL: NTP not synchronized ($NTP_SYNCED)"
    FAIL=$((FAIL + 1))
  fi
elif command -v chronyc > /dev/null 2>&1; then
  check "NTP sync (chrony)" chronyc tracking
else
  echo "SKIP: No NTP tool found"
fi

# Outbound HTTPS
check "Outbound HTTPS (google.com)" curl -sI --max-time 10 https://google.com

echo ""
echo "=== Network Configuration ==="
echo ""
echo "--- Interfaces ---"
ip addr show 2>/dev/null || ifconfig 2>/dev/null
echo ""
echo "--- Routing Table ---"
ip route show 2>/dev/null || route -n 2>/dev/null
echo ""

echo "=== Results ==="
echo "  Passed: $PASS"
echo "  Failed: $FAIL"
[ "$FAIL" -eq 0 ] && echo "  Status: ALL CHECKS PASSED" || echo "  Status: SOME CHECKS FAILED"
`

var v28DeployMonitoringScript = `#!/bin/bash
set -euo pipefail

. /etc/os-release
FAMILY="${ID_LIKE:-$ID}"
PORT="${PARAM_EXPORTER_PORT:-9100}"

echo "=== Deploy Monitoring Agent (node_exporter) ==="
echo "OS: $PRETTY_NAME (family: $FAMILY)"
echo "Listen port: $PORT"
echo ""

# Create user
echo ">>> Creating node_exporter user..."
useradd --no-create-home --shell /usr/sbin/nologin node_exporter 2>/dev/null || true

# Download latest node_exporter
echo ">>> Downloading node_exporter..."
ARCH=$(uname -m)
case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
esac
DOWNLOAD_URL=$(curl -s https://api.github.com/repos/prometheus/node_exporter/releases/latest | grep "browser_download_url.*linux-${ARCH}.tar.gz" | head -1 | cut -d'"' -f4)
if [ -z "$DOWNLOAD_URL" ]; then
  echo "ERROR: Could not determine download URL for node_exporter"
  exit 1
fi
echo "URL: $DOWNLOAD_URL"
curl -fsSL "$DOWNLOAD_URL" | tar xz -C /tmp
cp /tmp/node_exporter-*/node_exporter /usr/local/bin/
chown node_exporter:node_exporter /usr/local/bin/node_exporter
rm -rf /tmp/node_exporter-*

# Create systemd unit
echo ">>> Creating systemd service..."
cat > /etc/systemd/system/node_exporter.service << UNIT
[Unit]
Description=Prometheus Node Exporter
After=network.target

[Service]
User=node_exporter
Group=node_exporter
Type=simple
ExecStart=/usr/local/bin/node_exporter --web.listen-address=:${PORT}
ProtectSystem=full
NoNewPrivileges=true
ProtectHome=true
ProtectKernelTunables=true

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now node_exporter
echo "DONE: node_exporter service started"

# Open firewall port if firewall is active
if command -v ufw > /dev/null 2>&1 && ufw status | grep -q "Status: active"; then
  ufw allow "$PORT"/tcp
  echo "DONE: Opened port $PORT in UFW"
elif command -v firewall-cmd > /dev/null 2>&1 && systemctl is-active firewalld > /dev/null 2>&1; then
  firewall-cmd --permanent --add-port="${PORT}/tcp"
  firewall-cmd --reload
  echo "DONE: Opened port $PORT in firewalld"
fi

# Verify
echo ""
echo ">>> Verifying..."
sleep 2
if curl -s "http://localhost:${PORT}/metrics" | head -5; then
  echo ""
  echo "=== node_exporter deployed successfully on port $PORT ==="
else
  echo "WARNING: Could not fetch metrics — service may still be starting"
fi
`

var v28LogForwardingScript = `#!/bin/bash
set -euo pipefail

. /etc/os-release
FAMILY="${ID_LIKE:-$ID}"
SERVER="${PARAM_SYSLOG_SERVER}"
PORT="${PARAM_SYSLOG_PORT:-514}"
PROTOCOL="${PARAM_SYSLOG_PROTOCOL:-tcp}"

echo "=== Configure Log Forwarding ==="
echo "OS: $PRETTY_NAME (family: $FAMILY)"
echo "Target: ${PROTOCOL}://${SERVER}:${PORT}"
echo ""

# Install rsyslog if not present
if ! command -v rsyslogd > /dev/null 2>&1; then
  echo ">>> Installing rsyslog..."
  case "$FAMILY" in
    *debian*|*ubuntu*)
      apt-get update -y -qq
      apt-get install -y -qq rsyslog
      ;;
    *rhel*|*fedora*|*centos*)
      dnf install -y -q rsyslog || yum install -y -q rsyslog
      ;;
  esac
fi

# Configure remote forwarding
echo ">>> Configuring rsyslog forwarding..."
if [ "$PROTOCOL" = "tcp" ]; then
  DIRECTIVE="@@${SERVER}:${PORT}"
else
  DIRECTIVE="@${SERVER}:${PORT}"
fi

cat > /etc/rsyslog.d/50-forgemill-forward.conf << RSYSLOG
# Forgemill log forwarding
*.* ${DIRECTIVE}
RSYSLOG

# Configure log rotation
echo ">>> Configuring log rotation..."
cat > /etc/logrotate.d/forgemill-syslog << 'LOGROTATE'
/var/log/syslog /var/log/messages {
    weekly
    rotate 12
    compress
    delaycompress
    missingok
    notifempty
    postrotate
        /usr/lib/rsyslog/rsyslog-rotate 2>/dev/null || systemctl reload rsyslog 2>/dev/null || true
    endscript
}
LOGROTATE

# Restart rsyslog
echo ">>> Restarting rsyslog..."
systemctl enable --now rsyslog
systemctl restart rsyslog

# Send test message
logger -t forgemill-test "Log forwarding configured — target ${PROTOCOL}://${SERVER}:${PORT}"
echo "DONE: Test message sent"

echo ""
echo "=== Configuration Summary ==="
echo "  Server:   ${SERVER}"
echo "  Port:     ${PORT}"
echo "  Protocol: ${PROTOCOL}"
echo "  Config:   /etc/rsyslog.d/50-forgemill-forward.conf"
`

var v28UserProvisioningScript = `#!/bin/bash
set -euo pipefail

. /etc/os-release
FAMILY="${ID_LIKE:-$ID}"
USER="${PARAM_USERNAME}"
SSH_KEY="${PARAM_SSH_PUBLIC_KEY:-}"
SUDO="${PARAM_SUDO_ACCESS:-full}"
EXTRA_GROUPS="${PARAM_GROUPS:-}"

echo "=== User & Access Provisioning ==="
echo "OS: $PRETTY_NAME (family: $FAMILY)"
echo "Username: $USER"
echo ""

# Create user
echo ">>> Creating user $USER..."
useradd -m -s /bin/bash "$USER" 2>/dev/null || echo "User $USER already exists"

# Add to groups
if [ -n "$EXTRA_GROUPS" ]; then
  echo ">>> Adding to groups: $EXTRA_GROUPS"
  IFS=',' read -ra GROUPS_ARR <<< "$EXTRA_GROUPS"
  for grp in "${GROUPS_ARR[@]}"; do
    grp=$(echo "$grp" | xargs)
    groupadd "$grp" 2>/dev/null || true
    usermod -aG "$grp" "$USER" 2>/dev/null || echo "  WARN: Could not add to group $grp"
  done
fi

# Set up SSH key
if [ -n "$SSH_KEY" ]; then
  echo ">>> Setting up SSH authorized key..."
  SSH_DIR="/home/${USER}/.ssh"
  AUTH_KEYS="$SSH_DIR/authorized_keys"
  mkdir -p "$SSH_DIR"
  chmod 700 "$SSH_DIR"
  touch "$AUTH_KEYS"
  # Check if key already exists to avoid duplicates
  if grep -qF "$SSH_KEY" "$AUTH_KEYS" 2>/dev/null; then
    echo "SKIP: SSH key already present in authorized_keys"
  else
    echo "$SSH_KEY" >> "$AUTH_KEYS"
    echo "DONE: SSH key added"
  fi
  chmod 600 "$AUTH_KEYS"
  chown -R "${USER}:${USER}" "$SSH_DIR"
else
  echo "SKIP: No SSH key provided"
fi

# Configure sudo
echo ">>> Configuring sudo access: $SUDO"
case "$SUDO" in
  full)
    # Determine the sudo/wheel group based on OS
    case "$FAMILY" in
      *debian*|*ubuntu*)
        usermod -aG sudo "$USER" 2>/dev/null || true
        ;;
      *rhel*|*fedora*|*centos*)
        usermod -aG wheel "$USER" 2>/dev/null || true
        ;;
    esac
    echo "${USER} ALL=(ALL) NOPASSWD:ALL" > "/etc/sudoers.d/${USER}"
    chmod 440 "/etc/sudoers.d/${USER}"
    echo "DONE: Full sudo (NOPASSWD)"
    ;;
  limited)
    case "$FAMILY" in
      *debian*|*ubuntu*)
        usermod -aG sudo "$USER" 2>/dev/null || true
        ;;
      *rhel*|*fedora*|*centos*)
        usermod -aG wheel "$USER" 2>/dev/null || true
        ;;
    esac
    echo "DONE: Limited sudo (password required)"
    ;;
  none)
    rm -f "/etc/sudoers.d/${USER}" 2>/dev/null || true
    echo "DONE: No sudo access"
    ;;
esac

echo ""
echo "=== Summary ==="
echo "  User:   $USER"
echo "  Groups: $(id -nG "$USER" 2>/dev/null || echo 'N/A')"
echo "  SSH:    $([ -n "$SSH_KEY" ] && echo 'key configured' || echo 'no key')"
echo "  Sudo:   $SUDO"
`
