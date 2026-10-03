#!/usr/bin/env bash
# Local/CI driver for the screenshot harness.
#
#   run.sh seed    <workdir>                                  fresh DB + fixtures (starts the server once to migrate)
#   run.sh tour    <binary> <dist> <workdir> <shotdir>        serve <binary>+<dist> on $E2E_PORT against <workdir>/data, run tour.mjs
#   run.sh compare <base-bin> <base-dist> <head-bin> <head-dist> <workdir>
#                                                             seed once, tour both, diff; exit 2 on regression
#
# Env: E2E_PORT (8097), E2E_ADMIN_PASSWORD (E2eVerifyPass123), E2E_THRESHOLD (0.5).
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
PORT=${E2E_PORT:-8097}
export E2E_ADMIN_PASSWORD=${E2E_ADMIN_PASSWORD:-E2eVerifyPass123}
export E2E_BASE_URL="http://localhost:${PORT}"

start() { # binary dist datadir logfile
  FORGEMILL_DATA_DIR="$3" FORGEMILL_LISTEN_ADDR=":${PORT}" FORGEMILL_FRONTEND_PATH="$2" \
  FORGEMILL_ADMIN_PASSWORD="$E2E_ADMIN_PASSWORD" FORGEMILL_LOG_LEVEL=warn "$1" >"$4" 2>&1 &
  SERVER_PID=$!
  for _ in $(seq 1 60); do curl -sf "${E2E_BASE_URL}/api/version" >/dev/null 2>&1 && return 0; sleep 0.5; done
  echo "server did not become ready; log:"; cat "$4"; return 1
}
stop() { if [ -n "${SERVER_PID:-}" ]; then kill "$SERVER_PID" 2>/dev/null || true; wait "$SERVER_PID" 2>/dev/null || true; SERVER_PID=; fi; }
trap stop EXIT

seed() { # workdir binary dist
  local work=$1 bin=$2 dist=$3
  rm -rf "$work/data"; mkdir -p "$work/data"
  start "$bin" "$dist" "$work/data" "$work/seed-server.log"; stop   # creates schema + admin
  python3 "$HERE/seed.py" "$work/data/forgemill.db"
}

tour() { # binary dist workdir shotdir
  local bin=$1 dist=$2 work=$3 shots=$4
  rm -rf "$work/run"; cp -r "$work/data" "$work/run"          # each tour gets an identical copy
  start "$bin" "$dist" "$work/run" "$work/$(basename "$shots").server.log"
  E2E_FIXED_TIME=$(cat "$work/data/seed-time") E2E_SHOT_DIR="$shots" node "$HERE/tour.mjs"
  stop
}

case "${1:-}" in
  seed)    seed "$2" "${3:?binary}" "${4:?dist}" ;;
  tour)    tour "$2" "$3" "$4" "$5" ;;
  compare)
    base_bin=$2 base_dist=$3 head_bin=$4 head_dist=$5 work=$6
    mkdir -p "$work"
    seed "$work" "$head_bin" "$head_dist"
    tour "$base_bin" "$base_dist" "$work" "$work/shots-base"
    tour "$head_bin" "$head_dist" "$work" "$work/shots-head"
    node "$HERE/diff.mjs" "$work/shots-base" "$work/shots-head" "${E2E_THRESHOLD:-0.5}"
    ;;
  *) sed -n 2,12p "$0"; exit 2 ;;
esac
