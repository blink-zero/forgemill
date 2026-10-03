#!/usr/bin/env bash
# Dump the JSON (sorted keys) of a list of GET endpoints from a running
# instance so two builds can be diffed with `diff -r`. Used to prove backend
# refactors are byte-for-byte identical on the read path.
#   api-snapshot.sh <outdir> /vms /vms/1 "/history?page=1&per_page=5" ...
# Env: E2E_BASE_URL (http://localhost:8097), E2E_ADMIN_USER (admin), E2E_ADMIN_PASSWORD (required)
set -euo pipefail
OUT=$1; shift; mkdir -p "$OUT"
BASE=${E2E_BASE_URL:-http://localhost:8097}
TOKEN=$(curl -s -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"${E2E_ADMIN_USER:-admin}\",\"password\":\"${E2E_ADMIN_PASSWORD:?}\"}" | python3 -c 'import sys,json; print(json.load(sys.stdin)["token"])')
for p in "$@"; do
  curl -s -H "Authorization: Bearer $TOKEN" "$BASE/api$p" \
    | python3 -c 'import sys,json; print(json.dumps(json.load(sys.stdin), indent=1, sort_keys=True))' \
    > "$OUT/$(echo "$p" | tr '/?&=' '____').json"
  sleep 0.3
done
ls "$OUT"
