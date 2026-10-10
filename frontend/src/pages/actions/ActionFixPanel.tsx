import { useMemo, useState } from "react";
import type { ActionFixResult } from "@/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { CodeBlock } from "@/components/code/CodeBlock";
import { lineDiff, collapseDiff } from "@/lib/lineDiff";
import { Check, X, Wrench, Sparkles, AlertTriangle, Info } from "lucide-react";

/*
  A proposed fix, shown as a diff against what is in the editor. The script
  in the editor does not change until "Apply" — the user reads the diff, the
  per-finding outcome and the fresh check of the result first.
*/
const RISK_VARIANT: Record<string, "success" | "warning" | "destructive" | "secondary"> = { low: "success", medium: "warning", high: "destructive", critical: "destructive" };

export function ActionFixPanel({ before, result, onApply, onDiscard }: {
  before: string;
  result: ActionFixResult;
  onApply: () => void;
  onDiscard: () => void;
}) {
  const [showAll, setShowAll] = useState(false);
  const diff = useMemo(() => lineDiff(before, result.script), [before, result.script]);
  const rows = useMemo(() => (diff ? (showAll ? diff : collapseDiff(diff)) : []), [diff, showAll]);
  const applied = result.changes.filter((c) => c.applied);
  const skipped = result.changes.filter((c) => !c.applied);
  const added = diff ? diff.filter((l) => l.kind === "add").length : 0;
  const removed = diff ? diff.filter((l) => l.kind === "del").length : 0;
  const unchanged = result.script.replace(/\r\n/g, "\n") === before.replace(/\r\n/g, "\n");
  const review = result.review;

  return (
    <div className="rounded-md border border-primary/30 bg-card/60 p-3 space-y-3" aria-live="polite" data-testid="action-fix-panel">
      <div className="flex items-start gap-2">
        <Wrench className="h-4 w-4 text-primary mt-0.5" />
        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex items-center gap-2 flex-wrap">
            <span className="text-sm font-medium">Proposed fix</span>
            {result.used_ai ? (
              <Badge variant="info" title={`Edited by ${result.model || "the configured model"}${result.duration_ms ? ` in ${result.duration_ms} ms` : ""}`}><Sparkles className="h-2.5 w-2.5" /> {result.model || "AI"}</Badge>
            ) : (
              <Badge variant="secondary" title="Deterministic edits only; no model was called.">automatic</Badge>
            )}
            {!unchanged && <Badge variant="secondary">+{added} / −{removed} lines</Badge>}
            {review && <Badge variant={RISK_VARIANT[review.risk] || "secondary"} title="The fixed script was checked again">after fix · risk: {review.risk} · {review.findings.length} finding{review.findings.length === 1 ? "" : "s"}</Badge>}
          </div>
          {unchanged && <p className="text-13 text-warning flex items-start gap-1.5"><AlertTriangle className="h-3.5 w-3.5 shrink-0 mt-px" /> Nothing changed. See the notes below for why.</p>}
          {result.redaction && result.redaction.total > 0 && (
            <p className="text-xs text-muted-foreground">{result.redaction.total} secret{result.redaction.total === 1 ? "" : "s"} redacted before sending; placeholders in the result are where they were — put the real values in parameters.</p>
          )}
        </div>
        <Button size="icon" variant="ghost" className="h-7 w-7 shrink-0" onClick={onDiscard} aria-label="Discard proposed fix"><X className="h-3.5 w-3.5" /></Button>
      </div>

      <ul className="space-y-1 text-xs">
        {applied.map((c, i) => (
          <li key={`a${i}`} className="flex items-start gap-1.5"><Check className="h-3.5 w-3.5 text-success shrink-0 mt-px" /><span><span className="font-medium">{c.title}</span>{c.line ? <span className="text-muted-foreground font-mono"> · line {c.line}</span> : null}{c.note ? <span className="text-muted-foreground"> — {c.note}</span> : null}<span className="text-[10px] uppercase tracking-wide text-muted-foreground/70 ml-1.5">{c.by}</span></span></li>
        ))}
        {skipped.map((c, i) => (
          <li key={`s${i}`} className="flex items-start gap-1.5"><AlertTriangle className="h-3.5 w-3.5 text-warning shrink-0 mt-px" /><span><span className="font-medium">{c.title}</span>{c.line ? <span className="text-muted-foreground font-mono"> · line {c.line}</span> : null}<span className="text-muted-foreground"> — not fixed{c.note ? `: ${c.note}` : ""}</span></span></li>
        ))}
        {(result.notes || []).map((n, i) => (
          <li key={`n${i}`} className="flex items-start gap-1.5 text-muted-foreground"><Info className="h-3.5 w-3.5 shrink-0 mt-px" /><span>{n}</span></li>
        ))}
      </ul>

      {result.parameters_added && result.parameters_added.length > 0 && (
        <div className="flex items-center gap-2 flex-wrap text-xs">
          <span className="text-muted-foreground">Parameters the fix adds:</span>
          {result.parameters_added.map((p) => <code key={p.name} className="font-mono bg-muted px-1.5 py-0.5 rounded">{p.name}<span className="text-muted-foreground">:{p.type}</span></code>)}
        </div>
      )}

      {!unchanged && (diff ? (
        <div className="code-block rounded-md border bg-muted/30 overflow-x-auto text-xs font-mono leading-5 max-h-96 overflow-y-auto" data-testid="action-fix-diff">
          {rows.map((r, i) => r.kind === "skip" ? (
            <button key={i} type="button" className="block w-full text-left px-3 text-muted-foreground bg-muted/40 hover:bg-muted/60" onClick={() => setShowAll(true)}>… {r.count} unchanged line{r.count === 1 ? "" : "s"}</button>
          ) : (
            <div key={i} className={`flex ${r.kind === "add" ? "bg-success/10" : r.kind === "del" ? "bg-destructive/10" : ""}`}>
              <span className="w-10 shrink-0 text-right pr-2 select-none text-muted-foreground/60">{r.oldNo ?? ""}</span>
              <span className="w-10 shrink-0 text-right pr-2 select-none text-muted-foreground/60">{r.newNo ?? ""}</span>
              <span className={`w-4 shrink-0 select-none ${r.kind === "add" ? "text-success" : r.kind === "del" ? "text-destructive" : "text-muted-foreground/50"}`}>{r.kind === "add" ? "+" : r.kind === "del" ? "−" : " "}</span>
              <span className={`whitespace-pre pr-3 ${r.kind === "del" ? "line-through decoration-destructive/50" : ""}`}>{r.text || " "}</span>
            </div>
          ))}
        </div>
      ) : (
        <CodeBlock code={result.script} language="bash" lineNumbers className="max-h-96" />
      ))}

      <div className="flex items-center gap-2 flex-wrap">
        <Button size="sm" onClick={onApply} disabled={unchanged && !(result.parameters_added && result.parameters_added.length > 0)} data-testid="action-fix-apply"><Check className="h-3.5 w-3.5 mr-1" /> Apply to editor</Button>
        <Button size="sm" variant="outline" onClick={onDiscard}>Discard</Button>
        <span className="text-xs text-muted-foreground">Applying replaces the script and keeps the fresh check; nothing is saved until you save the action.</span>
      </div>
    </div>
  );
}
