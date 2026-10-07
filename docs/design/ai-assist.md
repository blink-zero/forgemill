# AI assistance for actions — design

Status: approved direction (2026-10-06), delivery in three PRs. Scope: **review** and **draft** of post-deploy actions. Nothing else in Forgemill calls a model.

---

## 1. Why these two

Actions are bash run as root over a fleet. Two moments carry most of the risk and most of the typing:

- **Before you run or save** an action you did not write five minutes ago: does it do what the name says, is anything in it destructive, will it work on the second run, does it assume Debian on a Rocky VM, is there a credential pasted into it?
- **When you need an action that doesn't exist**: most people start from a snippet found online and adapt it to Forgemill's conventions (parameters as env vars, idempotent, non-interactive, distro-aware). A good first draft that already follows the conventions saves the adapting.

Everything else the model could do here either duplicates the MCP (natural-language operations, where the human-in-the-loop already lives in the client) or has too much blast radius for the gain (generated Packer/cloud-init).

## 2. Ground rules

These are properties of the implementation, not aspirations:

| rule | how it is enforced |
|---|---|
| **Off by default** | `ai_enabled` setting defaults to false; the UI shows no AI control until an admin turns it on. |
| **Your model, your key** | Provider (`anthropic` \| `openai`-compatible), base URL and model are settings; the key is stored **encrypted** (`ai_api_key_enc`, same AES-256 as target passwords) and is **write-only** — `GET /api/settings` never returns it, only `ai_api_key_set: true`. The `openai` adapter covers OpenAI, Azure-style gateways, OpenRouter, vLLM, LM Studio and **Ollama** (`/v1`), so a fully local setup is one URL. |
| **A person clicks every call** | No background use. Review and draft are buttons in the editor. |
| **The model reads and drafts; it never acts** | The endpoints return text and structured findings. Saving an action and running it stay exactly the same human steps, behind the same RBAC (admin). |
| **Secrets never leave** | `internal/ai/redact` runs on everything sent: passwords in assignments and flags, API keys/tokens by shape (`sk-…`, `ghp_…`, `AKIA…`, JWTs, bearer headers), private-key blocks, URL credentials, and — optionally (`ai_redact_hostnames`) — IPv4/IPv6 addresses and FQDNs. Placeholders are stable (`«REDACTED:password»`) so the model can still reason about structure. The response is **not** un-redacted; the user sees placeholders where secrets were. |
| **Audited** | Every call logs `ai.action.review` / `ai.action.draft` with provider, model, input size, redaction counts and duration — never the content. |
| **Bounded** | Per-call timeout 60 s, max input 64 KB (the action size limit), dedicated rate limit (10 calls/min per instance), max output tokens capped per endpoint. A provider error is a 502 with a plain message; the editor keeps working. |
| **Private endpoints are opt-in** | Base URLs resolving to private ranges are refused unless `ai_allow_private_endpoint` is on (same SSRF guard as webhooks), so a mis-typed URL can't probe the LAN by default. |

## 3. Architecture

```
internal/ai/                     — provider layer, no Forgemill types
  provider.go    Provider interface { Complete(ctx, Request) (Response, error) }, Request{System, User, MaxTokens, JSON bool}
  anthropic.go   Messages API adapter
  openai.go      Chat Completions adapter (OpenAI-compatible; Ollama via /v1)
  redact.go      Redact(text, opts) (clean string, Report)
  config.go      Config{Enabled, Provider, BaseURL, Model, APIKey, RedactHostnames, AllowPrivate} + Validate(); New(cfg) Provider
  http.go        shared client: timeout, SSRF guard, no redirects to other hosts, body limit

internal/service/
  actionlint.go  deterministic checks (no model): missing `set -e`, `rm -rf` on roots/variables, `curl … | sh`, `dd`/`mkfs`/`fdisk`, `chmod 777`, hard-coded IPs, secret-looking literals, interactive commands (`apt-get install` without -y, `passwd`), missing DEBIAN_FRONTEND with apt, parameters referenced but not declared / declared but unused
  ai_assist.go   AIAssistService: ReviewAction, DraftAction; builds prompts, redacts, calls the provider, parses + validates JSON, merges deterministic lint, audits
  ai_prompts.go  system prompts (Forgemill conventions), few-shot shape, JSON schemas as Go types

internal/api/handlers/ai.go
  GET  /api/ai/status            enabled, provider, model, key_set  (any authenticated user — the editor needs to know whether to show the buttons)
  POST /api/ai/test              admin: round-trip "reply with OK" against the configured provider
  POST /api/ai/actions/lint      admin: deterministic findings only (works with AI off)
  POST /api/ai/actions/review    admin: lint + model review  → ActionReview
  POST /api/ai/actions/draft     admin: prompt (+ optional existing script/parameters) → ActionDraft (already linted + validated)

Settings (app_settings, allowlisted): ai_enabled, ai_provider, ai_base_url, ai_model, ai_api_key (write-only → ai_api_key_enc), ai_redact_hostnames, ai_allow_private_endpoint
```

### Review contract

```json
{
  "summary": "Installs nginx from the distro repo and enables it; safe to re-run.",
  "risk": "low | medium | high | critical",
  "findings": [
    { "severity": "critical|high|medium|low|info", "source": "lint|model", "line": 12,
      "title": "Deletes a path built from an unchecked variable",
      "detail": "`rm -rf \"$TARGET_DIR\"/*` runs with root; if TARGET_DIR is empty this is `rm -rf /*`.",
      "suggestion": "Guard with `: \"${TARGET_DIR:?}\"` and refuse `/`." }
  ],
  "distro_support": { "debian": true, "rhel": false, "notes": "uses apt only" },
  "idempotent": true,
  "suggested_parameters": [ { "name": "TARGET_DIR", "label": "Target directory", "type": "string", "required": true } ]
}
```

Deterministic lint findings are always present (source `lint`); model findings are added when AI is on. The risk level is the max of both. The editor shows the panel the same way in both modes, so turning AI on adds depth without changing the workflow.

### Draft contract

```json
{ "name": "Mount NFS share", "description": "…", "category": "scripts",
  "script": "#!/bin/bash\nset -euo pipefail\n…", "parameters": [ … ], "tags": ["nfs","storage"],
  "notes": ["Works on Debian/Ubuntu and RHEL-family; installs nfs-common / nfs-utils accordingly."],
  "warnings": ["Edits /etc/fstab; a wrong export path makes the next boot wait on the mount."] }
```

A draft is validated like a user submission (`ValidateActionScript`, parameter names/types, category enum) and then **linted and reviewed** before it is shown, so the panel presents the draft together with its findings. "Use this draft" fills the editor form; nothing is saved until the user clicks Create.

### Prompting

System prompt states Forgemill's conventions: bash, `set -euo pipefail`, `export DEBIAN_FRONTEND=noninteractive`, parameters are environment variables declared in the parameter list, detect the distro via `/etc/os-release` and branch for apt/dnf, idempotent (check before change), non-interactive, no secrets in scripts (use a `password` parameter), explicit exit codes, short comments. Output is requested as JSON only; the parser tolerates fenced code blocks and retries once with a "JSON only" nudge on a parse failure. Model temperature is low for review, moderate for drafting.

### Failure modes

- AI off → review returns lint only, draft returns 409 "AI assistance is off" (the UI never shows Draft in that state).
- Provider unreachable / 4xx / timeout → 502 with the provider's message trimmed; lint findings still returned on review.
- Model returns unparseable output → 502 "the model did not return a usable answer" after one retry.
- Redaction report counts are returned so the UI can say "3 secrets were redacted before sending".

## 4. UI

- **Settings → AI** (admin tab): enable switch; provider select; base URL (prefilled per provider; hint for Ollama); model (free text with per-provider suggestions); API key (write-only field, "key set · Replace / Clear"); redact hostnames; allow private endpoint; **Test** button (round trip + latency); a plain paragraph of what gets sent and the redaction rules.
- **Action editor**: a **Check** button always present (lint; with AI: full review). Results in a panel under the script: risk badge, findings grouped by severity with line links into the textarea, distro/idempotency chips, "Add suggested parameters". With AI on, a **Draft with AI** panel above the script: prompt, platform hint, Generate → draft + its review → "Use this draft" / "Regenerate" / discard. Drafts are marked in the description field only if the user leaves the generated text; nothing hidden.
- Everything degrades: AI off → only Check (lint).

## 5. Delivery

| PR | contents | gate |
|---|---|---|
| A | `internal/ai` (providers, redaction, http guard, config), settings keys incl. encrypted write-only key, `GET /ai/status`, `POST /ai/test`, Settings → AI tab | redaction tests (every pattern, stability, counts); adapter tests against httptest fakes; settings never echo the key; SSRF guard; screenshot of the tab |
| B | `actionlint.go`, `AIAssistService.ReviewAction`, `/ai/actions/lint` + `/review`, editor Check panel | lint tests per rule; review merges lint + model, survives bad JSON, audits; editor shots in both modes |
| C | `DraftAction`, `/ai/actions/draft`, editor Draft panel; README | draft validated + reviewed before return; UI shot |

Release: **v0.21.0** (minor) with forgemill-mcp **v0.21.0** (lockstep tag; optional `review_action` / `draft_action` tools can follow).
