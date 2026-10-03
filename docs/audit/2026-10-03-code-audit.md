# Forgemill code audit — 2026-10-03

Scope: the whole repository at `dev` `772c07a` (Go backend `internal/`, `cmd/`; React frontend `frontend/src`). Constraint for everything below: **100 % behavioural parity** — no public API, config contract, CLI flag, runtime behaviour or output format changes. Every remediation is a refactor that preserves business logic; where a finding *is* a latent behavioural bug, it is labelled as such and left as a recommendation rather than folded into a refactor.

How the audit was done: `staticcheck`, `gocyclo`, `ineffassign`, `go test -cover` on the backend; `tsc --noUnusedLocals --noUnusedParameters`, `depcheck`, and route/usage greps on the frontend; then manual reading of every hot spot the tooling pointed at (SyncAll, the Proxmox client, the WebSocket hubs, the executor/SSH path, the DB layer, the three largest React pages).

**Baseline numbers**

| | |
|---|---|
| Go | 26 300 LOC non-test · largest files `db/sqlite.go` 2 331, `provider/proxmox/client.go` 1 865, `provider/vmware/deploy.go` 1 113 |
| Go cyclomatic > 20 | 17 functions; top: `runMigrations` 72 (sequential, acceptable), `vmware.DeployVM` 68, `factory.executeBuild` 42, `proxmox.GetTemplateDetail` 41, `proxmox.DeployVM` 39 |
| Go test coverage | handlers 2.5 % · vmware 2.2 % · db 12.8 % · service 13.4 % · factory 15.1 % · middleware 16.9 % · proxmox 19.8 % |
| staticcheck | 5 findings (1 deprecated API, 3 unused functions, 1 style) · ineffassign clean |
| Frontend | 9 767 LOC in pages; `VMDetail.tsx` 1 580, `Settings.tsx` 1 319, `Templates.tsx` 1 261 |
| Frontend strictness | 26 unused symbols; 11 explicit `any`; 58 `as` casts (most benign `e.target as Node`); 37 swallowed catches; 0 unit tests, no test runner configured |
| Dead weight | 2 unrouted pages (`Blueprints.tsx`, `BulkDeploy.tsx`); 5 unused `@radix-ui/*` dependencies; 3 unused Go functions |

---

## 1. Executive summary — top five areas, ranked by risk vs. reward

| # | Area | Reward | Risk | Why it ranks here |
|---|---|---|---|---|
| **1** | **Hypervisor round-trip amplification in `SyncAll`** (`service/vm.go`, `provider/proxmox`, `provider/vmware`) | High — sync time is 3×N API calls on Proxmox and 1+N heavyweight calls on vSphere; the periodic/auto sync runs this after every mutation | Low — pure data-flow change inside the provider; same results | Every Proxmox `GetVMStatus` re-fetches `/cluster/resources` to find the node (20 call sites do this), then `status/current`, then the agent — for 30 VMs that is ~90 requests per sync. On vSphere, `ListVMs` already returns power state, IP, CPU, memory and guest id for every VM, yet `SyncAll` then calls `GetVMStatus` per VM and re-fetches the same data. `ListVMs` also pulls the entire `config` property (devices, extra config, …) when it needs five fields. |
| **2** | **Error contracts by string matching** (`handlers/vms.go`, `templates.go`, `settings.go`, `factory.go`, `db/sqlite.go`, `vmware/deploy.go`) | Medium-high — a reworded message silently turns a 404/409 into a 500; the DB uniqueness check depends on the driver's English text | Low — the sentinel-error pattern already exists in the codebase (NIC endpoints) and the handler mapping is mechanical | Six handler sites branch on `strings.Contains(err.Error(), …)`; `isUniqueConstraintError` greps for `"UNIQUE constraint failed"` instead of checking the SQLite error code; `isNotSupportedError` greps govmomi text. |
| **3** | **Frontend resilience** (`pages/*.tsx`, hooks) | Medium — silent failures and a leaked WebSocket are the class of bug behind this week's "Add Network Adapter vanished" report | Low — additive: error surfacing, cleanup effects, a shared polling hook | 37 `catch {}`/`.catch(() => {})` sites, eight `catch (e: any)` in Settings; the Actions-tab WebSocket is never closed when the component unmounts mid-execution; two pollers keep firing in background tabs; two pages and five dependencies are dead. |
| **4** | **Decomposition of the largest units** (`VMDetail.tsx`, `Settings.tsx`, `Templates.tsx`; `vmware.DeployVM`, `proxmox.GetTemplateDetail`/`DeployVM`; `db/sqlite.go` scan boilerplate) | Medium — review and change cost on these files is now the dominant maintenance tax | Medium — mechanical but wide; must be gated on the screenshot-diff harness and provider doubles already in the repo | `VMDetail.tsx` holds three components in one 1 580-line file; `sqlite.go` hand-writes the same 15–20 column `Scan` for `managed_vms` three times and for `deployments` four times. |
| **5** | **Zero-behaviour hardening & dead code** | Medium (security posture) | Very low | `chi/middleware.RealIP` is deprecated and, as wired, trusts `X-Forwarded-For` from *any* client once `FORGEMILL_TRUSTED_PROXIES` is set — the configured proxy list is never consulted. Plus three unused Go functions, `Blueprints`/`BulkDeploy` pages that no route reaches, five unused Radix packages, and 26 unused imports/locals. |

Two latent **behavioural** bugs surfaced that are *not* refactors and are called out separately in §2 (R-6, R-7): the disk-expand guard compares against the VM's *total* disk size, and `?tab=` deep links were fixed this week but the same pattern (state initialised once, URL ignored) exists in `Settings.tsx` tabs.

---

## 2. Findings

Severity: **H** high, **M** medium, **L** low. Category: **Q** quality/maintainability, **R** reliability, **P** performance, **T** typing/strictness, **S** security hardening.

### Performance

| ID | Finding | Scope | Impact | Non-breaking remediation |
|---|---|---|---|---|
| P-1 **H** | `resolveVMNode` fetches `/cluster/resources?type=vm` on every call; 20 call sites, so every status/power/disk/NIC operation pays a cluster-wide listing first | `provider/proxmox/client.go:1550`, all `node = p.node` fallbacks | 3× request amplification on Proxmox; `SyncAll` over N VMs issues ~3N calls; cluster listing cost grows with cluster size | Cache `vmid → node` on the provider for the lifetime of a `Connect()`/`Disconnect()` cycle (one map + mutex, invalidated on a miss). Same results, one listing per sync. See diff A. |
| P-2 **H** | `SyncAll` calls `GetVMStatus` per VM after `ListVMs` has already returned power state, IP, CPU, memory and guest id for every VM | `service/vm.go:382` | 1+N provider calls per target where 1 would do for the common fields; on vSphere each `GetVMStatus` is a property-collector round-trip | Use the `VMInfo` from `ListVMs` for power/IP/CPU/memory/guest and only call `GetVMStatus` for disk (or add `DiskGB` to `VMInfo`, which both providers can fill from data they already fetch). Fall back to `GetVMStatus` when `ListVMs` errored, exactly as the orphan logic does today. See diff B. |
| P-3 **M** | vSphere `ListVMs` retrieves the whole `config` property set (`config` includes every device, extra-config key, hardware tree) for every VM in inventory | `provider/vmware/lifecycle.go:403` | Payload and server-side cost scale with inventory size, not with the five fields used | Request `config.template`, `config.hardware.numCPU`, `config.hardware.memoryMB`, `config.guestId`, `guest.ipAddress`, `runtime.powerState`, `name` explicitly. Identical output. See diff B. |
| P-4 **M** | Proxmox VM config is fetched by six separate hand-rolled copies (`GetTemplate`, `GetTemplateDetail`, `ListDisks`, `findDiskByIndex`, `detectOSType`, `getVMConfig`); `ExpandDisk` fetches it twice (once in `findDiskByIndex`) | `provider/proxmox/client.go:330,376,1289,1578,1715`, `nic.go:100` | Duplicate I/O and five parsers to keep in sync | Route every reader through `getVMConfig` (added this week); have `ExpandDisk` pass the config it already has into `findDiskByIndex`. |
| P-5 **L** | `GetStats` runs nine sequential `COUNT(*)` round-trips on every dashboard load | `db/sqlite.go` `GetStats` | Nine statement executions where one would do; SQLite is in-process so the cost is small but it runs on the most-visited page | Single `SELECT (SELECT COUNT(*) FROM targets), (SELECT COUNT(*) …), …` into the same struct. Same numbers. |
| P-6 **L** | Background pollers (`VMs.tsx` every 30 s, `NotificationBell` every poll interval) keep running in hidden tabs | `frontend/src/pages/VMs.tsx:74`, `components/Layout/NotificationBell.tsx:65` | Steady API load from idle tabs; with several tabs it eats into the per-IP rate budget that caused this week's incident | A `useVisiblePolling(fn, ms)` hook that pauses on `document.hidden` and refreshes immediately on `visibilitychange`. Same cadence while visible. See diff F2. |
| P-7 **L** | `ExecutionHub.SendOutput` spawns a goroutine that sleeps 60 s per finished execution to clear buffers | `api/ws/execution_hub.go:79` | One parked goroutine per completed execution for a minute; harmless at current scale, untidy | `time.AfterFunc(60*time.Second, …)` — same timing, no blocked goroutine. |

### Reliability & robustness

| ID | Finding | Scope | Impact | Non-breaking remediation |
|---|---|---|---|---|
| R-1 **H** | Handlers map errors to HTTP statuses by substring: `"must be powered off"`, `"hot-add"`, `"already registered"`, `"invalid role"`, `"not found"` ×3, `"in progress"`/`"already"` | `handlers/vms.go:235,333`, `settings.go:359`, `templates.go:53,71,90`, `factory.go:123` | Any rewording of a service message silently changes a client-visible status code (404/409 → 500); untestable without reproducing the exact text | Export sentinel errors from the service layer (`ErrVMNotFound`, `ErrAlreadyRegistered`, `ErrPoweredOn`, `ErrTemplateNotFound`, `ErrBuildInProgress`, `ErrInvalidRole`), wrap with `%w`, map with `errors.Is` — the pattern `AddNIC` already uses. Response bodies and codes unchanged. See diff C. |
| R-2 **H** | `isUniqueConstraintError` greps the driver's message text | `db/sqlite.go:1203` | Depends on `modernc.org/sqlite` English wording; a driver update can turn a 409 into a 500 | `errors.As(err, *sqlite.Error)` and compare `Code()` against `sqlite3.SQLITE_CONSTRAINT_UNIQUE` / `SQLITE_CONSTRAINT_PRIMARYKEY`. See diff D. |
| R-3 **M** | `isNotSupportedError` greps govmomi text (`"not supported"`) to decide on the ESXi deploy fallback | `provider/vmware/deploy.go:1021` | A fault text change would route vCenter deploys into the ESXi fallback path, or vice-versa | Check the fault type: `fault.Is(err, &types.NotSupported{})` (govmomi exposes typed faults via `types.HasFault`, as `isInvalidPowerStateFault` already does in the same package). |
| R-4 **M** | Twenty `if err != nil { node = p.node }` fallbacks silently use the *connected* node when the VM's node can't be resolved | `provider/proxmox/*.go` | On a multi-node cluster this sends the operation to the wrong node and surfaces as an opaque Proxmox 500/404 later; the real cause (listing failed) is discarded | Keep the fallback (behaviour), but log it at `Warn` with the lookup error once per operation; with P-1's cache the lookup becomes cheap enough to make failures rare. |
| R-5 **M** | Actions-tab WebSocket is only closed via the modal's `closeModal`; navigating away mid-execution leaves the socket open until the server drops it | `frontend/src/pages/VMDetail.tsx:1035–1131` | Leaked connection per abandoned execution view; stale `setState` on an unmounted component | `useEffect(() => () => wsRef.current?.close(), [])` in `ActionsTab`. See diff F1. |
| R-6 **M** ⚠ behavioural | `ExpandDisk` rejects `new_size_gb <= vm.DiskGB`, but `vm.DiskGB` is the **sum of all disks** (vSphere `GetVMStatus` adds every `VirtualDisk`) | `service/vm.go:661` | Expanding a 20 GB second disk on a VM with a 100 GB system disk to 50 GB is refused as "must be larger than current size (120GB)". Edge case, user-visible | Not a parity-safe refactor — a fix: compare against the selected disk's size from `ListDisks` (`disks[key].SizeGB`). Recommend as a tracked bug. |
| R-7 **L** ⚠ behavioural | Tab state initialised once from a constant, URL ignored — the pattern fixed this week in `VMDetail` (`?tab=`) also exists in `Settings.tsx` (`tab` has no URL binding, so deep links / refresh lose the tab) | `frontend/src/pages/Settings.tsx` | Refresh on Webhooks/API Keys returns to Users | Same treatment as `VMDetail`: initialise from `useSearchParams` and write back on change. Additive. |
| R-8 **L** | 37 swallowed catches; eight typed `catch (e: any)` | `pages/VMDetail.tsx` (7), `Factory.tsx` (4), `VMs.tsx` (3), `Templates.tsx` (3), `Settings.tsx` … | Failures disappear (console, cancel, sync, preview) — exactly how the rate-limit incident hid | Route through `getErrorMessage(e, fallback)` → toast, or `console.warn` with context where a toast is wrong (background refresh). `catch (e: unknown)` everywhere. |
| R-9 **L** | Polling loops sleep without observing the context: `DeleteVM` (`time.Sleep(1s)` ×30), `awaitTask` (checks `ctx` only at the top of each second), `runDeploy` (`Sleep(3s)`) | `provider/proxmox/client.go:896,961`, `service/deploy.go:474` | Cancellation is honoured up to 1–3 s late; a `ctx` deadline doesn't interrupt the sleep | `select { case <-ctx.Done(): return ctx.Err(); case <-time.After(d): }` helper. Same cadence. |
| R-10 **L** | `Execute` fetches the action twice (second time with the error discarded) to build redacted parameter values | `service/executor.go:~185` | Duplicate DB read; a transient failure silently stores unredacted-but-empty values | Reuse the `action` already loaded in the same function. |
| R-11 **L** | `ListVMs` listing failure disables orphan detection for the target but is only logged at `Warn` inside `vmIsOrphaned`'s caller | `service/vm.go` | Operators can't tell from `SyncAllResult` that orphan detection was skipped | Add the list error to `result.Errors` (additive field content; shape unchanged). |

### Code quality & maintainability

| ID | Finding | Scope | Impact | Non-breaking remediation |
|---|---|---|---|---|
| Q-1 **M** | `VMDetail.tsx` (1 580 lines) contains three components — `VMDetail`, `CredentialsCard`, `ActionsTab` — plus three copies of copy-to-clipboard logic | `frontend/src/pages/VMDetail.tsx` | Every change to any VM-page feature touches one file; three divergent clipboard helpers | Split into `pages/vm/VMDetail.tsx`, `pages/vm/ActionsTab.tsx`, `pages/vm/CredentialsCard.tsx`, `pages/vm/NetworkAdaptersCard.tsx`; one `copyToClipboard(text)` in `lib/utils.ts` (one already exists there — use it). No markup change. |
| Q-2 **M** | Duplicated status/power helpers: `statusVariant` ×5, `powerVariant` ×2, `powerLabel` ×2, `statusColors` ×2 across pages, with slightly different mappings (`cancelled` → `warning` on the dashboard, `secondary` elsewhere) | `pages/Dashboard.tsx`, `VMs.tsx`, `VMDetail.tsx`, `History.tsx`, `Factory.tsx`, `DeployLive.tsx` | Visual inconsistency and five places to update when a status is added | `lib/status.ts` exporting `deploymentStatusVariant`, `executionStatusVariant`, `powerVariant`, `powerLabel`. Keep each page's current mapping as-is in the first pass (parity), then reconcile deliberately. |
| Q-3 **M** | `db/sqlite.go` repeats the 20-column `managed_vms` `Scan` three times and the 16-column `deployments` scan four times; column list and field order must stay aligned by hand | `db/sqlite.go` (`ListManagedVMs`, `GetManagedVM`, …) | A new column means editing every copy; mismatches surface only at runtime | `scanManagedVM(row interface{ Scan(...any) error })` and `scanDeployment(...)` helpers plus one shared `SELECT` column fragment constant. See diff E. |
| Q-4 **M** | `vmware.DeployVM` (cyclo 68, ~300 lines) interleaves resolve → clone-spec → customise → network → cloud-init → task; `esxiDeployFallback` (38) duplicates two-thirds of it | `provider/vmware/deploy.go:22,657` | The highest-risk code in the product is the hardest to read and test; the two paths drift (`buildNICFromTemplate` exists for one) | Extract `resolvePlacement`, `buildCloneSpec`, `applyNetwork`, `applyCloudInit`, `waitClone` used by both paths. Pure extraction; the existing `deploy_*_test.go` and a govmomi simulator (`vcsim`) test should gate it. |
| Q-5 **L** | Unused code: `(*DB).getFamilyByBaseNameAndTarget`, `(*Engine).runCommand`, `(*Provider).defaultDatacenter` (staticcheck U1000); 26 unused frontend imports/locals/state (`Factory.tsx` carries a whole unused update-check flow: `checkingUpdates`, `checkForUpdates`, `managedTemplates`, `targetsList`) | see §0 | Noise; `Factory.tsx` suggests a half-removed feature | Delete; enable `noUnusedLocals`/`noUnusedParameters` in `tsconfig.json` so it can't regrow (CI runs `tsc --noEmit`). |
| Q-6 **L** | `Blueprints.tsx` and `BulkDeploy.tsx` are not routed from `App.tsx`; `Blueprints` has no nav entry either, but both compile and ship in the bundle via dynamic import | `frontend/src/pages`, `App.tsx` | Dead UI that still has to be migrated (it cost time in this week's upgrades); the blueprint API has no UI consumer | Either route them (feature decision) or move them under `pages/_unrouted/` and exclude from the build. Backend endpoints untouched. |
| Q-7 **L** | Five `@radix-ui/*` packages installed but unused (`dialog`, `select`, `separator`, `switch`, `tabs`); `depcheck` also flags `tailwindcss` but that is a false positive (consumed by `@tailwindcss/vite` through `@import "tailwindcss"`) | `frontend/package.json` | Install size, Dependabot noise, misleading "Radix UI" in docs (already corrected in README) | Remove the five packages. |
| Q-8 **L** | `TargetService.getProvider` keeps a hard-coded fallback `switch` behind the registry with a dated removal note ("after 1–2 weeks of production use", added 2026-03) | `service/target.go:161` | Two code paths for one job; the registry path has been the only one exercised for seven months | Remove the fallback; the registry is populated by `init()` in both provider packages and `IsRegistered` can guard a clear error. |
| Q-9 **L** | `cloudinit.go:153` `fmt.Sprintf` with no verbs (staticcheck S1039) | `service/cloudinit.go` | Style | Use the string directly. |
| Q-10 **L** | Mixed logging: 8 `fmt.Println`/`log.*` calls alongside `slog` | `cmd/`, `internal/` | Unstructured lines in otherwise structured logs | Convert to `slog` at the same level. |

### Typing & strictness

| ID | Finding | Scope | Impact | Non-breaking remediation |
|---|---|---|---|---|
| T-1 **M** | `targets.getTypes()` returns `{ types: any[] }` although `ProviderMetadata` is fully typed in `ProviderContext.tsx` | `frontend/src/api/client.ts:107` | The one API that gates provider-specific UI is untyped at the boundary | `api.get<{ types: ProviderMetadata[] }>` (move the interface to `types/`). |
| T-2 **M** | `catch (e: any)` ×8 in `Settings.tsx`; `inputs?: any` in the deployment manifest type | `pages/Settings.tsx`, `api/client.ts:352` | `any` leaks into toast text and manifest rendering | `catch (e: unknown)` + `getErrorMessage`; `inputs?: Record<string, unknown>`. |
| T-3 **L** | Proxmox JSON is decoded into `map[string]interface{}` and re-typed ad hoc (`intFromJSON`, string asserts) in several places | `provider/proxmox/client.go` | Each caller re-implements coercion; mistakes are runtime | A `qemuConfig` struct with the ~20 keys actually read (`netN`/`scsiN` kept as `map[string]string` for the indexed keys), decoded once in `getVMConfig`. |
| T-4 **L** | `tsconfig` has `noUnusedLocals: false`, `noUnusedParameters: false` | `frontend/tsconfig.json` | The 26 unused symbols above accumulate unnoticed | Enable both after Q-5 lands. |

### Security hardening (no behaviour change for legitimate traffic)

| ID | Finding | Scope | Impact | Non-breaking remediation |
|---|---|---|---|---|
| S-1 **M** | `chi/middleware.RealIP` is deprecated (GHSA-3fxj-6jh8-hvhx et al.) and, as used, rewrites `RemoteAddr` from `X-Forwarded-For` for **every** client once `FORGEMILL_TRUSTED_PROXIES` is non-empty — the configured proxy list is never compared against the peer address | `api/router.go:60`, `config.go:24` | With the setting on, any client can spoof its IP in the audit log and dodge the per-IP rate limiter by sending `X-Forwarded-For`. The intent of the config variable is correct; the wiring isn't | A 20-line `TrustedProxyRealIP(cidrs)` middleware that applies the header only when `RemoteAddr` is in the list. Same header semantics for real proxies. See diff G. |
| S-2 **L** | `TrustedProxies` is a comma-separated string that is only tested for non-emptiness; no validation at startup | `config.go:53` | A typo silently enables RealIP for everyone (compounded by S-1) | Parse into `[]netip.Prefix` at startup; fail fast on an invalid entry. |

### Tests

| ID | Finding | Scope | Impact | Remediation |
|---|---|---|---|---|
| X-1 **H** | Coverage: handlers 2.5 %, vmware 2.2 %, service 13 %, db 13 %; frontend has no test runner at all | repo-wide | Every refactor above is currently gated only by the Playwright screenshot harness (which lives outside the repo) and the few provider doubles | Before Q-3/Q-4: add `httptest`-based handler tests for status mapping (R-1 makes these trivial), a `vcsim`-backed test for `vmware.DeployVM`/`ListVMs`, and commit the screenshot harness + seed script under `frontend/e2e/` with a `npm run e2e` target so UI refactors are gated in CI. |

---

## 3. Before / after — the critical ones

### A. Proxmox: resolve VM → node once per connection (P-1, R-4)

**Before** — `client.go`, repeated at 20 call sites:

```go
func (p *Provider) GetVMStatus(ctx context.Context, vmID string) (*provider.VMStatus, error) {
	node, err := p.resolveVMNode(ctx, vmID) // GET /cluster/resources?type=vm — every call
	if err != nil {
		node = p.node // silent fallback
	}
	body, err := p.doGet(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/status/current", url.PathEscape(node), url.PathEscape(vmID)))
	...
```

**After** — one cached lookup, one logged fallback:

```go
// nodeCache maps vmid -> node for the life of a Connect/Disconnect cycle.
// Proxmox VMs don't change node without a migration, and a migration
// shows up as a 404 on the old node, which invalidates the entry.
type Provider struct {
	// ...existing fields
	nodeMu    sync.Mutex
	nodeCache map[string]string
}

func (p *Provider) Connect(ctx context.Context) error {
	p.nodeMu.Lock(); p.nodeCache = map[string]string{}; p.nodeMu.Unlock()
	// ...existing body unchanged
}

// nodeFor returns the node hosting vmID, consulting /cluster/resources at
// most once per unknown VM per connection. On lookup failure it falls back
// to the connected node exactly as before, but says so.
func (p *Provider) nodeFor(ctx context.Context, vmID string) string {
	p.nodeMu.Lock()
	if n, ok := p.nodeCache[vmID]; ok {
		p.nodeMu.Unlock()
		return n
	}
	p.nodeMu.Unlock()

	node, err := p.resolveVMNode(ctx, vmID)
	if err != nil {
		slog.Warn("proxmox: could not resolve VM node, using connected node", "vmid", vmID, "node", p.node, "error", err)
		return p.node
	}
	p.nodeMu.Lock(); p.nodeCache[vmID] = node; p.nodeMu.Unlock()
	return node
}

func (p *Provider) GetVMStatus(ctx context.Context, vmID string) (*provider.VMStatus, error) {
	node := p.nodeFor(ctx, vmID)
	body, err := p.doGet(ctx, fmt.Sprintf("/nodes/%s/qemu/%s/status/current", url.PathEscape(node), url.PathEscape(vmID)))
	...
```

Mechanical replacement of the 20 `node, err := p.resolveVMNode(...); if err != nil { node = p.node }` blocks with `node := p.nodeFor(ctx, vmID)`. `resolveVMNode` can additionally populate the cache for *every* VM in the listing it already paid for, so a `SyncAll` over N VMs resolves nodes with a single call. The httptest double in `nic_api_test.go` can assert the call count.

### B. `SyncAll`: stop re-fetching what `ListVMs` returned (P-2, P-3)

**Before** — `service/vm.go`:

```go
hypervisorVMs, listErr := p.ListVMs(ctx)          // has power, IP, CPU, mem, guest for every VM
...
for _, vm := range targetVMs {
	...
	status, err := p.GetVMStatus(ctx, vm.VMRef)   // fetches the same again, per VM
	if err != nil { ...; continue }
	s.updateVMPowerState(&vm, status.PowerState, status.IPAddress)
	if status.CPU > 0 || status.MemoryMB > 0 || status.DiskGB > 0 { s.db.UpdateManagedVMResources(...) }
	if status.GuestID != "" { s.db.UpdateManagedVMOSType(...) }
```

**After** — add `DiskGB` to `VMInfo` (both providers already touch the data: vSphere sums `VirtualDisk` capacities from the device list it retrieves; Proxmox has `maxdisk` in `/cluster/resources`), then:

```go
for _, vm := range targetVMs {
	...
	var status provider.VMStatus
	if hvm, ok := hypervisorRefs[vm.VMRef]; listErr == nil && ok {
		// Everything SyncAll needs came back in the listing — no per-VM call.
		status = provider.VMStatus{PowerState: hvm.PowerState, IPAddress: hvm.IPAddress, CPU: hvm.CPU, MemoryMB: hvm.MemoryMB, DiskGB: hvm.DiskGB, GuestID: hvm.GuestID}
	} else {
		// Listing failed or didn't include this VM: fall back to the old path.
		st, err := p.GetVMStatus(ctx, vm.VMRef)
		if err != nil { ...same handling...; continue }
		status = *st
	}
	// ...the existing update block, unchanged
```

And in `vmware.ListVMs`, replace the property set:

```go
// before
containerView.Retrieve(ctx, []string{"VirtualMachine"}, []string{"name", "config", "guest", "runtime"}, &vms)
// after — the five fields actually read, instead of the whole config tree
containerView.Retrieve(ctx, []string{"VirtualMachine"}, []string{
	"name", "runtime.powerState", "guest.ipAddress", "config.template",
	"config.guestId", "config.hardware.numCPU", "config.hardware.memoryMB", "config.hardware.device",
}, &vms)
```

(`config.hardware.device` only if `DiskGB` is added; otherwise drop it too.) vSphere `PowerState` from the listing is the same `runtime.powerState` string `GetVMStatus` returns; Proxmox `ListVMs` must run its `NormalizePowerState` on the listing values the way `GetVMStatus` does — verify that in the diff or the fallback path masks nothing. Net: one listing per target instead of 1+N (vSphere) or 1+3N (Proxmox) calls.

### C. Error contracts: sentinels instead of substrings (R-1)

**Before** — `handlers/vms.go`:

```go
if err := h.svc.Resize(r.Context(), id, req.CPU, req.MemoryMB); err != nil {
	if strings.Contains(err.Error(), "must be powered off") || strings.Contains(err.Error(), "hot-add") {
		writeError(w, "VM must be powered off to change this resource (hot-add not enabled)", http.StatusConflict)
		return
	}
	writeErrorLog(w, "failed to resize VM", http.StatusInternalServerError, err)
	return
}
```

**After** — `service/vm.go` exports the intent; the handler branches on it:

```go
// service/vm.go
var ErrRequiresPowerOff = errors.New("VM must be powered off for this change")

func (s *VMService) Resize(ctx context.Context, id int64, cpu, memoryMB int) error {
	...
	if vm.PowerState != "poweredOff" && vm.PowerState != "stopped" {
		return fmt.Errorf("%w: resize (current state: %s)", ErrRequiresPowerOff, vm.PowerState)
	}
	...
	if err := p.ResizeVM(ctx, vm.VMRef, cpu, memoryMB); err != nil {
		if errors.Is(err, provider.ErrRequiresPowerOff) { // vmware returns this for the hot-add case
			return fmt.Errorf("%w: %v", ErrRequiresPowerOff, err)
		}
		return err
	}

// handlers/vms.go
if err := h.svc.Resize(r.Context(), id, req.CPU, req.MemoryMB); err != nil {
	if errors.Is(err, service.ErrRequiresPowerOff) {
		writeError(w, "VM must be powered off to change this resource (hot-add not enabled)", http.StatusConflict)
		return
	}
	writeErrorLog(w, "failed to resize VM", http.StatusInternalServerError, err)
	return
}
```

Same status, same body text, no dependency on wording. Apply the same to `Register` (`ErrAlreadyRegistered` → 409), `templates.go` (`db.ErrNotFound` → 404), `settings.go` (`ErrInvalidRole` → 400), `factory.go` (`ErrBuildInProgress` → 409). Each becomes a two-line `httptest` case.

### D. Unique-constraint detection by error code (R-2)

```go
// before
func isUniqueConstraintError(err error) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// after
import (
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func isUniqueConstraintError(err error) bool {
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return false
	}
	switch se.Code() {
	case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
		return true
	}
	return false
}
```

One table-driven test with a real in-memory DB (the `newTestDB` helper exists) pins both the positive and the negative case.

### E. One scanner per table (Q-3)

```go
// before — repeated in ListManagedVMs, GetManagedVM, ListManagedVMsByTarget ...
rows.Scan(&vm.ID, &vm.DeploymentID, &vm.TargetID, &vm.VMName, &vm.VMRef, &vm.PowerState, &vm.IPAddress,
	&vm.CPU, &vm.MemoryMB, &vm.DiskGB, &vm.OSType, &vm.Platform, &vm.LastSyncedAt, &vm.CreatedAt, &vm.TargetName,
	&vm.TemplateName, &vm.StateChangedAt, &vm.LastPoweredOnAt, &vm.LastPoweredOffAt, &vm.TotalRuntimeSeconds)

// after
const managedVMColumns = `v.id, v.deployment_id, v.target_id, v.vm_name, v.vm_ref, v.power_state, v.ip_address,
	v.cpu, v.memory_mb, v.disk_gb, v.os_type, COALESCE(v.platform, 'linux'), v.last_synced_at, v.created_at,
	COALESCE(t.name, ''), COALESCE(d.template_name, tmpl.name, ''),
	v.state_changed_at, v.last_powered_on_at, v.last_powered_off_at, v.total_runtime_seconds`

type rowScanner interface{ Scan(dest ...any) error }

func scanManagedVM(r rowScanner) (models.ManagedVM, error) {
	var vm models.ManagedVM
	err := r.Scan(&vm.ID, &vm.DeploymentID, &vm.TargetID, &vm.VMName, &vm.VMRef, &vm.PowerState, &vm.IPAddress,
		&vm.CPU, &vm.MemoryMB, &vm.DiskGB, &vm.OSType, &vm.Platform, &vm.LastSyncedAt, &vm.CreatedAt, &vm.TargetName,
		&vm.TemplateName, &vm.StateChangedAt, &vm.LastPoweredOnAt, &vm.LastPoweredOffAt, &vm.TotalRuntimeSeconds)
	return vm, err
}
```

`*sql.Row` and `*sql.Rows` both satisfy `rowScanner`, so `GetManagedVM` and the list functions share one column order. Adding a column becomes a two-place edit that fails to compile if the two drift.

### F. Frontend resilience

**F1 — close the execution socket on unmount (R-5)**, `ActionsTab`:

```tsx
// after — alongside the existing wsRef
useEffect(() => {
  return () => {
    wsRef.current?.close();
    wsRef.current = null;
  };
}, []);
```

**F2 — visibility-aware polling (P-6)**, `hooks/useVisiblePolling.ts`:

```ts
export function useVisiblePolling(fn: () => void, intervalMs: number) {
  useEffect(() => {
    let timer: ReturnType<typeof setInterval> | null = null;
    const start = () => { if (!timer) timer = setInterval(fn, intervalMs); };
    const stop = () => { if (timer) { clearInterval(timer); timer = null; } };
    const onVis = () => { if (document.hidden) stop(); else { fn(); start(); } };
    if (!document.hidden) start();
    document.addEventListener("visibilitychange", onVis);
    return () => { stop(); document.removeEventListener("visibilitychange", onVis); };
  }, [fn, intervalMs]);
}
// VMs.tsx:   useVisiblePolling(reload, 30000);   — replaces the bare setInterval
```

Same 30 s cadence while the tab is visible; nothing while hidden; an immediate refresh when it becomes visible again.

**F3 — stop swallowing (R-8, T-2)**, e.g. `VMDetail.tsx` `doConsole`:

```tsx
// before
} catch {
  // silent
}
// after
} catch (e: unknown) {
  toast(getErrorMessage(e, "Could not open the console"), "error");
}
```

### G. Trusted-proxy aware RealIP (S-1, S-2)

```go
// internal/api/middleware/realip.go
// TrustedProxyRealIP applies X-Forwarded-For / X-Real-IP only when the
// direct peer is one of the configured proxies. Unlike chi's RealIP it
// never trusts the headers from an arbitrary client.
func TrustedProxyRealIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if peer, perr := netip.ParseAddr(host); err == nil && perr == nil && inAny(peer, trusted) {
				if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
					// Leftmost entry is the client, as chi's RealIP read it.
					if ip, _, _ := strings.Cut(xff, ","); ip != "" {
						r.RemoteAddr = net.JoinHostPort(strings.TrimSpace(ip), "0")
					}
				} else if xr := r.Header.Get("X-Real-IP"); xr != "" {
					r.RemoteAddr = net.JoinHostPort(strings.TrimSpace(xr), "0")
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// router.go
if len(cfg.TrustedProxies) > 0 {              // []netip.Prefix, parsed and validated in config.Load
	r.Use(middleware.TrustedProxyRealIP(cfg.TrustedProxies))
}
```

For deployments behind the configured proxy the observed `RemoteAddr` is identical to today; for everything else the spoofing vector closes. `config.Load` parses `FORGEMILL_TRUSTED_PROXIES` into prefixes (a bare IP becomes a /32 or /128) and fails fast on garbage — the env var's accepted format is unchanged.

---

## 4. Suggested order of work

1. **Tests first where they're cheapest** (X-1): sentinel-error handler tests, the `isUniqueConstraintError` table test, `vcsim` smoke for `ListVMs`/`DeployVM`, and commit the Playwright screenshot harness. This is what makes 2–5 safe.
2. **R-1, R-2, R-3** — error contracts. Small diffs, each independently verifiable.
3. **P-1, P-2, P-3, P-4** — the sync hot path. Measure before/after with the Proxmox httptest double counting requests.
4. **S-1, S-2** — proxy handling. Needs a one-line note in the README config table (format unchanged).
5. **Q-5, Q-6, Q-7, Q-8, Q-9, Q-10, T-1, T-2, T-4** — dead code and strictness, in one sweep, then turn the strict `tsconfig` flags on.
6. **Q-1, Q-2, Q-3** — frontend split and DB scanners, each gated by the screenshot diff / `go test`.
7. **Q-4** — `DeployVM` decomposition, last, with `vcsim` coverage in place.
8. Track **R-6** and **R-7** as bugs (they change behaviour, deliberately).
