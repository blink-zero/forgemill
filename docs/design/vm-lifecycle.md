# VM lifecycle policies — design

Status: proposal (2026-10-04). Focus item 4 from the post-audit roadmap.

**Decision 2026-10-04:** *Discover & adopt* (section 3) is approved in principle and is being built. *Expiry* (section 2) is **deferred** — automated destruction was judged too risky for now; the section stays as the record of the design should it be revisited.

Two capabilities, one idea: **Forgemill owns the whole life of a VM, not just its birth.**

- **Expiry** — a VM can carry an expiry; when it arrives, Forgemill retires the VM (destroy, or power off) and tells you beforehand. Disposable VMs stop becoming orphans.
- **Discover & adopt** — VMs that already exist on a target (built by hand, by another tool, or untracked earlier) can be brought under management in one step, and become first-class: synced, actionable, expirable.

Both are built on things that already exist: the sync loop (`SyncAll` already fetches every VM on every target), the destroy path (hard stop + delete, audited), notifications/webhooks, the per-VM event log, and the deploy pipeline.

---

## 1. Why this fits the vision

Forgemill's promise is *infrastructure forged to order*: a template, a form, a VM with cloud-init and actions applied, in minutes. Two gaps break the promise at the edges:

1. **What gets forged never gets un-forged.** Every validation run this week created a dozen `fm-audit-*` VMs and had to delete each by hand. Labs, demos, CI runners, review environments — most VMs Forgemill creates have a natural lifetime, and today the only lifetime is "until someone remembers".
2. **Forgemill only knows what it made.** A target with 40 VMs of which Forgemill deployed 8 is 20 % managed. The other 32 can't be synced, actioned, snapshotted or retired from here, and the UI text literally says untracked VMs "cannot currently be re-imported".

Closing both turns the VM list from "things I deployed" into "the estate", with a lifetime on every row. That is the difference between a deployment tool and a management plane.

---

## 2. Expiry

### 2.1 User experience

**At deploy** — Configure step, next to the hardware fields, one control: **Expiry**

```
Expiry   [ Never ▾ ]      ( Never · 4 hours · 1 day · 7 days · 30 days · Custom… )
         On expiry: ( • Destroy  ○ Power off )
```

- Default comes from a global setting (*Settings → Preferences → Default expiry for new deployments*, default **Never**), so a lab can default to "1 day" and production stays "Never".
- Custom opens a date/time picker in the user's timezone preference; stored and displayed in UTC-correct terms (the app already formats times through `useTimezone`).
- The Review step shows *Expires: Oct 5, 14:30 (in 1 day) · destroy* in the summary; the deployment manifest/receipt records it.

**On the VM page** — the existing *Lifecycle* card gains a row:

```
Expires    in 3h 12m  (Oct 4, 14:30)  ·  destroy        [Extend ▾] [Change] [Remove]
```

- *Extend* is one click: +1 hour / +1 day / +7 days — the thing you reach for when a demo runs long.
- An expired-and-pending VM (hypervisor unreachable, or inside the final minute) shows a warning banner at the top of the page: *Scheduled for destruction — the last attempt failed: <reason>. Extend to keep it.*
- Adopted and registered VMs can be given an expiry here too (it is a property of the managed VM, not of the deployment).

**On the VMs list** — a small *expires in 2h* chip on the card / an *Expires* column in table view, sortable; the search box's existing query grammar (`uptime:`, `age:`) gains `expires:<24h` / `expires:none`; a filter chip *Expiring soon (24h)*.

**Dashboard** — an *Expiring soon* tile (count, next three with times) that links to the filtered VM list. Empty state: *No VMs expire in the next 24 hours.*

**Notifications (in-app + webhook)**:
- `vm.expiring` — *web-03 expires in 1 hour (destroy). [Extend]* — sent once, at a lead time that scales with the lifetime (1 h for ≤ 1 day, 24 h otherwise, configurable in settings). Goes to the user who set the expiry, plus admins.
- `vm.expired` — *web-03 was destroyed by its expiry policy.* / *…powered off by its expiry policy.*
- `vm.expiry_failed` — admins only — *Could not destroy web-03 at expiry: <hypervisor reason>. Will retry.*

**MCP** — `deploy_vm(…, expires_in="4h" | expires_at="2026-10-05T14:30:00Z", expiry_action="destroy")`, `set_vm_expiry(vm_id, …)`, `extend_vm_expiry(vm_id, by="1d")`, `clear_vm_expiry(vm_id)`; `list_vms` / `get_vm` return `expires_at` and `expiry_action`. An agent running a validation can then create VMs that clean themselves up.

### 2.2 Rules (precise)

- Expiry is a timestamp, stored UTC (`expires_at`), with `expiry_action ∈ {destroy, poweroff}`.
- The policy is about **retention, not power state**: a VM that was powered off manually is still destroyed at expiry. The copy says so where the action is chosen.
- **Destroy at expiry uses exactly the user-facing destroy** (`VMService.Delete(force=false)`): hard stop + delete on the hypervisor, DB row removed, audit entry with actor `system:expiry`, webhook `vm.expired`. Nothing is ever *untracked only* by the policy — a VM is either destroyed on the hypervisor or it stays managed.
- **Power off at expiry** powers off and keeps the VM and its expiry marked *fulfilled* (`expiry_fulfilled_at`), so it isn't acted on again; the VM page shows *Expired Oct 4 (powered off)*. Extending clears the fulfilled mark.
- **Failure**: if the hypervisor operation fails, the VM stays managed, a VM event and an admin notification record the reason, and the scheduler retries every tick with backoff (1, 5, 15, 60 min, then hourly). It never gives up silently and never deletes the DB row on a failed destroy (that invariant already exists: `V3-L6`).
- **Extend** adds to `max(now, expires_at)` so an already-expired VM extended by 1 h gets a full hour, not a negative one.
- Changing or removing expiry is audited (`vm.expiry.set` / `vm.expiry.clear`, with old and new values) and written to the VM's event log.
- Setting an expiry in the past is rejected (400) — use destroy if you mean now.
- One scheduler, in-process, one-minute tick, idempotent: it selects `expires_at <= now AND expiry_fulfilled_at IS NULL` and processes each VM under a per-VM lock, so a manual destroy racing the policy is harmless (the delete path already treats 404 as done).
- Global kill switch: `Settings → Preferences → Enforce VM expiry` (default on). Off = nothing is destroyed, notifications still fire (so a paused environment still sees what *would* have happened).

### 2.3 Data

```
managed_vms
  + expires_at            DATETIME NULL
  + expiry_action         TEXT NOT NULL DEFAULT 'destroy'   CHECK (destroy|poweroff)
  + expiry_set_by         INTEGER NULL REFERENCES users(id)
  + expiry_notified_at    DATETIME NULL
  + expiry_fulfilled_at   DATETIME NULL
  + expiry_last_error     TEXT NOT NULL DEFAULT ''
  + expiry_attempts       INTEGER NOT NULL DEFAULT 0
app_settings
  default_expiry (duration or 'never'), default_expiry_action, expiry_lead_time, enforce_expiry
```

`DeployRequest` gains `expires_in` (`"4h"`, `"7d"`) **or** `expires_at` (RFC 3339) and `expiry_action`; `config_json` carries them so the manifest shows the intent.

### 2.4 API

```
PUT    /api/vms/{id}/expiry       {expires_at | expires_in, action}   → 200 vm
POST   /api/vms/{id}/expiry/extend {by: "1h"|"1d"|"7d"}                → 200 vm
DELETE /api/vms/{id}/expiry                                             → 204
GET    /api/vms?expiring_within=24h                                     (list filter)
```

---

## 3. Discover & adopt

### 3.1 User experience

**Entry points**
- Targets page → a target's row shows *Unmanaged: 7* next to its VM count when the last sync found VMs Forgemill doesn't track; clicking it opens Discover for that target.
- VMs page header → **Discover…** (target picker when more than one target).
- Dashboard → *Unmanaged VMs* tile (sum across targets) linking to the first target with any.

**Discover view** (`/targets/{id}/discover`) — a table of what the hypervisor reports and Forgemill doesn't manage:

```
☐  Name              Power     IP             vCPU/RAM/Disk      Guest OS       Node/Host
☐  jenkins-agent-02  running   10.20.10.44    4 / 8 GB / 80 GB   Ubuntu 22.04   esx-01
☐  old-test-vm       stopped   —              2 / 2 GB / 20 GB   (unknown)      esx-02
…
[Search]  [Show ignored (3)]                   [Ignore selected]  [Adopt 2 VMs]
```

- Templates are excluded (they're templates, not VMs). VMs already managed are excluded. Already-adopted-then-untracked VMs simply reappear here — which is the "re-import" path the UI used to say didn't exist.
- **Ignore** hides noise permanently per target+ref (appliances, other teams' VMs); *Show ignored* reveals and can un-ignore. Ignored refs are excluded from the *Unmanaged* count.
- **Adopt** (confirm dialog, non-destructive so one click, but the dialog lists what adoption does): creates the managed record, syncs it immediately (power, IP, sizes, guest OS), writes a VM event *Adopted from vcenter-lab by alice*, audits `vm.adopt`, and offers an optional expiry in the same dialog (*Expire these VMs in: Never ▾*). The result toast links to the VM list filtered to *origin: adopted*.
- Zero-state: *Everything on vcenter-lab is already managed by Forgemill.*

**Adopted VMs are first-class**
- VM page shows an *Adopted* badge where deployed VMs show *from <template>*; the Details card shows *Origin: adopted Oct 4 by alice* instead of a template.
- The Credentials card, which today reveals the deployment's generated credentials, shows for adopted VMs: *Not deployed by Forgemill — no stored credentials.* **[Set SSH credentials]** → username + password or private key, encrypted at rest with the existing encryptor, used by *execute_action* exactly like deployment credentials. Without them, the Actions tab explains what's missing instead of failing on connect. (Deployed VMs get the same control, which also fixes the "I rotated the password" case.)
- Everything else — power, snapshots, resize, expand/add disk, add NIC, sync, events, destroy, expiry — works unchanged, because it only ever needed `target_id` + `vm_ref`.
- The VMs list gets an *Origin* filter (deployed / adopted / registered).

**MCP** — `discover_vms(target_id, include_ignored=False)`, `adopt_vms(target_id, vm_refs=[…], expires_in=None)`, `ignore_discovered_vms(target_id, vm_refs=[…])`, `set_vm_credentials(vm_id, username, password|private_key)`.

### 3.2 Rules (precise)

- Discovery is **read-only and live**: one `ListVMs` call against the target (the same call `SyncAll` makes), filtered in the service. It is never cached as truth; the *Unmanaged* counts are a by-product of the last sync and say when they were computed.
- Adoption is **idempotent** on `(target_id, vm_ref)`: adopting a VM that was adopted in the meantime returns the existing record (the unique index already exists; `ErrAlreadyRegistered` already exists).
- Adopted VMs have `deployment_id NULL`, `origin = 'adopted'`, `adopted_at`, `adopted_by`. The existing `POST /api/vms` (register by ref) becomes `origin = 'registered'` and is kept for API compatibility.
- Orphan detection is symmetric: if an adopted VM disappears from the hypervisor, `SyncAll` untracks it exactly as it does deployed VMs (and it reappears in Discover if it comes back).
- Permissions: Discover is visible to anyone who can see the target; **Adopt / Ignore / Set credentials require the operator role** (the same role that can register VMs and run actions today); nothing here is admin-only.
- Credentials: stored per VM (`vm_credentials`: `vm_id`, `username`, `secret_enc`, `kind ∈ {password, private_key}`, `set_by`, `updated_at`); never returned by the API after being set (only *set / not set*); `GetCredentials` for a VM returns the per-VM override when present, else the deployment's initial credentials, else 404 — one resolution order, used by the UI, `execute_action` and MCP alike.

### 3.3 Data

```
managed_vms
  + origin        TEXT NOT NULL DEFAULT 'deployed'  CHECK (deployed|adopted|registered)
  + adopted_at    DATETIME NULL
  + adopted_by    INTEGER NULL REFERENCES users(id)
target_ignored_vms (target_id, vm_ref PRIMARY KEY, vm_name, ignored_by, created_at)
vm_credentials     (vm_id PRIMARY KEY, username, secret_enc, kind, set_by, updated_at)
```

Backfill: existing rows with a `deployment_id` → `deployed`; without → `registered`.

### 3.4 API

```
GET    /api/targets/{id}/discover?include_ignored=false   → {target, computed_at, vms:[{ref,name,power_state,ip,cpu,memory_mb,disk_gb,guest_id,host}], ignored:[…]}
POST   /api/targets/{id}/adopt      {vm_refs:[…], expires_in?, expiry_action?}  → 201 {adopted:[vm…], skipped:[{ref, reason}]}
POST   /api/targets/{id}/ignore     {vm_refs:[…]}  /  DELETE …/ignore {vm_refs}
PUT    /api/vms/{id}/credentials    {username, password | private_key}  → 204 ;  DELETE → 204
GET    /api/vms?origin=adopted
SyncAllResult.targets[{target_id, unmanaged}]   (additive)
```

---

## 4. Delivery plan (one PR each, same gates as always)

*Expiry rows (1–3) are deferred; the adopt rows (4–7) are the active plan, with 6 (credentials) shipping alongside adoption.*

| # | PR | contents | gate |
|---|---|---|---|
| 1 | Expiry core | migration, `LifecycleService` (set/extend/clear, scheduler with backoff, notifications, webhooks, audit, VM events), `PUT/POST/DELETE /vms/{id}/expiry`, list filter | service tests with a fake clock: notify once at lead time, destroy at expiry, power-off marks fulfilled, failure retries with backoff and alerts, extend from past, kill switch |
| 2 | Expiry at deploy + UI | `expires_in/at` on deploy + preflight + manifest; Deploy form control; Lifecycle card row with Extend/Change/Remove; list chip/column/filter; Dashboard tile; settings defaults | screenshot gate (new states in the tour), service tests for deploy carry-through |
| 3 | Expiry in MCP | `deploy_vm` fields, `set/extend/clear_vm_expiry`, fields on `get_vm` | MCP tests |
| 4 | Discover & adopt core | migration (`origin`, ignore list), `DiscoveryService`, `GET discover`, `POST adopt/ignore`, `SyncAll` unmanaged counts, register endpoint → `origin=registered` | fakePVE + vcsim: discover excludes templates/managed/ignored; adopt is idempotent and syncs; orphan symmetry |
| 5 | Discover UI | Discover view, Targets/VMs/Dashboard entry points, Adopted badge + origin filter, adopt dialog with optional expiry | screenshot gate |
| 6 | VM credentials | `vm_credentials`, resolution order in `GetCredentials` + `execute_action`, Credentials card *Set credentials*, MCP `set_vm_credentials` | service tests (override beats deployment creds; never echoed back) |
| 7 | Discover in MCP | `discover_vms`, `adopt_vms`, `ignore_discovered_vms` | MCP tests |
| 8 | Docs + release | README sections, release notes → **v0.20.0** / mcp **v0.13.0** | live validation on vCenter + Proxmox |

Estimated size: 1–3 are a day; 4–7 another day and a half; 8 half a day. Each PR is independently releasable, so the expiry half can ship first if useful.

---

## 5. Decisions to confirm

1. **Default `expiry_action`** — proposal: `destroy` (the point of a TTL is cleanup; power-off is the opt-in gentler mode). Alternative: `poweroff` default with destroy opt-in.
2. **Adopt / ignore permission** — proposal: operator role (same as register VM / run action), not admin-only.
3. **Expiry lead-time defaults** — proposal: 1 h for lifetimes ≤ 24 h, 24 h otherwise; both configurable.
4. **Scope of `vm_credentials`** — proposal: ship it with adoption (PR 6) because an adopted VM without credentials is a half-adopted VM; it also fixes credential rotation for deployed VMs. Alternative: defer and ship adoption as "inventory + hypervisor operations only".
