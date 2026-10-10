#!/usr/bin/env bash
# Records the README demo: seeds a database (unless <workdir>/data exists),
# serves <binary>+<dist> against a copy of it, drives demo.mjs with video
# recording on, then renders docs/demo.gif (and an mp4 next to the .webm).
#
#   demo.sh <binary> <dist> <workdir>
#
# Env: E2E_PORT, E2E_ADMIN_PASSWORD (as run.sh); FFMPEG and GIFSICLE (default:
# on PATH — Playwright's bundled ffmpeg cannot write GIFs, so point FFMPEG at a
# full build, e.g. from the ffmpeg-static npm package, and GIFSICLE at the
# gifsicle npm package's binary); DEMO_SHOTS, DEMO_FAST (see demo.mjs).
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../.." && pwd)
bin=${1:?binary} dist=${2:?dist} work=${3:?workdir}
# shellcheck source=run.sh
source "$HERE/run.sh"

mkdir -p "$work"
[ -f "$work/data/seed-time" ] || seed "$work" "$bin" "$dist"
rm -rf "$work/run" "$work/video"; cp -r "$work/data" "$work/run"
start "$bin" "$dist" "$work/run" "$work/demo.server.log"
E2E_FIXED_TIME=$(cat "$work/data/seed-time") DEMO_OUT="$work/video" node "$HERE/demo.mjs"
stop

FF=${FFMPEG:-ffmpeg}
src="$work/video/demo.webm"
# GIF: 5 fps, 920 px wide, a 72-colour palette without dithering (the UI is
# flat, dithering only adds noise that defeats frame differencing), then
# gifsicle's lossy optimisation, which roughly halves it again. Two ffmpeg
# passes — palette file, then encode — so frames stream instead of the whole
# clip sitting in memory (the single-pass split/palettegen form gets
# OOM-killed on a three-minute recording). The first half second is the
# blank page before the login renders.
scale="fps=5,scale=920:-1:flags=lanczos"
"$FF" -y -loglevel error -ss 0.5 -i "$src" -vf "$scale,palettegen=max_colors=72:stats_mode=full" "$work/video/palette.png"
"$FF" -y -loglevel error -ss 0.5 -i "$src" -i "$work/video/palette.png" \
  -lavfi "$scale[x];[x][1:v]paletteuse=dither=none:diff_mode=rectangle" \
  "$work/video/demo-raw.gif"
"${GIFSICLE:-gifsicle}" -O3 --lossy=100 "$work/video/demo-raw.gif" -o "$ROOT/docs/demo.gif"
"$FF" -y -loglevel error -ss 0.5 -i "$src" -c:v libx264 -preset slow -crf 22 -pix_fmt yuv420p -movflags +faststart "$work/video/demo.mp4"
ls -la "$ROOT/docs/demo.gif" "$work/video/demo.mp4"
