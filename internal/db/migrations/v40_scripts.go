package migrations

// v40FormatMountDiskScript partitions, formats and mounts a disk that was
// attached with "Add Disk" (or any other unused disk). It is deliberately
// conservative: it never touches a device that already has partitions or a
// filesystem, and auto-detection refuses to guess when more than one disk
// qualifies.
var v40FormatMountDiskScript = `#!/bin/bash
set -euo pipefail

echo "=== Format and Mount New Disk ==="
echo ""

DEVICE="${PARAM_DEVICE:-}"
FSTYPE="${PARAM_FILESYSTEM:-ext4}"
MOUNT_POINT="${PARAM_MOUNT_POINT:-/data}"
LABEL="${PARAM_LABEL:-}"

case "$FSTYPE" in ext4|xfs) ;; *) echo "ERROR: FILESYSTEM must be ext4 or xfs (got '$FSTYPE')"; exit 1;; esac
case "$MOUNT_POINT" in /*) ;; *) echo "ERROR: MOUNT_POINT must be an absolute path (got '$MOUNT_POINT')"; exit 1;; esac

# Already done? (re-running the action must be harmless)
if findmnt -n "$MOUNT_POINT" >/dev/null 2>&1; then
    echo "$MOUNT_POINT is already mounted:"
    findmnt -n -o SOURCE,FSTYPE,SIZE,USED "$MOUNT_POINT"
    echo "Nothing to do."
    exit 0
fi

ROOT_SRC=$(findmnt -n -o SOURCE / || true)
ROOT_DISK=""
if [ -n "$ROOT_SRC" ]; then
    ROOT_DISK=$(lsblk -no PKNAME "$ROOT_SRC" 2>/dev/null | head -1 || true)
fi

# A disk qualifies when it is a whole disk (not a partition/LVM/rom), has no
# partitions and carries no filesystem or LVM/RAID signature.
is_unused_disk() {
    local dev="$1" name
    name=$(basename "$dev")
    [ "$(lsblk -dn -o TYPE "$dev" 2>/dev/null)" = "disk" ] || return 1
    [ -z "$(lsblk -dn -o FSTYPE "$dev" 2>/dev/null)" ] || return 1
    [ "$(lsblk -n -o NAME "$dev" 2>/dev/null | wc -l)" -eq 1 ] || return 1
    [ "$name" != "$ROOT_DISK" ] || return 1
    return 0
}

if [ -z "$DEVICE" ]; then
    echo ">>> No device given — looking for an unused disk..."
    CANDIDATES=()
    while read -r name; do
        if is_unused_disk "/dev/$name"; then CANDIDATES+=("/dev/$name"); fi
    done < <(lsblk -dn -o NAME,TYPE | awk '$2=="disk"{print $1}')
    if [ "${#CANDIDATES[@]}" -eq 0 ]; then
        echo "ERROR: no unused disk found (every disk has partitions or a filesystem)."
        echo "Disks seen:"; lsblk -d -o NAME,SIZE,TYPE,FSTYPE
        exit 1
    fi
    if [ "${#CANDIDATES[@]}" -gt 1 ]; then
        echo "ERROR: more than one unused disk — pass DEVICE explicitly:"
        for c in "${CANDIDATES[@]}"; do lsblk -d -o NAME,SIZE "$c" | tail -1; done
        exit 1
    fi
    DEVICE="${CANDIDATES[0]}"
    echo "Detected: $DEVICE ($(lsblk -dn -o SIZE "$DEVICE"))"
else
    [ -b "$DEVICE" ] || { echo "ERROR: $DEVICE is not a block device"; exit 1; }
    if ! is_unused_disk "$DEVICE"; then
        echo "ERROR: refusing to format $DEVICE — it is the root disk, already has partitions, or carries a filesystem:"
        lsblk -o NAME,SIZE,TYPE,FSTYPE,MOUNTPOINTS "$DEVICE"
        exit 1
    fi
fi
echo ""

# Tools: parted (partitioning) and the mkfs for the chosen filesystem.
need_pkgs=()
command -v parted >/dev/null 2>&1 || need_pkgs+=(parted)
if [ "$FSTYPE" = "xfs" ]; then command -v mkfs.xfs >/dev/null 2>&1 || need_pkgs+=(xfsprogs); fi
if [ "$FSTYPE" = "ext4" ]; then command -v mkfs.ext4 >/dev/null 2>&1 || need_pkgs+=(e2fsprogs); fi
if [ "${#need_pkgs[@]}" -gt 0 ]; then
    echo ">>> Installing: ${need_pkgs[*]}"
    if [ -f /etc/os-release ]; then . /etc/os-release; fi
    case "${ID_LIKE:-$ID}" in
        *debian*|*ubuntu*|ubuntu|debian)
            export DEBIAN_FRONTEND=noninteractive
            apt-get update -y >/dev/null
            apt-get install -y "${need_pkgs[@]}"
            ;;
        *rhel*|*fedora*|*centos*|rocky|almalinux)
            if command -v dnf >/dev/null 2>&1; then dnf install -y "${need_pkgs[@]}"; else yum install -y "${need_pkgs[@]}"; fi
            ;;
        *) echo "ERROR: missing ${need_pkgs[*]} and this OS family isn't supported for installing them"; exit 1;;
    esac
fi

echo ">>> Partitioning $DEVICE (GPT, one partition)..."
parted -s "$DEVICE" mklabel gpt mkpart primary 1MiB 100%
partprobe "$DEVICE" 2>/dev/null || true
udevadm settle 2>/dev/null || sleep 2
PART=$(lsblk -n -o PATH "$DEVICE" | sed -n 2p)
if [ -z "$PART" ] || [ ! -b "$PART" ]; then
    echo "ERROR: partition did not appear after partitioning $DEVICE"; exit 1
fi
echo "Partition: $PART"

echo ">>> Creating $FSTYPE filesystem..."
LABEL_ARGS=()
if [ -n "$LABEL" ]; then LABEL_ARGS=(-L "$LABEL"); fi
case "$FSTYPE" in
    ext4) mkfs.ext4 -q -F "${LABEL_ARGS[@]}" "$PART" ;;
    xfs)  mkfs.xfs -q -f "${LABEL_ARGS[@]}" "$PART" ;;
esac

UUID=$(blkid -s UUID -o value "$PART")
[ -n "$UUID" ] || { echo "ERROR: could not read the new filesystem's UUID"; exit 1; }

echo ">>> Mounting at $MOUNT_POINT (persisted in /etc/fstab by UUID, nofail)..."
mkdir -p "$MOUNT_POINT"
if ! grep -q "UUID=$UUID" /etc/fstab; then
    echo "UUID=$UUID $MOUNT_POINT $FSTYPE defaults,nofail 0 2" >> /etc/fstab
fi
if command -v systemctl >/dev/null 2>&1; then systemctl daemon-reload 2>/dev/null || true; fi
mount "$MOUNT_POINT"

echo ""
echo "✓ $PART ($FSTYPE, UUID $UUID) mounted at $MOUNT_POINT"
df -h "$MOUNT_POINT"
`

// v40ConfigureInterfaceScript brings up an interface that was attached with
// "Add Network Adapter". It writes a netplan drop-in on netplan systems and
// uses NetworkManager (nmcli) elsewhere, and never touches the interface
// that carries the default route.
var v40ConfigureInterfaceScript = `#!/bin/bash
set -euo pipefail

echo "=== Configure New Network Interface ==="
echo ""

IFACE="${PARAM_INTERFACE:-}"
MODE="${PARAM_MODE:-dhcp}"
ADDRESS="${PARAM_ADDRESS:-}"
GATEWAY="${PARAM_GATEWAY:-}"
DNS="${PARAM_DNS:-}"

case "$MODE" in dhcp|static) ;; *) echo "ERROR: MODE must be dhcp or static (got '$MODE')"; exit 1;; esac
if [ "$MODE" = "static" ]; then
    case "$ADDRESS" in */*) ;; *) echo "ERROR: ADDRESS must be in CIDR form for static mode, e.g. 10.0.0.5/24 (got '$ADDRESS')"; exit 1;; esac
fi

PRIMARY=$(ip -o route show default 2>/dev/null | awk '{print $5}' | head -1 || true)
echo "Primary interface (default route): ${PRIMARY:-none}"

has_ipv4() { [ -n "$(ip -4 -o addr show dev "$1" 2>/dev/null)" ]; }

if [ -z "$IFACE" ]; then
    echo ">>> No interface given — looking for one without an IPv4 address..."
    CANDIDATES=()
    while read -r name; do
        name="${name%%@*}"
        [ "$name" = "lo" ] && continue
        [ "$name" = "$PRIMARY" ] && continue
        [ -d "/sys/class/net/$name/device" ] || continue   # physical/virtual NIC, not a bridge/veth
        has_ipv4 "$name" && continue
        CANDIDATES+=("$name")
    done < <(ip -o link show | awk -F': ' '{print $2}')
    if [ "${#CANDIDATES[@]}" -eq 0 ]; then
        echo "ERROR: no unconfigured interface found."; echo "Interfaces:"; ip -br addr; exit 1
    fi
    if [ "${#CANDIDATES[@]}" -gt 1 ]; then
        echo "ERROR: more than one unconfigured interface — pass INTERFACE explicitly: ${CANDIDATES[*]}"; exit 1
    fi
    IFACE="${CANDIDATES[0]}"
    echo "Detected: $IFACE ($(cat /sys/class/net/$IFACE/address 2>/dev/null || echo 'no MAC'))"
else
    [ -d "/sys/class/net/$IFACE" ] || { echo "ERROR: interface $IFACE does not exist"; ip -br link; exit 1; }
    if [ "$IFACE" = "$PRIMARY" ]; then
        echo "ERROR: $IFACE carries the default route — refusing to reconfigure the primary interface"; exit 1
    fi
fi
echo ""

DNS_LIST=()
for d in $DNS; do DNS_LIST+=("$d"); done

if command -v netplan >/dev/null 2>&1 && [ -d /etc/netplan ]; then
    FILE="/etc/netplan/60-forgemill-$IFACE.yaml"
    echo ">>> Writing netplan drop-in $FILE..."
    {
        echo "network:"
        echo "  version: 2"
        echo "  ethernets:"
        echo "    $IFACE:"
        if [ "$MODE" = "dhcp" ]; then
            echo "      dhcp4: true"
            # A second DHCP default route would compete with the primary; keep it lower priority.
            echo "      dhcp4-overrides:"
            echo "        route-metric: 200"
        else
            echo "      dhcp4: false"
            echo "      addresses: [$ADDRESS]"
            if [ -n "$GATEWAY" ]; then
                echo "      routes:"
                echo "        - to: default"
                echo "          via: $GATEWAY"
                echo "          metric: 200"
            fi
            if [ "${#DNS_LIST[@]}" -gt 0 ]; then
                echo "      nameservers:"
                echo "        addresses: [$(IFS=,; echo "${DNS_LIST[*]}")]"
            fi
        fi
    } > "$FILE"
    chmod 600 "$FILE"
    netplan generate
    netplan apply
elif command -v nmcli >/dev/null 2>&1 && nmcli -t general status >/dev/null 2>&1; then
    CON="forgemill-$IFACE"
    echo ">>> Configuring with NetworkManager (connection $CON)..."
    nmcli -t -f NAME con show | grep -qx "$CON" && nmcli con delete "$CON" >/dev/null
    if [ "$MODE" = "dhcp" ]; then
        nmcli con add type ethernet ifname "$IFACE" con-name "$CON" ipv4.method auto ipv4.route-metric 200 >/dev/null
    else
        ARGS=(ipv4.method manual ipv4.addresses "$ADDRESS")
        [ -n "$GATEWAY" ] && ARGS+=(ipv4.gateway "$GATEWAY" ipv4.route-metric 200)
        [ "${#DNS_LIST[@]}" -gt 0 ] && ARGS+=(ipv4.dns "$(IFS=,; echo "${DNS_LIST[*]}")")
        nmcli con add type ethernet ifname "$IFACE" con-name "$CON" "${ARGS[@]}" >/dev/null
    fi
    nmcli con up "$CON" >/dev/null
else
    echo "ERROR: neither netplan nor NetworkManager is available — configure $IFACE manually (ifupdown / systemd-networkd)."
    exit 1
fi

echo ""
# Give DHCP a moment before reporting.
for _ in 1 2 3 4 5 6 7 8 9 10; do has_ipv4 "$IFACE" && break; sleep 1; done
echo "✓ $IFACE configured ($MODE):"
ip -br addr show dev "$IFACE"
[ "$MODE" = "dhcp" ] && ! has_ipv4 "$IFACE" && echo "WARNING: no IPv4 lease yet — check the DHCP server on this network"
exit 0
`

// v40BuiltinActions are the guest-side follow-through for Add Disk and
// Add Network Adapter.
var v40BuiltinActions = []struct {
	name, description, category, script, parameters, tags string
}{
	{
		name:        "Format and Mount New Disk",
		description: "Partition, format and mount a disk that was attached with Add Disk (or any unused disk). Auto-detects the single unused disk, refuses anything that already has partitions or a filesystem, and persists the mount in /etc/fstab by UUID.",
		category:    "scripts",
		script:      v40FormatMountDiskScript,
		parameters:  `[{"name":"DEVICE","label":"Device","type":"string","required":false,"default":"","placeholder":"auto-detect (e.g. /dev/sdb)","options":null,"description":"Block device to format. Leave empty to auto-detect the one unused disk; required when more than one qualifies."},{"name":"FILESYSTEM","label":"Filesystem","type":"select","required":false,"default":"ext4","placeholder":"","options":["ext4","xfs"],"description":"Filesystem to create"},{"name":"MOUNT_POINT","label":"Mount Point","type":"string","required":false,"default":"/data","placeholder":"/data","options":null,"description":"Absolute path to mount the new filesystem at (created if missing)"},{"name":"LABEL","label":"Label","type":"string","required":false,"default":"","placeholder":"data","options":null,"description":"Optional filesystem label"}]`,
		tags:        `["disk","storage","mount","filesystem","format"]`,
	},
	{
		name:        "Configure New Network Interface",
		description: "Bring up an interface that was attached with Add Network Adapter: DHCP or a static address, via netplan (Ubuntu/Debian) or NetworkManager (Rocky/Alma). Auto-detects the single interface without an address and never touches the one carrying the default route.",
		category:    "scripts",
		script:      v40ConfigureInterfaceScript,
		parameters:  `[{"name":"INTERFACE","label":"Interface","type":"string","required":false,"default":"","placeholder":"auto-detect (e.g. ens192)","options":null,"description":"Interface to configure. Leave empty to auto-detect the one interface without an IPv4 address."},{"name":"MODE","label":"Mode","type":"select","required":false,"default":"dhcp","placeholder":"","options":["dhcp","static"],"description":"DHCP or a static address"},{"name":"ADDRESS","label":"Address (CIDR)","type":"string","required":false,"default":"","placeholder":"10.0.1.5/24","options":null,"description":"Static mode only: IPv4 address with prefix length"},{"name":"GATEWAY","label":"Gateway","type":"string","required":false,"default":"","placeholder":"10.0.1.1","options":null,"description":"Optional; added as a lower-priority default route (metric 200) so the primary interface keeps precedence"},{"name":"DNS","label":"DNS Servers","type":"string","required":false,"default":"","placeholder":"10.0.1.53 10.0.1.54","options":null,"description":"Optional, space-separated"}]`,
		tags:        `["network","interface","netplan","networkmanager","dhcp"]`,
	},
}
