import { useEffect, useState } from "react";
import { ai as aiApi } from "@/api/client";
import { useAIJob } from "@/hooks/useAIJob";
import type { ActionDraft, ActionParameter } from "@/types";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
import { useToast } from "@/components/ui/toast";
import { Sparkles, Loader2, Check, X, AlertTriangle } from "lucide-react";

/*
  Draft with AI: a description in, a complete action out — in Forgemill's
  conventions, validated like a user submission, and already reviewed by the
  linter and the model. "Use this draft" only fills the form; the user still
  reads it and clicks Create. Shown only when AI assistance is on.
*/
export function ActionDraftPanel({ modelName, existingScript, existingParameters, draftActionId, initialPrompt, onSaved, onClose }: {
  modelName?: string;
  existingScript?: string;
  existingParameters?: ActionParameter[];
  /** Regenerate into this draft instead of creating a new one. */
  draftActionId?: number;
  initialPrompt?: string;
  /** Called with the saved draft action's id once the job is done. */
  onSaved: (actionId: number, draft: ActionDraft) => void;
  onClose: () => void;
}) {
  const { toast } = useToast();
  const [prompt, setPrompt] = useState(initialPrompt || "");
  const [useExisting, setUseExisting] = useState(Boolean(existingScript?.trim()));
  const [draft, setDraft] = useState<ActionDraft | null>(null);
  // The draft runs as a background job: polled, refresh-safe, no long request.
  const { job, running: busy, stage, elapsed, error, start } = useAIJob("ai-draft-job");
  useEffect(() => {
    if (job?.status !== "done" || !job.draft) return;
    setDraft(job.draft);
    // Saved server-side as a draft action: hand it to the editor.
    if (!job.draft.refused && job.draft.action_id) onSaved(job.draft.action_id, job.draft);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [job]);

  const generate = async () => {
    if (prompt.trim().length < 8) {
      toast("Describe what the action should do — a sentence or two", "error");
      return;
    }
    setDraft(null);
    await start(() => aiApi.startDraftJob({
      prompt: prompt.trim(),
      platform: "linux",
      ...(useExisting && existingScript?.trim() ? { existing_script: existingScript, existing_parameters: existingParameters } : {}),
      ...(draftActionId ? { draft_action_id: draftActionId } : {}),
    }));
  };

  return (
    <div className="rounded-md border border-primary/30 bg-primary/5 p-3 space-y-3">
      <div className="flex items-start justify-between gap-2">
        <div className="space-y-0.5">
          <p className="text-sm font-medium flex items-center gap-2"><Sparkles className="h-4 w-4 text-primary" /> Draft with AI {modelName && <Badge variant="info">{modelName}</Badge>}</p>
          <p className="text-xs text-muted-foreground">Describe what the action should do. You get a script in Forgemill's conventions (set -e, parameters as variables, idempotent, distro-aware), already checked, saved as a <span className="text-foreground">draft</span> — listed under Drafts, never runnable until you publish it.</p>
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
          {busy && <span className="text-xs text-muted-foreground">{stage === "reviewing" ? "Checking the draft…" : stage === "drafting" ? "Asking the model…" : "Starting…"} {elapsed}s{elapsed >= 45 ? " — large models can take a few minutes. The result is saved as a draft either way; the bell will link to it if you leave." : ""}</span>}
        </div>
        {error && !busy && (
          <div className="rounded-md border border-warning/30 bg-warning/5 px-3 py-2 text-xs text-warning flex items-start gap-2">
            <AlertTriangle className="h-4 w-4 shrink-0 mt-px" />
            <p className="break-words">{error}</p>
          </div>
        )}
      </div>

      {draft && (
        <div className="space-y-2 border-t pt-3">
          {draft.refused ? (
            <div className="rounded-md border border-warning/30 bg-warning/5 px-3 py-2 text-xs text-warning flex items-start gap-2">
              <AlertTriangle className="h-4 w-4 shrink-0 mt-px" />
              <div>
                <p className="font-medium">The model declined to draft this.</p>
                {(draft.warnings || []).map((w, i) => <p key={i}>{w}</p>)}
              </div>
            </div>
          ) : (
            <p className="text-xs text-muted-foreground flex items-center gap-1.5"><Check className="h-3.5 w-3.5 text-success" /> Saved as draft "{draft.name}" — it is loaded in the editor below. Review it, then <span className="text-foreground">Publish</span>, or regenerate with a different description.</p>
          )}
        </div>
      )}
    </div>
  );
}
