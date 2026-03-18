#!/bin/bash
set -euo pipefail

BASE="http://localhost:8080"
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass=0
fail=0
results=()

log() { echo -e "${YELLOW}[TEST]${NC} $*"; }
ok() { echo -e "${GREEN}[PASS]${NC} $*"; pass=$((pass+1)); results+=("PASS: $*"); }
err() { echo -e "${RED}[FAIL]${NC} $*"; fail=$((fail+1)); results+=("FAIL: $*"); }

# Auth
TOKEN=$(curl -sf "$BASE/api/auth/login" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | jq -r '.token')
AUTH="Authorization: Bearer $TOKEN"

api() {
  local method=$1 path=$2
  shift 2
  curl -sf -X "$method" -H "$AUTH" -H 'Content-Type: application/json' "$BASE$path" "$@"
}

# ==========================================
# STEP 1: Add targets
# ==========================================
log "Adding ESXi target..."
ESXI_ID=$(api POST /api/targets -d '{
  "name": "esxi01",
  "type": "esxi",
  "hostname": "192.168.1.100",
  "port": 443,
  "username": "root",
  "password": "SuperSecretPassword_12345",
  "validate_certs": false
}' | jq -r '.id')
if [ "$ESXI_ID" != "null" ] && [ -n "$ESXI_ID" ]; then
  ok "ESXi target added (id: $ESXI_ID)"
else
  err "Failed to add ESXi target"
  exit 1
fi

log "Adding Proxmox target..."
PVE_ID=$(api POST /api/targets -d '{
  "name": "pve01",
  "type": "proxmox",
  "hostname": "192.168.1.101",
  "port": 8006,
  "username": "root@pam",
  "password": "SuperSecretPassword_12345",
  "validate_certs": false
}' | jq -r '.id')
if [ "$PVE_ID" != "null" ] && [ -n "$PVE_ID" ]; then
  ok "Proxmox target added (id: $PVE_ID)"
else
  err "Failed to add Proxmox target"
  exit 1
fi

# Test connections
log "Testing ESXi connection..."
ESXI_TEST=$(api POST "/api/targets/$ESXI_ID/test" 2>&1 || echo '{"error":"failed"}')
echo "$ESXI_TEST" | jq -r '.message // .error' 2>/dev/null || echo "$ESXI_TEST"
if echo "$ESXI_TEST" | jq -e '.message' &>/dev/null; then
  ok "ESXi connection test passed"
else
  err "ESXi connection test failed"
fi

log "Testing Proxmox connection..."
PVE_TEST=$(api POST "/api/targets/$PVE_ID/test" 2>&1 || echo '{"error":"failed"}')
echo "$PVE_TEST" | jq -r '.message // .error' 2>/dev/null || echo "$PVE_TEST"
if echo "$PVE_TEST" | jq -e '.message' &>/dev/null; then
  ok "Proxmox connection test passed"
else
  err "Proxmox connection test failed"
fi

# ==========================================
# STEP 2: Check existing VMs/templates on hypervisors
# ==========================================
log "Listing targets..."
api GET /api/targets | jq '.'

echo ""
echo "=========================================="
echo "STEP 1 COMPLETE: Targets added"
echo "ESXi ID: $ESXI_ID, Proxmox ID: $PVE_ID"
echo "=========================================="
echo ""
echo "Pass: $pass  Fail: $fail"
echo ""
for r in "${results[@]}"; do echo "  $r"; done
