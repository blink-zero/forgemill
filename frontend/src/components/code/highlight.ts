// Prism with the handful of grammars Forgemill shows: action scripts (bash,
// PowerShell, Python), cloud-init / YAML, JSON manifests, Packer HCL, INI.
// Grammars attach to the global Prism on import, so the order matters:
// core first.
import Prism from "prismjs";
import "prismjs/components/prism-bash";
import "prismjs/components/prism-powershell";
import "prismjs/components/prism-python";
import "prismjs/components/prism-yaml";
import "prismjs/components/prism-json";
import "prismjs/components/prism-ini";
import "prismjs/components/prism-hcl";

export type CodeLanguage = "bash" | "powershell" | "python" | "yaml" | "json" | "ini" | "hcl" | "text";

/** Map Forgemill's script_type / platform hints to a Prism grammar. */
export function languageFor(scriptType?: string): CodeLanguage {
  switch ((scriptType || "").toLowerCase()) {
    case "bash": case "sh": case "shell": return "bash";
    case "powershell": case "ps1": return "powershell";
    case "python": case "py": return "python";
    case "yaml": case "yml": case "cloud-init": return "yaml";
    case "json": return "json";
    case "hcl": case "packer": return "hcl";
    case "ini": return "ini";
    default: return "text";
  }
}

/** Highlighted HTML for `code`, or escaped text for unknown languages. */
export function highlight(code: string, language: CodeLanguage): string {
  const grammar = language === "text" ? null : Prism.languages[language];
  if (!grammar) return escapeHtml(code);
  return Prism.highlight(code, grammar, language);
}

export function escapeHtml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}
