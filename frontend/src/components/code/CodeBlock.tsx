import { useMemo, useState } from "react";
import { highlight, type CodeLanguage } from "./highlight";
import { copyText, cn } from "@/lib/utils";
import { useToast } from "@/components/ui/toast";
import { Copy, Check } from "lucide-react";

/*
  Read-only code with syntax colouring, in the app's palette for both themes
  (see .code-block in index.css). Used wherever Forgemill shows a script or
  a structured document: action previews and versions, drafts, manifests.
*/
export function CodeBlock({ code, language = "bash", className, maxHeight = "16rem", lineNumbers = false, copy = true, wrap = false }: {
  code: string;
  language?: CodeLanguage;
  className?: string;
  maxHeight?: string;
  lineNumbers?: boolean;
  /** Copy button in the top-right corner. */
  copy?: boolean;
  wrap?: boolean;
}) {
  const { toast } = useToast();
  const [copied, setCopied] = useState(false);
  const html = useMemo(() => highlight(code, language), [code, language]);
  const lines = useMemo(() => (lineNumbers ? code.split("\n").length : 0), [code, lineNumbers]);

  const doCopy = () =>
    copyText(code).then(() => { setCopied(true); setTimeout(() => setCopied(false), 1500); }, () => toast("Failed to copy", "error"));

  return (
    <div className={cn("code-block relative rounded-md border overflow-hidden", className)}>
      <div className="overflow-auto" style={{ maxHeight }}>
        <div className="flex min-w-full">
          {lineNumbers && (
            <pre aria-hidden="true" className="code-gutter select-none text-right px-2.5 py-3 text-xs leading-5 font-mono">
              {Array.from({ length: lines }, (_, i) => i + 1).join("\n")}
            </pre>
          )}
          <pre className={cn("flex-1 px-3 py-3 text-xs leading-5 font-mono", wrap ? "whitespace-pre-wrap break-words" : "whitespace-pre")}>
            <code className={`language-${language}`} dangerouslySetInnerHTML={{ __html: html }} />
          </pre>
        </div>
      </div>
      {copy && (
        <button type="button" onClick={doCopy} aria-label="Copy code" title="Copy"
          className="absolute top-1.5 right-1.5 p-1.5 rounded-md bg-background/70 text-muted-foreground hover:text-foreground hover:bg-background border border-border/60">
          {copied ? <Check className="h-3.5 w-3.5 text-success" /> : <Copy className="h-3.5 w-3.5" />}
        </button>
      )}
    </div>
  );
}
