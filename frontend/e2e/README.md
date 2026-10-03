# UI regression harness

Pixel-diffs every primary route of the app, in both themes, between two builds
running against the same seeded database. It is what gates frontend refactors:
a pure refactor must produce a 0.00 % diff on every page.

```
frontend/e2e/
  seed.py          fixtures for a freshly-migrated DB (fictional lab data), pins a reference time
  tour.mjs         Playwright walk of ~20 states × dark/light → PNGs; fails on uncaught page errors
  diff.mjs         per-image % of differing pixels; writes diff images above the threshold
  run.sh           seed / tour / compare driver (also used by .github/workflows/ui-regression.yml)
  api-snapshot.sh  JSON dumps of GET endpoints for diffing backend refactors
```

## Local use

```bash
# once
cd frontend && npm ci && npx playwright install chromium

# build the two sides
git worktree add /tmp/fm-base origin/dev
(cd /tmp/fm-base && go build -o /tmp/fm-base/forgemill ./cmd/forgemill && cd frontend && npm ci && npm run build)
go build -o /tmp/fm-head ./cmd/forgemill && (cd frontend && npm run build)

# seed once, tour both, diff
frontend/e2e/run.sh compare /tmp/fm-base/forgemill /tmp/fm-base/frontend/dist /tmp/fm-head frontend/dist /tmp/fm-e2e
# → /tmp/fm-e2e/shots-base, shots-head, shots-head/_diff/*.diff.png
```

`npm run e2e -- <args>` is a shortcut for `run.sh`, and `npm run e2e:diff a b` for `diff.mjs`.

## What the tour pins down

- The browser clock is fixed to the seed's reference time (`page.clock.setFixedTime`), so
  relative labels ("12d ago", "Up 12d 15h") are identical on every run.
- Hypervisor-backed calls (`/targets/:id/resources`, `/deploy/preflight`, `/vms/:id/nics`,
  `/vms/:id/credentials`) are answered by in-browser fixtures; everything else is the real API
  on the real build.
- Pauses between pages keep the run under the API rate limit (5 req/s, burst 40 per IP).

## Thresholds

`diff.mjs` ignores per-channel differences below 24/255 (anti-aliasing) and fails above
0.5 % of pixels per image. Text re-flow from a changed label is well above that; a
one-pixel border change is well below it.

## Backend parity

```bash
E2E_ADMIN_PASSWORD=… frontend/e2e/api-snapshot.sh /tmp/api-base /vms /vms/1 /history "/history?status=completed"
# switch binaries, re-run into /tmp/api-head, then
diff -r /tmp/api-base /tmp/api-head
```
