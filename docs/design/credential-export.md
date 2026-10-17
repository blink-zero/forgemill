# Credential export (1Password) — design

Status: accepted (2026-10-17); backend in `feat/credential-export`.

An admin can export the SSH login of every managed VM as a file 1Password imports natively, after re-entering their own password. One request, one password, one file, one audit entry that is written before the first byte leaves the server.

---

## 1. Problem

Forgemill already holds a login for most VMs it manages: either the per-VM credential an admin set (`vm_credentials`, password or private key, optional sudo password) or the cloud-init user and password it generated at deploy time (`deployments.initial_username` / `initial_password_enc`). Today the only way to get one out is the per-VM "reveal" on the VM page (`GET /vms/{id}/credentials`), one VM at a time, with no audit entry. Teams that keep their secrets in 1Password end up copying by hand or never recording them.

What this is not: an export of hypervisor target passwords, LDAP bind passwords, API keys, webhook secrets or AI keys. Those are Forgemill's own secrets, not the fleet's, and have different owners. Scope can grow later behind the same gate.

## 2. User experience

Settings → new admin-only tab **Credentials** (`?tab=credentials`).

1. The tab opens with a live preview, no password needed: *"42 VMs have a login Forgemill can export. 38 password logins, 4 key logins, 6 with a separate sudo password. 7 VMs have no stored login and will be skipped."* Preview comes from a counts-only endpoint; no secret is decrypted for it.
2. Scope: **All VMs** (default) / **One target** (dropdown) / **Selected VMs** (multi-select from the inventory, search box).
3. Options, all unticked by default:
   - Include sudo passwords
   - Include private keys
   - Include logins inherited from the deployment (ticked by default; untick to export only explicitly set credentials)
4. Format: **1Password (.1pux)** (default, recommended) / **CSV (generic)**.
5. A plain warning in the card, not a modal: *"The file contains plaintext passwords. Import it into 1Password, then delete it. Every export is recorded in the audit log and every admin is notified."*
6. Password field: *"Confirm your password"*. Submit button is disabled until the field is non-empty and the preview count is above zero. One in-flight guard; the field is cleared on success or failure.
7. On success the browser downloads `forgemill-credentials-2026-10-17T0953Z.1pux` (or `.csv`) and the card shows *"Exported 42 logins. Last export: you, just now."* On a wrong password: *"Password not accepted."* with no further detail, and after five failures in a minute the button is disabled for the cooldown with a countdown.

The tab also lists the last ten exports (actor, time, count, format, scope) read from the audit log, so an admin can see at a glance who pulled what.

Phone width: the card stacks, the VM multi-select becomes a full-width list, the download works through the same blob path.

## 3. API

### `GET /api/settings/credentials/export/preview`
Admin role, full scope, any auth method (it is counts only). Returns:

```json
{"total_vms": 49, "exportable": 42, "password_logins": 38, "key_logins": 4,
 "with_sudo_password": 6, "from_deployment": 20, "skipped_no_login": 7,
 "by_target": [{"target_id": 1, "name": "pve-lab", "exportable": 30}, …]}
```

Accepts `?target_id=` and `?vm_ids=1,2,3` so the preview tracks the scope selection.

### `POST /api/settings/credentials/export`

```json
{"password": "…", "format": "1pux" | "csv",
 "scope": {"all": true} | {"target_id": 1} | {"vm_ids": [1,2,3]},
 "include_sudo_passwords": false, "include_private_keys": false,
 "include_deployment_logins": true}
```

Gate, in order, each failing closed:

| Check | On failure |
|---|---|
| `FORGEMILL_CREDENTIAL_EXPORT=disabled` (env, default enabled) | 404, route not mounted |
| Role is `admin` (`RequireRole("admin")`) | 403 |
| Authenticated with a **JWT session**, not an API key. Needs a new `authMethodKey` in the middleware context (`"jwt"` / `"api_key"`); `RequireScope("full")` is not enough because a full-scope key has no password to re-enter. | 403 `"credential export requires an interactive session"` |
| Rate limit: the existing `loginLimiter` (5/min per IP) **and** a per-user counter (5 failures → 15 min lockout, in memory, keyed by user id) | 429 |
| Password length 1–72 bytes (same bounds as login) | 401 |
| Password verifies for **this** user: local user → `bcrypt.CompareHashAndPassword` against `PasswordHash`; LDAP user (`ExternalID != ""`) → `ldap.Authenticate(username, password)`, never the local hash; the dummy-hash timing guard from `Login` applies | 403 `{"error":"password not accepted","code":"password"}`, audit `credentials.export.denied`. 403 rather than 401 because the UI treats every 401 as an expired session and reloads to the login page |
| Scope resolves to at least one exportable VM | 422 |
| **Synchronous** audit write succeeds (see §5) | 500, nothing exported |

Response: `200`, `Content-Type: application/zip` (1pux) or `text/csv; charset=utf-8`, `Content-Disposition: attachment; filename="…"`, `Cache-Control: no-store`, `X-Content-Type-Options: nosniff`, body streamed. No JSON envelope; the frontend reads the `Content-Disposition` filename.

Not exposed through `forgemill-mcp` and not added to the CLI. The MCP README gets one line saying so.

## 4. File formats

### 4.1 1PUX (primary)

1PUX is 1Password's own unencrypted export: a zip containing `export.attributes`, `export.data` and an empty `files/` directory. 1Password 8 on every platform imports it directly, and unlike CSV it carries structured fields, URLs, tags and notes. Layout from the format description ([1Password: About the 1PUX format](https://support.1password.com/1pux-format/)):

```
export.attributes   {"version": 3, "description": "Forgemill credential export", "createdAt": <unix>}
export.data         {"accounts": [{"attrs": {…}, "vaults": [{"attrs": {…}, "items": [ … ]}]}]}
files/
```

One vault, `"name": "Forgemill"`, `"type": "U"`. One Login item (`categoryUuid: "001"`) per VM:

| 1Password field | Value |
|---|---|
| `overview.title` | `<vm_name> (<target_name>)` |
| `overview.url` / `urls[0]` | `ssh://<ip_address>` when the VM has an IP, else empty |
| `overview.tags` | `["forgemill", "<target_name>", "<platform>"]` |
| `loginFields[username]` | login username (`designation: "username"`) |
| `loginFields[password]` | login password (`designation: "password"`), omitted for key logins |
| `details.sections[0]` "Forgemill" | `Sudo password` (concealed, only if included and present), `Private key` (concealed, only if included; key logins only), `Host key fingerprint` (string), `VM ref` (string), `Source` (`vm` / `deployment`), `Set by` / `Set at` |
| `details.notesPlain` | `Exported from Forgemill <version> on <ts> by <actor>. VM id <id>, platform <platform>, OS <os_type>.` |
| `uuid` | 26-char random (1Password's own alphabet); unique within the vault, not stable across exports — 1Password does not de-duplicate imports, so stable ids would buy nothing |
| `createdAt` / `updatedAt` | credential `set_at` where known, else export time |
| `state` | `active`; `favIndex` 0 |

Key logins: 1Password 8 has an *SSH Key* category that would be the better home for a private key. Its category UUID is not in the public format description; the implementation should try it in a round-trip test with a real account and fall back to the concealed section field above if the import rejects it. This is the one open question in the format.

### 4.2 CSV (secondary)

For the 1Password.com web importer and any other manager. The importer is positional and does not reliably read the header row, so the column order is fixed: `title,website,username,password,notes`. RFC 4180 quoting, UTF-8, no BOM, `\r\n` line endings, every row the same field count. Key logins and sudo passwords do not fit CSV: key logins are skipped and the count is reported in the response header `X-Forgemill-Skipped: 4`; sudo passwords, when included, go into the notes column as `sudo password: …`.

Spreadsheet formula injection: titles and notes that begin with `= + - @ \t \r` are prefixed with a single quote. Password cells are never altered, because a mangled password is worse than a formula warning in a spreadsheet nobody should open this file in.

## 5. Server-side handling

**Decrypt late, hold little.** `CredentialExportService.Export(ctx, scope, opts, actor, w)` iterates `ListManagedVMs` (filtered by scope) and resolves each login through the existing `resolveVMCredentials`, so the vm-then-deployment precedence is the same one the executor uses. Items are encoded one at a time into the zip or CSV writer; no temp file. The handler writes into an in-memory buffer (bounded by the 5 000-VM cap) rather than straight to the socket, so that the audit entry can carry the real count and a failure part-way through still gets a proper status instead of a truncated file.

**Audit before body.** `AuditService.Log` is async and drops entries when its 1000-slot buffer is full. A credential export must never be unrecorded, so `AuditService.LogSync(…) error` inserts directly and returns the DB error, and the handler calls it before `WriteHeader`. Entry: action `credentials.export`, resource `credentials`, metadata `{format, scope, vm_count, include_sudo_passwords, include_private_keys, include_deployment_logins, user_agent}`. Denied attempts: `credentials.export.denied` with `{reason: "password" | "rate_limited" | "api_key"}`. The same `LogSync` now guards the existing single-VM reveal (`GET /vms/{id}/credentials`, audit action `vm.credentials.reveal`), which the 12 October sweep found unaudited.

**Tell the other admins.** `NotificationService.EmitForAdmins("warning", "Credentials exported", "<actor> exported logins for <n> VMs (<format>)", "/settings?tab=credentials", "credentials.exported")` and a webhook event `credentials.exported` with the same metadata minus secrets. An admin exporting the whole fleet at 03:00 should wake someone up.

**Logging.** `slog` gets actor, count, format, duration. Never a username/password pair, never a VM-to-username map at debug level.

**Resource bounds.** Scope is capped at 5 000 VMs per request (an installation that large uses the target filter; the preview reports the cap); the request body at 64 KiB; the decrypt loop checks the request context so a client that disconnects stops it.

**No new schema.** Nothing is persisted except the audit entry and the notification. The "last exports" list is `ListAuditLogs(action = credentials.export, limit 10)`.

## 6. Frontend

- `frontend/src/pages/settings/CredentialsExport.tsx`, mounted from `Settings.tsx` as the `credentials` tab, admin-only like `ai` and `diagnostics`, and listed in `config/navigation.ts` / the command palette.
- Preview via `api.get<CredentialExportPreview>("/settings/credentials/export/preview", { params })`, debounced on scope change, cancelled with an `AbortController` when the scope changes again (the Deploy page's target-switch race must not be repeated here).
- Export via `api.post(url, body, { responseType: "blob" })`; on 200 create an object URL, click a hidden anchor, revoke the URL on the next tick. Axios' 401 interceptor currently reloads to `/login` on **any** 401; this endpoint returns 401 for a wrong password, so the interceptor must skip this URL (or the endpoint returns 403 with a `code: "password"` body — pick one; 403 is less disruptive given the current interceptor).
- Password input `type="password" autoComplete="current-password"`, cleared after every attempt, never kept in state longer than the request.
- The whole form sits behind a `useSubmit`-style in-flight guard so a double click cannot produce two audit entries and two downloads.
- Typed role gate: the tab is rendered only for `isAdmin`, and the backend decides; a viewer deep-linking to `?tab=credentials` gets the same admin-only fallback the other admin tabs use.

## 7. Tests

Backend (`internal/api/handlers/credential_export_test.go`, `internal/service/credential_export_test.go`):

- viewer / user → 403; API-key admin (full scope) → 403; JWT admin wrong password → 401 and a `credentials.export.denied` audit row; sixth wrong password in a minute → 429.
- Local admin right password → 200, `Content-Type: application/zip`, zip opens, `export.attributes.version == 3`, `export.data` parses, item count equals exportable count, a key login has no `password` loginField, sudo password present only when requested.
- LDAP admin: `ldap.Authenticate` is called with the posted password and a bcrypt compare is **not** attempted against the empty hash.
- Audit row exists **before** the body is read (use a DB hook that fails the insert and assert the response is 500 with an empty body).
- CSV: quoting of commas, quotes and newlines; formula-prefix sanitising on title/notes only; key logins skipped with the header count.
- Scope filters: target, vm_ids, exclude deployment-inherited logins.
- Notification emitted to every admin, not the actor only; webhook payload has no secret fields (assert by grepping the JSON for the known password).
- `FORGEMILL_CREDENTIAL_EXPORT=disabled` → route absent (404).

Frontend: Playwright state for the tab in both themes (`frontend/e2e`), plus a desktop and a 390 px viewport. Once Vitest exists, a unit test for the blob-download helper and the interceptor exemption.

Release checklist, manual, once: import the generated `.1pux` and `.csv` into a real 1Password account; confirm every field lands where §4.1 says, confirm the SSH Key category question, and screenshot the result into `docs/design/screenshots/`.

## 8. Rollout

- One PR for the backend (service, handler, `authMethodKey`, `LogSync`, tests), one for the frontend, one for docs: README *Security* gets a paragraph (what is exported, how it is gated, that it is audited and notified, the env switch), `forgemill-mcp` README gets the "not exposed" line, CHANGELOG entry.
- No migration. No new config except the optional `FORGEMILL_CREDENTIAL_EXPORT=disabled` kill switch for hardened installs.
- Estimate: backend 1.5 days, frontend 1 day, 1Password round-trip and docs half a day.

## 9. Decisions and alternatives considered

- **Password per export, not a "sudo mode" window.** A five-minute elevated token is friendlier but means a stolen JWT during that window exports the fleet. One password, one file.
- **1PUX over CSV as the default.** CSV loses URLs, tags, sudo passwords and keys, and the importer is positional and header-blind. 1PUX is the native format and the only one that round-trips cleanly.
- **Not encrypting the file.** 1Password cannot import an encrypted archive. The mitigation is the warning, the audit entry, the admin notification and the advice to delete after import. An encrypted-zip option for non-1Password use can come later; it is additive.
- **Fail closed on audit.** An export that cannot be recorded does not happen. This is the one place where losing availability is the right trade.
- **Admin only, no per-user "my VMs" export.** The deployment-inherited login belongs to whoever deployed, but the explicit credential is set by admins; mixing ownership rules here invites the same authorization drift the sweep found elsewhere. Start strict.
