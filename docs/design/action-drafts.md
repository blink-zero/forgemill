# Action drafts — design

Status: approved 2026-10-10, delivered as v0.22.0. Follows `ai-assist.md`.

## Problem

AI drafts and reviews ran as background jobs whose results lived only in the browser session: close the panel, refresh in another tab, restart the server, or wait ten minutes and the draft was gone. The notification's "ready" link opened the Actions page with nothing to show.

## Model

**A draft is an action that has not been published.** `actions.status` is `draft` or `active`; `actions.source` is `user` or `ai`.

- When the model finishes a draft, it is **saved as a draft action** (name, description, category, script, parameters, tags) with its review attached (`review_json`, `reviewed_at`) and the model's notes/warnings/prompt (`draft_meta_json`). It is a normal object from then on: listed under *Drafts* on the Actions page with an *AI draft · not runnable* badge, deep-linkable (`/actions?open=<id>`), visible to every admin, surviving refreshes, tabs and restarts.
- A **draft cannot run**: the executor refuses a draft `action_id`, `POST /api/deploy` refuses `action_ids` that are drafts (so blueprints can't smuggle one in), the default `GET /api/actions` hides drafts (so export, the deploy picker, the VM Actions tab and the MCP never see them unless they ask with `?include_drafts=true`).
- **Publish** (`POST /api/actions/{id}/publish`) runs the same validation as Create on the stored content and flips the status. Versioning starts at publish — edits to a draft overwrite it without snapshots, so drafts create no version noise. **Discard** is the ordinary delete.
- **Reviews attach to actions**: *Check with AI* on a saved action stores the review on it (`review_json` carries a hash of the reviewed script; the UI says "checked before the last edit" when the script has changed since). *Check* on an unsaved new script first saves it as a draft so the review has a home. A content update clears the stored review.
- **Regenerate** overwrites the same draft (the prompt is kept in `draft_meta_json`).
- The bell links to the draft: `AI draft ready: <name>` → `/actions?open=<id>`; failures link to the editor with the reason.

Jobs remain only as the polling mechanism; the result they carry is the action id.

## Why this over persisting jobs

A persisted-job design needs a jobs table, a deep link into a transient object, a per-tab editor cache and a separate "recent drafts" list — four new things to explain. A draft action reuses the list, the editor, the badge system, delete, audit and the MCP that already exist, and strengthens the ground rule: the model's output is stored, visible, reviewable, and never executable until a person publishes it.
