import type { ActionReview, ActionFinding, ActionParameter } from "@/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ShieldCheck, ShieldAlert, AlertTriangle, Info, Sparkles, Plus, X } from "lucide-react";

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

export function ActionReviewPanel({ review, onJumpToLine, onAddParameters, onClose }: {
  review: ActionReview;
  onJumpToLine?: (line: number) => void;
  onAddParameters?: (params: ActionParameter[]) => void;
  onClose: () => void;
}) {
  const grouped = SEV_ORDER.map((sev) => ({ sev, items: review.findings.filter((f) => f.severity === sev) })).filter((g) => g.items.length > 0);
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
          {grouped.map(({ sev, items }) => items.map((f: ActionFinding, i: number) => (
            <li key={`${sev}-${i}`} className={`rounded-md border px-3 py-2 ${SEV_CLASS[sev] || SEV_CLASS.info}`}>
              <div className="flex items-start gap-2">
                <Badge variant={SEV_BADGE[sev] || "secondary"} className="mt-px shrink-0">{sev}</Badge>
                <div className="min-w-0 flex-1 space-y-0.5">
                  <p className="text-13 font-medium flex items-center gap-2 flex-wrap">
                    {f.title}
                    {f.line ? (
                      <button type="button" className="text-xs font-mono text-primary hover:underline" onClick={() => onJumpToLine?.(f.line!)} title="Jump to line">line {f.line}</button>
                    ) : null}
                    <span className="text-[10px] uppercase tracking-wide text-muted-foreground/70">{f.source === "model" ? "model" : "lint"}</span>
                  </p>
                  {f.detail && <p className="text-xs text-muted-foreground">{f.detail}</p>}
                  {f.suggestion && <p className="text-xs text-foreground/90 flex items-start gap-1"><Info className="h-3 w-3 shrink-0 mt-0.5 text-muted-foreground" /> {f.suggestion}</p>}
                </div>
              </div>
            </li>
          )))}
        </ul>
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
