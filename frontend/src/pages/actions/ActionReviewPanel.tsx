import { useEffect, useMemo, useState } from "react";
import type { ActionReview, ActionFinding, ActionParameter } from "@/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ShieldCheck, ShieldAlert, AlertTriangle, Info, Sparkles, Plus, X, Wrench } from "lucide-react";

/*
  Result of "Check": the deterministic linter's findings, plus the model's
  when AI assistance is on. Same panel either way — turning AI on adds depth
  without changing the workflow. Nothing here blocks saving; it informs.
*/

const RISK_VARIANT: Record<string, "success" | "warning" | "destructive" | "secondary"> = { low: "success", medium: "warning", high: "destructive", critical: "destructive" };
const SEV_ORDER = ["critical", "high", "medium", "low", "info"];
const SEV_CLASS: Record<string, string> = {
  critical: "border-destructive/40 bg-destructive/5",
  high: "border-destructive/30 bg-destructive/5",
  medium: "border-warning/30 bg-warning/5",
  low: "border-border bg-muted/40",
  info: "border-border bg-muted/30",
};
const SEV_BADGE: Record<string, "destructive" | "warning" | "secondary" | "info"> = { critical: "destructive", high: "destructive", medium: "warning", low: "secondary", info: "info" };

export function ActionReviewPanel({ review, aiOn, fixing, fixElapsed, onFix, onJumpToLine, onAddParameters, onClose }: {
  review: ActionReview;
  /** Whether model-backed fixes are available (findings marked fix: "ai" need it). */
  aiOn?: boolean;
  fixing?: boolean;
  fixElapsed?: number;
  /** Fix the ticked findings. Absent = no fix controls (read-only view). */
  onFix?: (selected: ActionFinding[]) => void;
  onJumpToLine?: (line: number) => void;
  onAddParameters?: (params: ActionParameter[]) => void;
  onClose: () => void;
}) {
  // Findings keep their index in review.findings so a selection survives re-grouping.
  const indexed = useMemo(() => review.findings.map((f, idx) => ({ f, idx })), [review.findings]);
  const grouped = SEV_ORDER.map((sev) => ({ sev, items: indexed.filter(({ f }) => f.severity === sev) })).filter((g) => g.items.length > 0);
  const canFix = (f: ActionFinding) => f.fix === "auto" || (f.fix === "ai" && Boolean(aiOn));
  const fixable = indexed.filter(({ f }) => canFix(f));
  const needsAI = indexed.filter(({ f }) => f.fix === "ai" && !aiOn).length;
  const [selected, setSelected] = useState<Set<number>>(() => new Set());
  // A new review means a new set of findings; clear what was ticked.
  useEffect(() => { setSelected(new Set()); }, [review]);
  const toggle = (idx: number) => setSelected((prev) => { const n = new Set(prev); if (n.has(idx)) n.delete(idx); else n.add(idx); return n; });
  const selectAll = () => setSelected(new Set(fixable.map(({ idx }) => idx)));
  const selectedCount = [...selected].filter((idx) => indexed[idx] && canFix(indexed[idx].f)).length;
  const showFix = Boolean(onFix) && review.findings.some((f) => f.fix === "auto" || f.fix === "ai");
  const okIcon = review.risk === "low" ? <ShieldCheck className="h-4 w-4 text-success" /> : <ShieldAlert className={`h-4 w-4 ${review.risk === "medium" ? "text-warning" : "text-destructive"}`} />;

  return (
    <div className="rounded-md border bg-card/60 p-3 space-y-3" aria-live="polite">
      <div className="flex items-start gap-2">
        {okIcon}
        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex items-center gap-2 flex-wrap">
            <span className="text-sm font-medium">Script check</span>
            <Badge variant={RISK_VARIANT[review.risk] || "secondary"}>risk: {review.risk}</Badge>
            {review.lint_only ? (
              <Badge variant="secondary" title="Deterministic checks only. Turn on AI assistance in Settings → AI for a model review.">lint only</Badge>
            ) : (
              <Badge variant="info" title={`Reviewed by ${review.model || "the configured model"}${review.duration_ms ? ` in ${review.duration_ms} ms` : ""}`}><Sparkles className="h-2.5 w-2.5" /> {review.model || "AI"}</Badge>
            )}
            {review.idempotent !== undefined && <Badge variant={review.idempotent ? "success" : "warning"}>{review.idempotent ? "idempotent" : "not idempotent"}</Badge>}
            <Badge variant={review.distro_support.debian && review.distro_support.rhel ? "secondary" : "warning"} title={review.distro_support.notes || ""}>
              {review.distro_support.debian && review.distro_support.rhel ? "Debian + RHEL" : review.distro_support.debian ? "Debian/Ubuntu only" : review.distro_support.rhel ? "RHEL-family only" : "distro: unknown"}
            </Badge>
          </div>
          {review.summary && <p className="text-13 text-muted-foreground">{review.summary}</p>}
          {review.ai_error && (
            <p className="text-xs text-warning flex items-start gap-1.5"><AlertTriangle className="h-3.5 w-3.5 shrink-0 mt-px" /> AI review unavailable: {review.ai_error} Showing the automatic checks only.</p>
          )}
          {review.redaction && review.redaction.total > 0 && (
            <p className="text-xs text-muted-foreground">{review.redaction.total} secret{review.redaction.total === 1 ? "" : "s"} redacted before sending ({Object.entries(review.redaction.counts).map(([k, v]) => `${k} ×${v}`).join(", ")}).</p>
          )}
        </div>
        <Button size="icon" variant="ghost" className="h-7 w-7 shrink-0" onClick={onClose} aria-label="Close check results"><X className="h-3.5 w-3.5" /></Button>
      </div>

      {review.findings.length === 0 ? (
        <p className="text-13 text-muted-foreground">No findings. {review.lint_only ? "The automatic checks found nothing to flag." : "Neither the automatic checks nor the model found anything to flag."}</p>
      ) : (
        <ul className="space-y-1.5">
          {grouped.map(({ sev, items }) => items.map(({ f, idx }) => (
            <li key={`${sev}-${idx}`} className={`rounded-md border px-3 py-2 ${SEV_CLASS[sev] || SEV_CLASS.info}${selected.has(idx) && canFix(f) ? " ring-1 ring-primary/40" : ""}`}>
              <div className="flex items-start gap-2">
                {showFix && (
                  <input
                    type="checkbox"
                    className="mt-1 h-3.5 w-3.5 shrink-0 accent-primary disabled:opacity-40"
                    checked={selected.has(idx) && canFix(f)}
                    disabled={!canFix(f) || fixing}
                    onChange={() => toggle(idx)}
                    aria-label={`Fix: ${f.title}`}
                    title={canFix(f) ? (f.fix === "auto" ? "Fixed by a deterministic edit" : "Fixed by the model") : f.fix === "ai" ? "Needs AI assistance (Settings → AI)" : "No automatic fix for this finding"}
                  />
                )}
                <Badge variant={SEV_BADGE[sev] || "secondary"} className="mt-px shrink-0">{sev}</Badge>
                <div className="min-w-0 flex-1 space-y-0.5">
                  <p className="text-13 font-medium flex items-center gap-2 flex-wrap">
                    {f.title}
                    {f.line ? (
                      <button type="button" className="text-xs font-mono text-primary hover:underline" onClick={() => onJumpToLine?.(f.line!)} title="Jump to line">line {f.line}</button>
                    ) : null}
                    <span className="text-[10px] uppercase tracking-wide text-muted-foreground/70">{f.source === "model" ? "model" : "lint"}</span>
                    {showFix && f.fix === "auto" && <span className="text-[10px] uppercase tracking-wide text-success/80" title="A deterministic edit can fix this">auto-fix</span>}
                    {showFix && f.fix === "ai" && <span className={`text-[10px] uppercase tracking-wide ${aiOn ? "text-primary/80" : "text-muted-foreground/60"}`} title={aiOn ? "The model can fix this" : "Needs AI assistance (Settings → AI)"}>AI fix</span>}
                  </p>
                  {f.detail && <p className="text-xs text-muted-foreground">{f.detail}</p>}
                  {f.suggestion && <p className="text-xs text-foreground/90 flex items-start gap-1"><Info className="h-3 w-3 shrink-0 mt-0.5 text-muted-foreground" /> {f.suggestion}</p>}
                </div>
              </div>
            </li>
          )))}
        </ul>
      )}

      {showFix && (
        <div className="flex items-center gap-2 flex-wrap text-xs" data-testid="action-fix-controls">
          <Button size="sm" variant="outline" className="h-7" disabled={selectedCount === 0 || fixing} onClick={() => onFix?.([...selected].filter((idx) => indexed[idx] && canFix(indexed[idx].f)).map((idx) => indexed[idx].f))} data-testid="action-fix-selected">
            <Wrench className="h-3 w-3 mr-1" /> {fixing ? `Fixing…${fixElapsed ? ` ${fixElapsed}s` : ""}` : `Fix selected${selectedCount > 0 ? ` (${selectedCount})` : ""}`}
          </Button>
          {fixable.length > 0 && selectedCount < fixable.length && !fixing && (
            <button type="button" className="text-primary hover:underline" onClick={selectAll}>Select all fixable ({fixable.length})</button>
          )}
          {selectedCount > 0 && !fixing && <button type="button" className="text-muted-foreground hover:underline" onClick={() => setSelected(new Set())}>Clear</button>}
          <span className="text-muted-foreground">
            {fixable.length === 0 ? "No finding here has an automatic fix." : "Tick findings to fix; the result is shown as a diff before anything changes."}
            {needsAI > 0 ? ` ${needsAI} need${needsAI === 1 ? "s" : ""} AI assistance (Settings → AI).` : ""}
          </span>
        </div>
      )}

      {review.suggested_parameters && review.suggested_parameters.length > 0 && onAddParameters && (
        <div className="flex items-center gap-2 flex-wrap text-xs">
          <span className="text-muted-foreground">Suggested parameters:</span>
          {review.suggested_parameters.map((p) => <code key={p.name} className="font-mono bg-muted px-1.5 py-0.5 rounded">{p.name}<span className="text-muted-foreground">:{p.type}</span></code>)}
          <Button size="sm" variant="outline" className="h-7" onClick={() => onAddParameters(review.suggested_parameters || [])}><Plus className="h-3 w-3 mr-1" /> Add to parameters</Button>
        </div>
      )}
    </div>
  );
}
