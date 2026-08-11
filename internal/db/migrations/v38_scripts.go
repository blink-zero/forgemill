package migrations

var v38InstallNginxScript = `#!/bin/bash
set -euo pipefail

echo "=== Install Nginx ==="
echo ""

if [ -f /etc/os-release ]; then
    . /etc/os-release
else
    echo "ERROR: Cannot detect OS (no /etc/os-release)"
    exit 1
fi

echo "Detected OS: ${PRETTY_NAME:-$ID}"
echo ""

if command -v nginx &>/dev/null; then
    echo "Nginx is already installed: $(nginx -v 2>&1)"
    systemctl enable --now nginx
    exit 0
fi

case "${ID_LIKE:-$ID}" in
    *debian*|*ubuntu*|ubuntu|debian)
        export DEBIAN_FRONTEND=noninteractive
        apt-get update -y
        apt-get install -y nginx
        ;;
    *rhel*|*fedora*|*centos*|rocky|almalinux)
        if command -v dnf &>/dev/null; then
            dnf install -y nginx
        elif command -v yum &>/dev/null; then
            yum install -y nginx
        else
            echo "ERROR: Neither dnf nor yum found"
            exit 1
        fi
        ;;
    *suse*)
        zypper --non-interactive install nginx
        ;;
    *)
        echo "ERROR: Unsupported OS family: ${ID_LIKE:-$ID}"
        echo "Supported: Debian/Ubuntu, RHEL/Rocky/CentOS/Fedora/AlmaLinux, SUSE"
        exit 1
        ;;
esac

echo ""
echo ">>> Enabling and starting nginx..."
systemctl enable --now nginx

echo ""
echo "Nginx installed: $(nginx -v 2>&1)"
echo "=== Nginx installation complete ==="
`

var v38InstallPostgresScript = `#!/bin/bash
set -euo pipefail

echo "=== Install PostgreSQL ==="
echo ""

if [ -f /etc/os-release ]; then
    . /etc/os-release
else
    echo "ERROR: Cannot detect OS (no /etc/os-release)"
    exit 1
fi

echo "Detected OS: ${PRETTY_NAME:-$ID}"
echo ""

if command -v psql &>/dev/null; then
    echo "PostgreSQL is already installed: $(psql --version)"
    systemctl enable --now postgresql 2>/dev/null || true
    exit 0
fi

case "${ID_LIKE:-$ID}" in
    *debian*|*ubuntu*|ubuntu|debian)
        export DEBIAN_FRONTEND=noninteractive
        apt-get update -y
        apt-get install -y postgresql postgresql-contrib
        systemctl enable --now postgresql
        ;;
    *rhel*|*fedora*|*centos*|rocky|almalinux)
        if command -v dnf &>/dev/null; then
            dnf install -y postgresql-server postgresql-contrib
        elif command -v yum &>/dev/null; then
            yum install -y postgresql-server postgresql-contrib
        else
            echo "ERROR: Neither dnf nor yum found"
            exit 1
        fi
        if [ ! -d /var/lib/pgsql/data/base ]; then
            echo ">>> Initializing database..."
            postgresql-setup --initdb
        fi
        systemctl enable --now postgresql
        ;;
    *suse*)
        zypper --non-interactive install postgresql postgresql-server
        systemctl enable --now postgresql
        ;;
    *)
        echo "ERROR: Unsupported OS family: ${ID_LIKE:-$ID}"
        echo "Supported: Debian/Ubuntu, RHEL/Rocky/CentOS/Fedora/AlmaLinux, SUSE"
        exit 1
        ;;
esac

echo ""
echo "PostgreSQL installed: $(psql --version)"
echo "=== PostgreSQL installation complete ==="
`

var v38InstallRedisScript = `#!/bin/bash
set -euo pipefail

echo "=== Install Redis ==="
echo ""

if [ -f /etc/os-release ]; then
    . /etc/os-release
else
    echo "ERROR: Cannot detect OS (no /etc/os-release)"
    exit 1
fi

echo "Detected OS: ${PRETTY_NAME:-$ID}"
echo ""

if command -v redis-server &>/dev/null; then
    echo "Redis is already installed: $(redis-server --version)"
    systemctl enable --now redis 2>/dev/null || systemctl enable --now redis-server 2>/dev/null || true
    exit 0
fi

case "${ID_LIKE:-$ID}" in
    *debian*|*ubuntu*|ubuntu|debian)
        export DEBIAN_FRONTEND=noninteractive
        apt-get update -y
        apt-get install -y redis-server
        systemctl enable --now redis-server
        ;;
    *rhel*|*fedora*|*centos*|rocky|almalinux)
        if command -v dnf &>/dev/null; then
            dnf install -y redis
        elif command -v yum &>/dev/null; then
            yum install -y redis
        else
            echo "ERROR: Neither dnf nor yum found"
            exit 1
        fi
        systemctl enable --now redis
        ;;
    *suse*)
        zypper --non-interactive install redis
        systemctl enable --now redis
        ;;
    *)
        echo "ERROR: Unsupported OS family: ${ID_LIKE:-$ID}"
        echo "Supported: Debian/Ubuntu, RHEL/Rocky/CentOS/Fedora/AlmaLinux, SUSE"
        exit 1
        ;;
esac

echo ""
echo "Redis installed: $(redis-server --version)"
echo "=== Redis installation complete ==="
`

var v38InstallNodeScript = `#!/bin/bash
set -euo pipefail

NODE_VERSION="${PARAM_NODE_VERSION:-20}"

echo "=== Install Node.js ==="
echo ""

if [ -f /etc/os-release ]; then
    . /etc/os-release
else
    echo "ERROR: Cannot detect OS (no /etc/os-release)"
    exit 1
fi

echo "Detected OS: ${PRETTY_NAME:-$ID}"
echo "Requested Node.js version: ${NODE_VERSION}.x"
echo ""

if command -v node &>/dev/null; then
    CURRENT=$(node --version)
    echo "Node.js is already installed: $CURRENT"
    if [[ "$CURRENT" == v${NODE_VERSION}.* ]]; then
        echo "Already on requested major version ${NODE_VERSION}.x — nothing to do."
        exit 0
    fi
    echo "Installed version doesn't match requested ${NODE_VERSION}.x — installing requested version..."
fi

case "${ID_LIKE:-$ID}" in
    *debian*|*ubuntu*|ubuntu|debian)
        export DEBIAN_FRONTEND=noninteractive
        echo ">>> Adding NodeSource repository for Node.js ${NODE_VERSION}.x..."
        curl -fsSL "https://deb.nodesource.com/setup_${NODE_VERSION}.x" | bash -
        apt-get install -y nodejs
        ;;
    *rhel*|*fedora*|*centos*|rocky|almalinux)
        echo ">>> Adding NodeSource repository for Node.js ${NODE_VERSION}.x..."
        curl -fsSL "https://rpm.nodesource.com/setup_${NODE_VERSION}.x" | bash -
        if command -v dnf &>/dev/null; then
            dnf install -y nodejs
        else
            yum install -y nodejs
        fi
        ;;
    *)
        echo "ERROR: Unsupported OS family for NodeSource: ${ID_LIKE:-$ID}"
        echo "Supported: Debian/Ubuntu, RHEL/Rocky/CentOS/Fedora/AlmaLinux"
        exit 1
        ;;
esac

echo ""
echo "Node.js installed: $(node --version)"
echo "npm installed:     $(npm --version)"
echo "=== Node.js installation complete ==="
`

var v38InstallMariaDBScript = `#!/bin/bash
set -euo pipefail

echo "=== Install MariaDB (MySQL-compatible) ==="
echo ""

if [ -f /etc/os-release ]; then
    . /etc/os-release
else
    echo "ERROR: Cannot detect OS (no /etc/os-release)"
    exit 1
fi

echo "Detected OS: ${PRETTY_NAME:-$ID}"
echo ""

if command -v mysql &>/dev/null || command -v mariadb &>/dev/null; then
    echo "A MySQL-compatible server is already installed."
    systemctl enable --now mariadb 2>/dev/null || systemctl enable --now mysql 2>/dev/null || true
    exit 0
fi

case "${ID_LIKE:-$ID}" in
    *debian*|*ubuntu*|ubuntu|debian)
        export DEBIAN_FRONTEND=noninteractive
        apt-get update -y
        apt-get install -y mariadb-server
        systemctl enable --now mariadb
        ;;
    *rhel*|*fedora*|*centos*|rocky|almalinux)
        if command -v dnf &>/dev/null; then
            dnf install -y mariadb-server
        elif command -v yum &>/dev/null; then
            yum install -y mariadb-server
        else
            echo "ERROR: Neither dnf nor yum found"
            exit 1
        fi
        systemctl enable --now mariadb
        ;;
    *suse*)
        zypper --non-interactive install mariadb
        systemctl enable --now mariadb
        ;;
    *)
        echo "ERROR: Unsupported OS family: ${ID_LIKE:-$ID}"
        echo "Supported: Debian/Ubuntu, RHEL/Rocky/CentOS/Fedora/AlmaLinux, SUSE"
        exit 1
        ;;
esac

echo ""
echo ">>> Note: run 'mysql_secure_installation' interactively afterward to set a root password and lock down defaults."
command -v mariadb &>/dev/null && mariadb --version || mysql --version
echo "=== MariaDB installation complete ==="
`

var v38ExpandRootFSScript = `#!/bin/bash
set -euo pipefail

echo "=== Expand Root Filesystem ==="
echo ""

ROOT_SRC=$(findmnt -n -o SOURCE /)
FSTYPE=$(findmnt -n -o FSTYPE /)
echo "Root device: $ROOT_SRC"
echo "Filesystem:  $FSTYPE"
echo ""

if command -v lsblk &>/dev/null && lsblk -no TYPE "$ROOT_SRC" 2>/dev/null | grep -q lvm; then
    echo "Root is on an LVM logical volume — growpart/resize2fs alone won't"
    echo "extend an LVM setup. Grow the underlying PV's partition and run"
    echo "'pvresize' + 'lvextend -r' manually instead."
    exit 1
fi

DISK=$(lsblk -no PKNAME "$ROOT_SRC" 2>/dev/null | head -1)
PARTNUM=$(echo "$ROOT_SRC" | grep -oE '[0-9]+$')

if [ -z "$DISK" ] || [ -z "$PARTNUM" ]; then
    echo "ERROR: Could not determine the underlying disk/partition number for $ROOT_SRC"
    exit 1
fi

echo "Underlying disk: /dev/$DISK, partition: $PARTNUM"
echo ""

if ! command -v growpart &>/dev/null; then
    echo ">>> Installing growpart..."
    if [ -f /etc/os-release ]; then . /etc/os-release; fi
    case "${ID_LIKE:-$ID}" in
        *debian*|*ubuntu*|ubuntu|debian)
            export DEBIAN_FRONTEND=noninteractive
            apt-get update -y
            apt-get install -y cloud-guest-utils
            ;;
        *rhel*|*fedora*|*centos*|rocky|almalinux)
            if command -v dnf &>/dev/null; then
                dnf install -y cloud-utils-growpart
            else
                yum install -y cloud-utils-growpart
            fi
            ;;
        *)
            echo "ERROR: growpart not found and this OS family isn't supported for installing it"
            exit 1
            ;;
    esac
fi

echo ">>> Growing partition $PARTNUM on /dev/$DISK..."
growpart "/dev/$DISK" "$PARTNUM" || echo "(already at maximum size, or nothing to grow)"

echo ""
echo ">>> Growing filesystem..."
case "$FSTYPE" in
    ext4|ext3|ext2)
        resize2fs "$ROOT_SRC"
        ;;
    xfs)
        xfs_growfs /
        ;;
    *)
        echo "ERROR: Unsupported filesystem type: $FSTYPE (supported: ext2/ext3/ext4, xfs)"
        exit 1
        ;;
esac

echo ""
df -h /
echo "=== Root filesystem expansion complete ==="
`

var v38ConfigureSwapScript = `#!/bin/bash
set -euo pipefail

SWAP_SIZE_MB="${PARAM_SWAP_SIZE_MB:-2048}"
SWAP_FILE="/swapfile"

echo "=== Configure Swap File ==="
echo ""

if swapon --show 2>/dev/null | grep -q "$SWAP_FILE"; then
    echo "$SWAP_FILE is already active as swap:"
    swapon --show
    exit 0
fi

if [ -f "$SWAP_FILE" ]; then
    echo "ERROR: $SWAP_FILE already exists but isn't active as swap."
    echo "Remove it manually first if you want this action to recreate it."
    exit 1
fi

AVAIL_MB=$(df -Pm / | awk 'NR==2 {print $4}')
if [ "$AVAIL_MB" -lt "$SWAP_SIZE_MB" ]; then
    echo "ERROR: Only ${AVAIL_MB}MB free on /, requested ${SWAP_SIZE_MB}MB swap file"
    exit 1
fi

echo ">>> Creating ${SWAP_SIZE_MB}MB swap file at $SWAP_FILE..."
if command -v fallocate &>/dev/null && fallocate -l "${SWAP_SIZE_MB}M" "$SWAP_FILE" 2>/dev/null; then
    :
else
    dd if=/dev/zero of="$SWAP_FILE" bs=1M count="$SWAP_SIZE_MB" status=progress
fi

chmod 600 "$SWAP_FILE"
mkswap "$SWAP_FILE"
swapon "$SWAP_FILE"

if ! grep -q "^$SWAP_FILE " /etc/fstab; then
    echo "$SWAP_FILE none swap sw 0 0" >> /etc/fstab
    echo ">>> Added $SWAP_FILE to /etc/fstab for persistence across reboots"
fi

echo ""
echo "Swap enabled:"
swapon --show
free -h
echo "=== Swap file configuration complete ==="
`

var v38UnattendedUpgradesScript = `#!/bin/bash
set -euo pipefail

echo "=== Enable Unattended Security Updates ==="
echo ""

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
        apt-get update -y
        apt-get install -y unattended-upgrades apt-listchanges

        cat > /etc/apt/apt.conf.d/20auto-upgrades <<'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
EOF

        echo ">>> Enabling unattended-upgrades service..."
        systemctl enable --now unattended-upgrades
        echo ""
        echo "Configured. Security-relevant packages will now be patched automatically."
        ;;
    *rhel*|*fedora*|*centos*|rocky|almalinux)
        if command -v dnf &>/dev/null; then
            dnf install -y dnf-automatic
            sed -i 's/^upgrade_type = .*/upgrade_type = security/' /etc/dnf/automatic.conf 2>/dev/null || true
            sed -i 's/^apply_updates = .*/apply_updates = yes/' /etc/dnf/automatic.conf 2>/dev/null || true
            systemctl enable --now dnf-automatic.timer
            echo ""
            echo "Configured dnf-automatic for security updates."
        else
            echo "ERROR: dnf-automatic requires dnf; yum-only systems aren't supported by this action"
            exit 1
        fi
        ;;
    *)
        echo "ERROR: Unsupported OS family: ${ID_LIKE:-$ID}"
        echo "Supported: Debian/Ubuntu (unattended-upgrades), RHEL/Rocky/Fedora/AlmaLinux (dnf-automatic)"
        exit 1
        ;;
esac

echo ""
echo "=== Unattended security updates enabled ==="
`

var v38HealthCheckScript = `#!/bin/bash
set -euo pipefail

echo "=== System Health Check ==="
echo ""

DISK_WARN=80
DISK_FAIL=95
MEM_WARN=85
MEM_FAIL=95

FAILURES=0
WARNINGS=0

check() {
    local label="$1" status="$2"
    if [ "$status" = "FAIL" ]; then FAILURES=$((FAILURES+1))
    elif [ "$status" = "WARN" ]; then WARNINGS=$((WARNINGS+1)); fi
    printf "%-34s [%s]\n" "$label" "$status"
}

echo "--- Disk usage ---"
while read -r line; do
    USE=$(echo "$line" | awk '{print $5}' | tr -d '%')
    MOUNT=$(echo "$line" | awk '{print $6}')
    if [ "$USE" -ge "$DISK_FAIL" ]; then
        check "Disk $MOUNT (${USE}%)" "FAIL"
    elif [ "$USE" -ge "$DISK_WARN" ]; then
        check "Disk $MOUNT (${USE}%)" "WARN"
    else
        check "Disk $MOUNT (${USE}%)" "PASS"
    fi
done < <(df -hP -x tmpfs -x devtmpfs | tail -n +2)
echo ""

echo "--- Memory ---"
MEM_USE=$(free | awk '/^Mem:/ {printf "%.0f", $3/$2*100}')
if [ "$MEM_USE" -ge "$MEM_FAIL" ]; then
    check "Memory usage (${MEM_USE}%)" "FAIL"
elif [ "$MEM_USE" -ge "$MEM_WARN" ]; then
    check "Memory usage (${MEM_USE}%)" "WARN"
else
    check "Memory usage (${MEM_USE}%)" "PASS"
fi
echo ""

echo "--- Load average ---"
CORES=$(nproc)
LOAD1=$(cut -d' ' -f1 /proc/loadavg)
LOAD_RATIO=$(awk -v l="$LOAD1" -v c="$CORES" 'BEGIN{printf "%.2f", l/c}')
if awk -v r="$LOAD_RATIO" 'BEGIN{exit !(r>=2)}'; then
    check "Load average ($LOAD1, ${CORES} cores)" "FAIL"
elif awk -v r="$LOAD_RATIO" 'BEGIN{exit !(r>=1)}'; then
    check "Load average ($LOAD1, ${CORES} cores)" "WARN"
else
    check "Load average ($LOAD1, ${CORES} cores)" "PASS"
fi
echo ""

echo "--- Uptime ---"
uptime -p 2>/dev/null || uptime
echo ""

echo "=== Summary: $FAILURES failed, $WARNINGS warning(s) ==="
if [ "$FAILURES" -gt 0 ]; then
    exit 1
fi
`

var v38InstallDockerComposeScript = `#!/bin/bash
set -euo pipefail

echo "=== Install Docker Compose ==="
echo ""

if docker compose version &>/dev/null; then
    echo "Docker Compose plugin is already installed: $(docker compose version)"
    exit 0
fi

if ! command -v docker &>/dev/null; then
    echo "ERROR: Docker isn't installed. Run the 'Install Docker' action first."
    exit 1
fi

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
        apt-get update -y
        apt-get install -y docker-compose-plugin
        ;;
    *rhel*|*fedora*|*centos*|rocky|almalinux)
        if command -v dnf &>/dev/null; then
            dnf install -y docker-compose-plugin
        else
            yum install -y docker-compose-plugin
        fi
        ;;
    *)
        echo "ERROR: Unsupported OS family for the docker-compose-plugin package: ${ID_LIKE:-$ID}"
        echo "Install the plugin manually per Docker's docs for this distro."
        exit 1
        ;;
esac

echo ""
echo "Docker Compose installed: $(docker compose version)"
echo "=== Docker Compose installation complete ==="
`
