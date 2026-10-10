import { useEffect, useState } from "react";
import { ai as aiApi } from "@/api/client";
import type { ActionDraft, ActionParameter } from "@/types";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
import { useToast } from "@/components/ui/toast";
import { getErrorMessage } from "@/lib/utils";
import { ActionReviewPanel } from "./ActionReviewPanel";
import { Sparkles, Loader2, Check, RefreshCw, X, AlertTriangle, Info } from "lucide-react";

/*
  Draft with AI: a description in, a complete action out — in Forgemill's
  conventions, validated like a user submission, and already reviewed by the
  linter and the model. "Use this draft" only fills the form; the user still
  reads it and clicks Create. Shown only when AI assistance is on.
*/
export function ActionDraftPanel({ modelName, existingScript, existingParameters, onUse, onClose }: {
  modelName?: string;
  existingScript?: string;
  existingParameters?: ActionParameter[];
  onUse: (draft: ActionDraft) => void;
  onClose: () => void;
}) {
  const { toast } = useToast();
  const [prompt, setPrompt] = useState("");
  const [useExisting, setUseExisting] = useState(Boolean(existingScript?.trim()));
  const [busy, setBusy] = useState(false);
  const [draft, setDraft] = useState<ActionDraft | null>(null);
  // Failures stay on screen (a toast can be missed after a long wait).
  const [error, setError] = useState<string | null>(null);
  const [elapsed, setElapsed] = useState(0);
  useEffect(() => {
    if (!busy) return;
    setElapsed(0);
    const t = setInterval(() => setElapsed((n) => n + 1), 1000);
    return () => clearInterval(t);
  }, [busy]);

  const generate = async () => {
    if (prompt.trim().length < 8) {
      toast("Describe what the action should do — a sentence or two", "error");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const res = await aiApi.draftAction({
        prompt: prompt.trim(),
        platform: "linux",
        ...(useExisting && existingScript?.trim() ? { existing_script: existingScript, existing_parameters: existingParameters } : {}),
      });
      setDraft(res.data);
    } catch (e: unknown) {
      const code = (e as { code?: string }).code;
      const msg = code === "ECONNABORTED"
        ? "The request timed out after five minutes. Try a smaller model, a shorter description, or check Settings → AI → Test connection."
        : getErrorMessage(e, "The model could not draft an action");
      setError(msg);
      toast(msg, "error");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="rounded-md border border-primary/30 bg-primary/5 p-3 space-y-3">
      <div className="flex items-start justify-between gap-2">
        <div className="space-y-0.5">
          <p className="text-sm font-medium flex items-center gap-2"><Sparkles className="h-4 w-4 text-primary" /> Draft with AI {modelName && <Badge variant="info">{modelName}</Badge>}</p>
          <p className="text-xs text-muted-foreground">Describe what the action should do. You get a script in Forgemill's conventions (set -e, parameters as variables, idempotent, distro-aware), already checked. Nothing is saved until you click Create.</p>
        </div>
        <Button size="icon" variant="ghost" className="h-7 w-7 shrink-0" onClick={onClose} aria-label="Close draft panel"><X className="h-3.5 w-3.5" /></Button>
      </div>

      <div className="space-y-2">
        <Label className="text-xs">What should it do?</Label>
        <textarea
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          rows={3}
          placeholder={"e.g. Mount an NFS export at a mount point given as a parameter and persist it in fstab. Install the NFS client if missing. Debian and RHEL families."}
          className="w-full rounded-md border border-input bg-card px-3 py-2 text-sm shadow-xs placeholder:text-muted-foreground/70 focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring/30"
        />
        {existingScript?.trim() && (
          <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
            <input type="checkbox" className="h-3.5 w-3.5" checked={useExisting} onChange={(e) => setUseExisting(e.target.checked)} />
            Start from the script currently in the editor (it is redacted before it is sent)
          </label>
        )}
        <div className="flex items-center gap-2">
          <Button size="sm" onClick={generate} disabled={busy || prompt.trim().length < 8}>
            {busy ? <Loader2 className="h-3.5 w-3.5 mr-1 animate-spin" /> : <Sparkles className="h-3.5 w-3.5 mr-1" />} {draft ? "Regenerate" : "Generate draft"}
          </Button>
          {busy && <span className="text-xs text-muted-foreground">Asking the model, then checking the result… {elapsed}s{elapsed >= 45 ? " — large models can take a minute or two" : ""}</span>}
        </div>
        {error && !busy && (
          <div className="rounded-md border border-warning/30 bg-warning/5 px-3 py-2 text-xs text-warning flex items-start gap-2">
            <AlertTriangle className="h-4 w-4 shrink-0 mt-px" />
            <p className="break-words">{error}</p>
          </div>
        )}
      </div>

      {draft && (
        <div className="space-y-3 border-t pt-3">
          {draft.refused ? (
            <div className="rounded-md border border-warning/30 bg-warning/5 px-3 py-2 text-xs text-warning flex items-start gap-2">
              <AlertTriangle className="h-4 w-4 shrink-0 mt-px" />
              <div>
                <p className="font-medium">The model declined to draft this.</p>
                {(draft.warnings || []).map((w, i) => <p key={i}>{w}</p>)}
              </div>
            </div>
          ) : (
            <>
              <div className="flex items-center gap-2 flex-wrap">
                <span className="text-sm font-medium">{draft.name}</span>
                <Badge variant="secondary">{draft.category}</Badge>
                {draft.tags.map((t) => <Badge key={t} variant="outline">{t}</Badge>)}
                <span className="text-xs text-muted-foreground ml-auto">{draft.model}{draft.duration_ms ? ` · ${(draft.duration_ms / 1000).toFixed(1)} s` : ""}</span>
              </div>
              {draft.description && <p className="text-13 text-muted-foreground">{draft.description}</p>}
              <pre className="rounded-md border bg-gray-950 text-success px-3 py-2 text-xs font-mono overflow-x-auto max-h-80 whitespace-pre">{draft.script}</pre>
              {draft.parameters.length > 0 && (
                <div className="text-xs flex items-center gap-2 flex-wrap">
                  <span className="text-muted-foreground">Parameters:</span>
                  {draft.parameters.map((p) => <code key={p.name} className="font-mono bg-muted px-1.5 py-0.5 rounded" title={p.description || ""}>{p.name}<span className="text-muted-foreground">:{p.type}{p.required ? "*" : ""}</span></code>)}
                </div>
              )}
              {(draft.warnings || []).length > 0 && (
                <ul className="text-xs text-warning space-y-0.5">
                  {draft.warnings!.map((w, i) => <li key={i} className="flex items-start gap-1.5"><AlertTriangle className="h-3.5 w-3.5 shrink-0 mt-px" /> {w}</li>)}
                </ul>
              )}
              {(draft.notes || []).length > 0 && (
                <ul className="text-xs text-muted-foreground space-y-0.5">
                  {draft.notes!.map((n, i) => <li key={i} className="flex items-start gap-1.5"><Info className="h-3.5 w-3.5 shrink-0 mt-px" /> {n}</li>)}
                </ul>
              )}
              {draft.review && <ActionReviewPanel review={draft.review} onClose={() => setDraft({ ...draft, review: undefined })} />}
              <div className="flex items-center gap-2">
                <Button size="sm" onClick={() => onUse(draft)}><Check className="h-3.5 w-3.5 mr-1" /> Use this draft</Button>
                <Button size="sm" variant="outline" onClick={generate} disabled={busy}><RefreshCw className="h-3.5 w-3.5 mr-1" /> Regenerate</Button>
                <span className="text-xs text-muted-foreground">Filling the form replaces its current content; review it before you click Create.</span>
              </div>
            </>
          )}
        </div>
      )}
    </div>
  );
}
