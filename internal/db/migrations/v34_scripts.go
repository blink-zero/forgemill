package migrations

var v34ChangePasswordScript = `#!/bin/bash
set -euo pipefail

. /etc/os-release
FAMILY="${ID_LIKE:-$ID}"
USER="${PARAM_USERNAME}"
NEW_PASS="${PARAM_NEW_PASSWORD}"

echo "=== Change VM Password ==="
echo "OS: $PRETTY_NAME (family: $FAMILY)"
echo "Username: $USER"
echo ""

# Verify user exists
if ! id "$USER" &>/dev/null; then
  echo "FAIL: User $USER does not exist"
  exit 1
fi

# Change password
echo ">>> Changing password for $USER..."
echo "${USER}:${NEW_PASS}" | chpasswd

echo "DONE: Password changed successfully for $USER"
echo ""
echo "=== Summary ==="
echo "  User:     $USER"
echo "  Password: changed"
`

var v34AddSSHKeyScript = `#!/bin/bash
set -euo pipefail

. /etc/os-release
FAMILY="${ID_LIKE:-$ID}"
USER="${PARAM_USERNAME}"
SSH_KEY="${PARAM_SSH_PUBLIC_KEY}"

echo "=== Add SSH Authorized Key ==="
echo "OS: $PRETTY_NAME (family: $FAMILY)"
echo "Username: $USER"
echo ""

# Verify user exists
if ! id "$USER" &>/dev/null; then
  echo "FAIL: User $USER does not exist"
  exit 1
fi

# Determine home directory
HOME_DIR=$(getent passwd "$USER" | cut -d: -f6)
if [ -z "$HOME_DIR" ]; then
  echo "FAIL: Could not determine home directory for $USER"
  exit 1
fi

SSH_DIR="${HOME_DIR}/.ssh"
AUTH_KEYS="${SSH_DIR}/authorized_keys"

echo ">>> Setting up SSH authorized key..."
mkdir -p "$SSH_DIR"
chmod 700 "$SSH_DIR"
touch "$AUTH_KEYS"

# Check for duplicate key
if grep -qF "$SSH_KEY" "$AUTH_KEYS" 2>/dev/null; then
  echo "SKIP: SSH key already present in authorized_keys"
else
  echo "$SSH_KEY" >> "$AUTH_KEYS"
  echo "DONE: SSH key added"
fi

chmod 600 "$AUTH_KEYS"
chown -R "${USER}:${USER}" "$SSH_DIR"

echo ""
echo "=== Summary ==="
echo "  User:       $USER"
echo "  SSH Dir:    $SSH_DIR"
echo "  Keys File:  $AUTH_KEYS"
echo "  Key Count:  $(wc -l < "$AUTH_KEYS") key(s)"
`
