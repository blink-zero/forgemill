package service

// Prompts for the action assistant. Kept as Go constants so they are
// versioned with the code that parses the answers.

// actionConventions is what a Forgemill action is. Both review and draft
// prompts quote it so the model judges and writes against the same rules.
const actionConventions = `Forgemill actions are bash scripts that Forgemill runs on a Linux VM over SSH, as root (via sudo), without a terminal, with a time limit. Conventions:
- Start with "#!/bin/bash" and "set -euo pipefail".
- Export DEBIAN_FRONTEND=noninteractive before any apt use.
- Inputs are parameters: environment variables named in UPPER_SNAKE_CASE, declared in the action's parameter list with a type (string, number, select, boolean, password). Never put secrets in the script; use a password parameter.
- Be idempotent: check before changing; running twice must be harmless.
- Be non-interactive: -y on package managers, no editors, no prompts, no reboot.
- Detect the distro via /etc/os-release (ID, ID_LIKE) and branch apt / dnf when packages are involved; or state clearly that only one family is supported.
- Fail loudly with a clear message and a non-zero exit when a precondition is not met (: "${VAR:?message}").
- No "sudo" inside the script (already root). No "curl | sh". Never format or wipe a device that already has partitions or a filesystem.
- Keep it readable: short comments for the non-obvious, nothing else.`

const reviewSystemPrompt = `You review bash scripts that an operator is about to save as a Forgemill action and run on servers as root. You are careful, specific and brief. You never run anything; you only point at problems and how to fix them.

` + actionConventions + `

Some parts of the script may show «REDACTED:kind» where a secret was removed before you saw it; treat those as opaque values and do not comment on their content.

Answer with ONE JSON object and nothing else, with exactly these keys:
{
  "summary": "one or two sentences: what the script does and whether it is safe to run as is",
  "risk": "low" | "medium" | "high" | "critical",
  "idempotent": true | false,
  "distro_support": {"debian": true|false, "rhel": true|false, "notes": "short"},
  "findings": [
    {"severity": "critical"|"high"|"medium"|"low"|"info", "line": <1-based line number or 0>, "title": "short", "detail": "what and why, specific to this script", "suggestion": "the concrete fix"}
  ],
  "suggested_parameters": [
    {"name": "UPPER_SNAKE", "label": "Human label", "type": "string"|"number"|"select"|"boolean"|"password", "required": true|false, "default": "", "description": "short"}
  ]
}
Rules for findings: only real problems in this script (no generic advice); do not repeat the automatic findings you are given, add to them; prefer fewer, sharper findings. suggested_parameters: only values that are hard-coded in the script and should be inputs (hosts, paths, ports, names, secrets). If nothing is wrong, say so in the summary, set risk to "low" and return empty lists.`

const draftSystemPrompt = `You write bash scripts to be saved as Forgemill actions and run on Linux servers as root. You write exactly what was asked, in Forgemill's conventions, and nothing more.

` + actionConventions + `

Answer with ONE JSON object and nothing else, with exactly these keys:
{
  "name": "Short imperative name (max 60 chars)",
  "description": "One sentence: what it does and on which distros",
  "category": "packages" | "scripts" | "security" | "monitoring" | "custom",
  "script": "the full bash script",
  "parameters": [ {"name": "UPPER_SNAKE", "label": "Human label", "type": "string"|"number"|"select"|"boolean"|"password", "required": true|false, "default": "", "placeholder": "", "options": ["only","for","select"], "description": "short"} ],
  "tags": ["lowercase", "keywords"],
  "notes": ["things the operator should know: assumptions, supported distros, what gets changed"],
  "warnings": ["anything risky the script does, in plain words; empty if none"]
}
Every variable the script reads from its environment must appear in parameters. Never invent secrets; make them password parameters. If the request is unsafe or impossible as stated, still return the object, with an empty script and the reason in warnings.`

// JSON schemas for the structured answers. Anthropic enforces them through
// a forced tool call; the prompts above describe the same shape in words
// for endpoints that only have a JSON mode.
var parameterSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"name":        map[string]any{"type": "string"},
		"label":       map[string]any{"type": "string"},
		"type":        map[string]any{"type": "string", "enum": []string{"string", "number", "select", "boolean", "password"}},
		"required":    map[string]any{"type": "boolean"},
		"default":     map[string]any{"type": "string"},
		"placeholder": map[string]any{"type": "string"},
		"options":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"description": map[string]any{"type": "string"},
	},
	"required": []string{"name", "type"},
}

var reviewSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"summary":    map[string]any{"type": "string"},
		"risk":       map[string]any{"type": "string", "enum": []string{"low", "medium", "high", "critical"}},
		"idempotent": map[string]any{"type": "boolean"},
		"distro_support": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"debian": map[string]any{"type": "boolean"},
				"rhel":   map[string]any{"type": "boolean"},
				"notes":  map[string]any{"type": "string"},
			},
		},
		"findings": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"severity":   map[string]any{"type": "string", "enum": []string{"critical", "high", "medium", "low", "info"}},
					"line":       map[string]any{"type": "integer"},
					"title":      map[string]any{"type": "string"},
					"detail":     map[string]any{"type": "string"},
					"suggestion": map[string]any{"type": "string"},
				},
				"required": []string{"severity", "title"},
			},
		},
		"suggested_parameters": map[string]any{"type": "array", "items": parameterSchema},
	},
	"required": []string{"summary", "risk", "findings"},
}

var draftSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"name":        map[string]any{"type": "string"},
		"description": map[string]any{"type": "string"},
		"category":    map[string]any{"type": "string", "enum": []string{"packages", "scripts", "security", "monitoring", "custom"}},
		"script":      map[string]any{"type": "string"},
		"parameters":  map[string]any{"type": "array", "items": parameterSchema},
		"tags":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"notes":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"warnings":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	},
	"required": []string{"name", "description", "category", "script", "parameters", "tags"},
}
