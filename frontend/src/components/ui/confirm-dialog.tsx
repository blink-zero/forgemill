import { createContext, useContext, useState, useCallback, useEffect, useRef, type ReactNode } from "react";
import { AlertTriangle } from "lucide-react";
import { Button } from "./button";
import { useFocusTrap } from "@/hooks/useFocusTrap";

interface ConfirmOptions {
  title: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  variant?: "default" | "destructive";
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

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<{
    options: ConfirmOptions;
    resolve: (value: boolean) => void;
  } | null>(null);

  const confirm = useCallback((options: ConfirmOptions) => {
    return new Promise<boolean>((resolve) => {
      setState({ options, resolve });
    });
  }, []);

  const handleResult = (result: boolean) => {
    state?.resolve(result);
    setState(null);
  };

  const focusTrapRef = useFocusTrap<HTMLDivElement>(!!state);

  // Handle Escape key
  useEffect(() => {
    if (!state) return;
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === "Escape") handleResult(false);
    };
    document.addEventListener("keydown", handleEscape);
    return () => document.removeEventListener("keydown", handleEscape);
  }, [state]);

  return (
    <ConfirmContext.Provider value={{ confirm }}>
      {children}
      {state && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={() => handleResult(false)}>
          <div ref={focusTrapRef} className="bg-card border rounded-lg shadow-xl max-w-sm w-full mx-4 p-6 space-y-4" onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true" aria-labelledby="confirm-dialog-title">
            <div className="flex items-start gap-3">
              <div className={`h-10 w-10 rounded-full flex items-center justify-center shrink-0 ${
                state.options.variant === "destructive" ? "bg-destructive/10" : "bg-yellow-500/10"
              }`}>
                <AlertTriangle className={`h-5 w-5 ${
                  state.options.variant === "destructive" ? "text-destructive" : "text-yellow-500"
                }`} />
              </div>
              <div>
                <h3 id="confirm-dialog-title" className="text-lg font-semibold">{state.options.title}</h3>
                <p className="text-sm text-muted-foreground mt-1">{state.options.message}</p>
              </div>
            </div>
            <div className="flex justify-end gap-2">
              <Button variant="outline" size="sm" onClick={() => handleResult(false)}>
                {state.options.cancelLabel || "Cancel"}
              </Button>
              <Button
                variant={state.options.variant === "destructive" ? "destructive" : "default"}
                size="sm"
                onClick={() => handleResult(true)}
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
