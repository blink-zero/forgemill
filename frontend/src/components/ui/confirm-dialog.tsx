import { createContext, useContext, useState, useCallback, useEffect, useRef, type ReactNode } from "react";
import { AlertTriangle, ShieldAlert } from "lucide-react";
import { Button } from "./button";
import { Input } from "./input";
import { useFocusTrap } from "@/hooks/useFocusTrap";

/*
  Confirmation with safe friction.

  Default variant: a plain question — one click to confirm, Enter works.

  Destructive variant adds three guards, each cheap for an intentional user
  and decisive against an accidental one:
    1. Arming delay — the confirm button is inert for the first 600 ms so a
       double-click (or a click that was meant for whatever was underneath)
       can't fall through onto it.
    2. Explicit acknowledgement — a checkbox the user must tick, or, when
       `confirmText` is given, the exact name they must type.
    3. Focus and keys — Cancel is focused on open; Enter never confirms.
  The confirm button only takes the solid `danger` fill once it's actually
  enabled, so the dialog literally reddens as the user commits.
*/
interface ConfirmOptions {
  title: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  variant?: "default" | "destructive";
  /** Concrete, bulleted consequences shown under the message. */
  consequences?: string[];
  /** Require this exact string to be typed before confirming (destructive only). */
  confirmText?: string;
  /** Skip the acknowledgement checkbox (destructive, when confirmText isn't used). */
  acknowledge?: boolean;
}

interface ConfirmContextValue {
  confirm: (options: ConfirmOptions) => Promise<boolean>;
}

const ConfirmContext = createContext<ConfirmContextValue>({
  confirm: () => Promise.resolve(false),
});

export function useConfirm() {
  return useContext(ConfirmContext);
}

const ARM_DELAY_MS = 600;

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<{
    options: ConfirmOptions;
    resolve: (value: boolean) => void;
  } | null>(null);
  const [acked, setAcked] = useState(false);
  const [typed, setTyped] = useState("");
  const [armed, setArmed] = useState(false);
  const cancelRef = useRef<HTMLButtonElement>(null);

  const confirm = useCallback((options: ConfirmOptions) => {
    return new Promise<boolean>((resolve) => {
      setAcked(false);
      setTyped("");
      setArmed(false);
      setState({ options, resolve });
    });
  }, []);

  const handleResult = (result: boolean) => {
    state?.resolve(result);
    setState(null);
  };

  const focusTrapRef = useFocusTrap<HTMLDivElement>(!!state);

  const destructive = state?.options.variant === "destructive";
  const needsText = destructive && !!state?.options.confirmText;
  const needsAck = destructive && !needsText && state?.options.acknowledge !== false;
  const satisfied =
    !destructive ||
    (needsText ? typed.trim() === state?.options.confirmText : true) &&
    (needsAck ? acked : true);
  const canConfirm = satisfied && (!destructive || armed);

  // Arming delay + initial focus on the safe choice.
  useEffect(() => {
    if (!state) return;
    const t = setTimeout(() => setArmed(true), destructive ? ARM_DELAY_MS : 0);
    if (destructive) cancelRef.current?.focus();
    return () => clearTimeout(t);
  }, [state, destructive]);

  // Escape always cancels; Enter confirms only the non-destructive variant.
  useEffect(() => {
    if (!state) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") handleResult(false);
      if (e.key === "Enter" && !destructive && (e.target as HTMLElement)?.tagName !== "BUTTON") handleResult(true);
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state, destructive]);

  return (
    <ConfirmContext.Provider value={{ confirm }}>
      {children}
      {state && (
        <div className="overlay" onClick={() => handleResult(false)}>
          <div
            ref={focusTrapRef}
            className="dialog-panel max-w-md"
            onClick={(e) => e.stopPropagation()}
            role="alertdialog"
            aria-modal="true"
            aria-labelledby="confirm-dialog-title"
            aria-describedby="confirm-dialog-desc"
          >
            {/* Header band: red for destructive, neutral otherwise */}
            <div className={`flex items-start gap-3 px-5 pt-5 pb-4 ${destructive ? "border-b border-destructive/20 bg-destructive/4 rounded-t-lg" : ""}`}>
              <div className={`h-9 w-9 rounded-md flex items-center justify-center shrink-0 border ${
                destructive ? "bg-destructive/10 border-destructive/30 text-destructive" : "bg-warning/10 border-warning/30 text-warning"
              }`}>
                {destructive ? <ShieldAlert className="h-4.5 w-4.5" /> : <AlertTriangle className="h-4.5 w-4.5" />}
              </div>
              <div className="min-w-0">
                {destructive && (
                  <div className="text-2xs font-semibold uppercase tracking-[0.08em] text-destructive mb-0.5">Irreversible action</div>
                )}
                <h3 id="confirm-dialog-title" className="text-[15px] font-semibold leading-tight">{state.options.title}</h3>
                <p id="confirm-dialog-desc" className="text-13 text-muted-foreground mt-1">{state.options.message}</p>
              </div>
            </div>

            <div className="px-5 py-4 space-y-3">
              {state.options.consequences && state.options.consequences.length > 0 && (
                <ul className="text-13 space-y-1">
                  {state.options.consequences.map((c) => (
                    <li key={c} className="flex gap-2">
                      <span className={`mt-[7px] status-dot ${destructive ? "text-destructive" : "text-warning"}`} aria-hidden="true" />
                      <span className="text-foreground/90">{c}</span>
                    </li>
                  ))}
                </ul>
              )}

              {needsText && (
                <div className="space-y-1.5">
                  <label htmlFor="confirm-dialog-text" className="text-13 text-muted-foreground">
                    Type <span className="font-mono font-semibold text-foreground">{state.options.confirmText}</span> to confirm
                  </label>
                  <Input
                    id="confirm-dialog-text"
                    value={typed}
                    onChange={(e) => setTyped(e.target.value)}
                    placeholder={state.options.confirmText}
                    autoComplete="off"
                    spellCheck={false}
                    className="font-mono"
                  />
                </div>
              )}

              {needsAck && (
                <label className="flex items-start gap-2.5 rounded-md border border-border bg-muted/40 px-3 py-2.5 cursor-pointer select-none">
                  <input
                    type="checkbox"
                    className="mt-0.5 h-3.5 w-3.5"
                    checked={acked}
                    onChange={(e) => setAcked(e.target.checked)}
                  />
                  <span className="text-13 text-foreground/90">I understand this can't be undone.</span>
                </label>
              )}
            </div>

            <div className="flex items-center justify-end gap-2 px-5 pb-5">
              <Button ref={cancelRef} variant="outline" size="sm" onClick={() => handleResult(false)}>
                {state.options.cancelLabel || "Cancel"}
              </Button>
              <Button
                variant={destructive ? (canConfirm ? "danger" : "destructive") : "default"}
                size="sm"
                disabled={!canConfirm}
                onClick={() => handleResult(true)}
                aria-disabled={!canConfirm}
              >
                {state.options.confirmLabel || "Confirm"}
              </Button>
            </div>
          </div>
        </div>
      )}
    </ConfirmContext.Provider>
  );
}
