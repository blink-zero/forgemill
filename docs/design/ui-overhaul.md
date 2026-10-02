# Forgemill UI overhaul — design spec

Branch: `design/ui-overhaul`. Visual and ergonomic refresh with **strict feature parity**: every input, workflow and operation that existed before exists after, at the same place in the flow. What changes is the surface it sits on and the friction in front of the things that can't be undone.

---

## 1. Design tokens

All tokens live in `frontend/src/index.css` as HSL triples and are consumed through Tailwind as `hsl(var(--token) / <alpha>)`. One token therefore serves solid fills (`bg-success`), soft tints (`bg-success/10`), borders (`border-success/30`) and text (`text-success`) — no parallel "light" variants to keep in sync.

### Surfaces

Two levels, deliberately only two. The page is a step below cards, so hierarchy reads from tone and borders can stay hairline.

| token | light | dark | role |
|---|---|---|---|
| `--background` | `220 20% 97%` | `225 18% 7%` | page |
| `--card` / `--popover` | `0 0% 100%` | `225 16% 10%` / `11%` | raised surface: cards, inputs, dialogs |
| `--muted` | `220 16% 94%` | `225 14% 14%` | sunken: table header, chips, disabled |
| `--accent` | `220 18% 92%` | `225 14% 17%` | hover fill |
| `--border` | `220 14% 88%` | `225 12% 18%` | hairlines |
| `--input` | `220 14% 84%` | `225 12% 22%` | control borders (one step stronger than hairlines) |
| `--sidebar` | `220 18% 95%` | `225 20% 5.5%` | chrome |

Shadows are tokens too (`--shadow-card`, `--shadow-pop`), used via `shadow-card` / `shadow-pop`. In dark mode the card shadow is an inset 1px top highlight — a machined edge rather than a drop shadow, which reads as mud on dark backgrounds.

### Brand & semantic status

| token | light | dark | used for |
|---|---|---|---|
| `--primary` | `226 72% 52%` | `221 88% 66%` | primary actions, active nav, links, focus ring |
| `--success` | `152 62% 34%` | `150 56% 52%` | running / completed / connected |
| `--warning` | `30 92% 40%` | `38 92% 58%` | suspended / pending / cancelled / cautions |
| `--destructive` | `0 70% 46%` | `0 72% 60%` | failed / errors / irreversible actions |
| `--info` | `212 88% 46%` | `212 90% 66%` | informational, targets |

Status colours are tuned for the way they're actually used — **as text on a 10% tint of themselves with a 25–35% border** — so a `success` badge passes contrast on both the page and card surfaces in both themes. Ad-hoc palette classes (`text-green-500`, `bg-yellow-500/5`, `text-yellow-600 dark:text-yellow-400` …) across the pages were swept to these tokens; brand icons (OS, provider) keep their own colours.

### Typography

- **Inter** for UI, **JetBrains Mono** for identifiers (IPs, MACs, refs, keys, typed confirmations). Feature settings `cv11 ss01 cv02` for the single-storey *a* and open digits; `tabular-nums` on every live metric so values don't jitter on the 60s tick.
- Scale: `2xs` 11/16 (labels, badges, column headers), `13` 13/20 (dense UI copy — table cells, buttons, descriptions), `sm` 14 (body), `xl` 20 (page titles), `3xl` (dashboard metrics).
- Column headers are globally 11px semibold uppercase `tracking-[0.06em]` muted — they read as labels, not content.
- Radius tightened to **6px** (`--radius: 0.375rem`); buttons and inputs at 4px.

---

## 2. Layout & hierarchy

- **Header** 48px, hairline bottom border, three zones: breadcrumbs · centred search pill (capped at 24rem) · utilities. Blur-backed so content scrolling under it stays legible.
- **Sidebar** 224px (56px collapsed), one tone below the page, 32px nav rows. Active item: `bg-primary/10` pill + 2px accent bar + heavier icon stroke, so state survives collapse to icons. Section headings 10px tracking-wide; user block and version/collapse controls share one footer row.
- **Content column** capped at 1600px with 20–32px gutters, so wide monitors don't stretch tables into unreadable lines.
- **Page header** — title 20px semibold, one-line description, actions right-aligned on the same baseline, hairline below. Content starts within the first ~100px of the viewport.
- **Cards** — single raised surface, 16px padding, 15px titles. Dashboard metrics are "instrument tiles": uppercase 11px label, 32px tabular value, 32px tinted icon tile using the semantic colour.
- **Tables** — quiet headers (above), 10px row padding, hover tint at `muted/50`, mono for identifiers.
- **Dialogs** — `.overlay` (page-tinted blur) + `.dialog-panel` (popover surface, `shadow-pop`, 140ms ease-in). Destructive dialogs get a red header band.
- **Empty states** — dashed card, 40px icon tile, one sentence, one action.
- **Login** — split layout: brand panel with a faint grid and three capability lines; form at 360px with explicit labels and autocomplete hints. Inputs and placeholders unchanged.

---

## 3. Components (Tailwind CSS)

### Button — `frontend/src/components/ui/button.tsx`

Base: `inline-flex items-center justify-center gap-1.5 rounded-md text-13 font-medium transition-[background-color,border-color,color,box-shadow,transform] duration-150 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 active:translate-y-px`

| variant | classes | use |
|---|---|---|
| `default` | `bg-primary text-primary-foreground edge-highlight shadow-xs hover:bg-primary/90` | the page's one primary action |
| `outline` | `border border-border bg-card text-foreground shadow-xs hover:bg-muted hover:border-input` | secondary |
| `secondary` | `bg-secondary text-secondary-foreground hover:bg-accent` | tertiary |
| `ghost` | `text-muted-foreground hover:bg-accent hover:text-foreground` | row actions |
| **`destructive`** | `border border-destructive/40 bg-destructive/[0.06] text-destructive hover:bg-destructive/[0.12] hover:border-destructive/60` | **every delete / destroy / clear at rest** — outlined, never a solid block |
| **`danger`** | `bg-destructive text-destructive-foreground edge-highlight shadow-xs hover:bg-destructive/90` | **the final commit button inside a confirmation only** |

The split is the core of the guardrail: a high-risk control can't be mistaken for the primary CTA, and the solid red only ever appears after the user has said "yes, this one".

### Badge — `badge.tsx`

`inline-flex items-center gap-1.5 rounded border px-1.5 py-px text-2xs font-medium leading-4` with `border-{tone}/30 bg-{tone}/10 text-{tone}`. Optional `dot` (`.status-dot` — a 6px dot in `currentColor`) and `pulse` for in-progress states give a colour-independent cue.

```tsx
<Badge variant="success" dot>Running</Badge>
<Badge variant="warning" dot pulse>Building</Badge>
<Badge variant="destructive" dot>Failed</Badge>
```

### Input / Select — `input.tsx`, `select.tsx`

`h-9 rounded-md border border-input bg-card text-13 shadow-xs hover:border-muted-foreground/40 focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30 aria-[invalid=true]:border-destructive`. Select is `appearance-none` with an inline chevron so it matches Input exactly.

### Card — `card.tsx`

`rounded-lg border border-border bg-card shadow-card`; header `p-4 pb-3`, title `text-[15px] font-semibold`, content `p-4 pt-0`.

### Key/value rows (utility classes, `index.css`)

```html
<div class="kv-row"><span class="kv-label">IP Address</span><span class="kv-value font-mono">10.20.10.11</span></div>
```

---

## 4. Safe confirmation pattern for critical actions

Two components implement it; both are drop-in for existing flows.

### `DangerZone` / `DangerZoneItem` — `danger-zone.tsx`

Quarantines irreversible actions: one red-tinted enclosure with an explicit label, always at the *end* of its parent (never between routine controls), one row per action with its description and an outlined `destructive` button. An armed action expands beneath its row.

```tsx
<DangerZone description="Both remove this VM from Forgemill. Neither can be undone.">
  <DangerZoneItem title="Untrack VM" description="Forget it here; it keeps running on the hypervisor."
    expanded={mode === "untrack" && <UntrackConfirm />}>
    <Button variant="outline" size="sm" onClick={() => setMode("untrack")}>Untrack VM</Button>
  </DangerZoneItem>
  <DangerZoneItem title="Destroy VM" description="Power off and delete it from the hypervisor."
    expanded={mode === "destroy" && (
      <>
        <Input value={typed} onChange={…} placeholder={vm.name} className="font-mono" />
        <Button variant={typed === vm.name ? "danger" : "destructive"} disabled={typed !== vm.name}>
          {typed === vm.name ? "Confirm Destroy" : "Type VM name to confirm"}
        </Button>
      </>
    )}>
    <Button variant="destructive" size="sm" onClick={() => setMode("destroy")}>Destroy VM</Button>
  </DangerZoneItem>
</DangerZone>
```

Applied to: VM detail (Untrack / Destroy), Settings → Data management (Clear deployment history).

### `useConfirm` — `confirm-dialog.tsx`

```ts
const ok = await confirm({
  title: "Delete User",
  message: `Delete user "${user.username}"?`,
  consequences: ["Their sessions are revoked immediately.", "Their API keys stop working."],
  confirmText: user.username,     // require the exact name to be typed
  confirmLabel: "Delete user",
  variant: "destructive",
});
```

For `variant: "destructive"` the dialog adds three guards — each cheap for an intentional user, decisive against an accidental one:

1. **Arming delay** — the confirm button is inert for 600 ms after open, so a double-click or a click aimed at whatever was underneath can't fall through onto it.
2. **Explicit acknowledgement** — a checkbox (*I understand this can't be undone*), or when `confirmText` is given, the exact name typed into a mono field.
3. **Focus and keys** — Cancel receives focus on open; **Enter never confirms** a destructive dialog; Escape always cancels.

The confirm button renders as outlined `destructive` while disabled and only takes the solid `danger` fill once it's enabled — the dialog literally reddens as the user commits. Non-destructive confirms are unchanged: one click, Enter works.

Applied to: user / webhook / API key / blueprint deletes (with consequences, typed name for users), snapshot revert/delete, action delete, clear history. The target and template delete modals keep their own richer flows (impact preview, untrack vs destroy) restyled on the same surfaces, with the final button on `danger`.

---

## 5. Parity checklist

- No route, page, tab, form field, button, menu item, filter, or keyboard shortcut removed or renamed in a way that changes behaviour. Placeholders are unchanged (automation that targets them keeps working).
- All API calls and their payloads are untouched — this is `frontend/` only.
- Destructive flows do exactly what they did before once confirmed; the additional friction is the only behavioural delta, and it's additive.
- `tsc --noEmit` and `vite build` clean; every primary route exercised in both themes with zero console errors.
