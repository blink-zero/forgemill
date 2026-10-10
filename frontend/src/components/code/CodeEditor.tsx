import { useCallback, useEffect, useRef } from "react";
import * as RSCE from "react-simple-code-editor";

// react-simple-code-editor ships CommonJS with `exports.default`; depending
// on the bundler's interop the default import is the component or the
// module object. Unwrap either shape.
type EditorComponent = typeof RSCE.default;
const Editor: EditorComponent = (() => {
  const m = RSCE as unknown as { default?: EditorComponent | { default?: EditorComponent } };
  const d = m.default as EditorComponent | { default?: EditorComponent } | undefined;
  if (d && typeof d === "object" && "default" in d && d.default) return d.default;
  if (typeof d === "function") return d;
  return RSCE as unknown as EditorComponent;
})();
import { highlight, type CodeLanguage } from "./highlight";
import { cn } from "@/lib/utils";

/*
  A textarea with syntax colouring: react-simple-code-editor keeps a real
  <textarea> for input (so selection, undo, paste, screen readers and our
  jump-to-line all keep working) and paints the highlighted copy behind it.
*/
export function CodeEditor({ value, onChange, language = "bash", placeholder, className, minRows = 10, maxHeight = "32rem", textareaRef, ariaLabel, disabled }: {
  value: string;
  onChange: (value: string) => void;
  language?: CodeLanguage;
  placeholder?: string;
  className?: string;
  minRows?: number;
  maxHeight?: string;
  /** Receives the underlying textarea, for jump-to-line and focus. */
  textareaRef?: (el: HTMLTextAreaElement | null) => void;
  ariaLabel?: string;
  disabled?: boolean;
}) {
  const container = useRef<HTMLDivElement | null>(null);
  const hl = useCallback((code: string) => highlight(code, language), [language]);

  useEffect(() => {
    const ta = container.current?.querySelector("textarea") ?? null;
    // The library spreads unknown props onto its wrapper div; the label
    // belongs on the input itself.
    if (ta && ariaLabel) ta.setAttribute("aria-label", ariaLabel);
    if (!textareaRef) return;
    textareaRef(ta);
    return () => textareaRef(null);
  }, [textareaRef, ariaLabel]);

  return (
    <div ref={container} className={cn("code-block code-editor rounded-md border overflow-auto", className)} style={{ maxHeight }}>
      <Editor
        value={value}
        onValueChange={onChange}
        highlight={hl}
        padding={12}
        placeholder={placeholder}
        disabled={disabled}
        textareaClassName="focus:outline-hidden"
        preClassName={`language-${language}`}
        textareaId={undefined}
        style={{ fontFamily: "var(--font-mono)", fontSize: "0.8125rem", lineHeight: "1.25rem", minHeight: `${minRows * 1.25 + 1.5}rem` }}
      />
    </div>
  );
}
