import { useEffect, useState } from "react";
import { settings as settingsApi, ai as aiApi } from "@/api/client";
import type { AITestResult } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select } from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { useToast } from "@/components/ui/toast";
import { getErrorMessage } from "@/lib/utils";
import { Sparkles, FlaskConical, Loader2, ShieldCheck, ShieldAlert, KeyRound, X } from "lucide-react";

/*
  Settings → AI. Off by default. The admin chooses a provider and model and
  supplies a key; the key is write-only (the server stores it encrypted and
  only ever reports that one is set). Everything the model is asked to look
  at is redacted first; the model only reads and drafts — it never runs
  anything. See docs/design/ai-assist.md.
*/

type Form = {
  enabled: boolean;
  provider: "anthropic" | "openai";
  base_url: string;
  model: string;
  key_set: boolean;
  api_key: string; // only while typing a new one
  redact_hostnames: boolean;
  allow_private_endpoint: boolean;
};

const DEFAULTS: Record<Form["provider"], { base: string; models: string[]; hint: string }> = {
  anthropic: {
    base: "https://api.anthropic.com",
    models: ["claude-sonnet-5", "claude-opus-5", "claude-haiku-4-5-20251001"],
    hint: "Anthropic Messages API. Needs an API key.",
  },
  openai: {
    base: "https://api.openai.com/v1",
    models: ["gpt-4.1", "gpt-4.1-mini", "llama3.1:8b", "qwen2.5-coder:14b"],
    hint: "Any OpenAI-compatible endpoint: OpenAI, OpenRouter, vLLM, LM Studio, or Ollama at http://host:11434/v1 (no key needed; tick \"allow private endpoint\" for a LAN address).",
  },
};

export function AISettings() {
  const { toast } = useToast();
  const [form, setForm] = useState<Form>({ enabled: false, provider: "anthropic", base_url: "", model: "", key_set: false, api_key: "", redact_hostnames: false, allow_private_endpoint: false });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [test, setTest] = useState<AITestResult | null>(null);
  const [dirty, setDirty] = useState(false);

  const load = async () => {
    setLoading(true);
    try {
      const res = await settingsApi.get();
      const s = res.data;
      setForm({
        enabled: s.ai_enabled === "true",
        provider: s.ai_provider === "openai" ? "openai" : "anthropic",
        base_url: s.ai_base_url || "",
        model: s.ai_model || "",
        key_set: s.ai_api_key_set === "true",
        api_key: "",
        redact_hostnames: s.ai_redact_hostnames === "true",
        allow_private_endpoint: s.ai_allow_private_endpoint === "true",
      });
      setDirty(false);
    } catch (e: unknown) {
      toast(getErrorMessage(e, "Failed to load AI settings"), "error");
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => { load(); /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, []);

  const update = (patch: Partial<Form>) => { setForm((f) => ({ ...f, ...patch })); setDirty(true); setTest(null); };

  const save = async (): Promise<boolean> => {
    setSaving(true);
    try {
      const body: Record<string, string> = {
        ai_enabled: String(form.enabled),
        ai_provider: form.provider,
        ai_base_url: form.base_url.trim(),
        ai_model: form.model.trim(),
        ai_redact_hostnames: String(form.redact_hostnames),
        ai_allow_private_endpoint: String(form.allow_private_endpoint),
      };
      if (form.api_key) body.ai_api_key = form.api_key;
      const res = await settingsApi.update(body);
      setForm((f) => ({ ...f, api_key: "", key_set: res.data.ai_api_key_set === "true" }));
      setDirty(false);
      toast(form.enabled ? "AI settings saved — assistance is on" : "AI settings saved — assistance is off");
      return true;
    } catch (e: unknown) {
      toast(getErrorMessage(e, "Failed to save AI settings"), "error");
      return false;
    } finally {
      setSaving(false);
    }
  };

  const clearKey = async () => {
    setSaving(true);
    try {
      await settingsApi.update({ ai_api_key: "" });
      setForm((f) => ({ ...f, api_key: "", key_set: false }));
      toast("API key removed");
    } catch (e: unknown) {
      toast(getErrorMessage(e, "Failed to remove the key"), "error");
    } finally {
      setSaving(false);
    }
  };

  // Test saves first (the server tests what is stored), then round-trips.
  const runTest = async () => {
    if (dirty || form.api_key) {
      const ok = await save();
      if (!ok) return;
    }
    setTesting(true);
    setTest(null);
    try {
      const res = await aiApi.test();
      setTest(res.data);
    } catch (e: unknown) {
      setTest({ ok: false, error: getErrorMessage(e, "Test failed") });
    } finally {
      setTesting(false);
    }
  };

  const d = DEFAULTS[form.provider];

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2"><Sparkles className="h-5 w-5" />AI assistance
            <Badge variant={form.enabled ? "success" : "secondary"}>{form.enabled ? "on" : "off"}</Badge>
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-5">
          <div className="rounded-md border bg-muted/40 px-3 py-2.5 text-13 text-muted-foreground space-y-1">
            <p><span className="text-foreground">What it does:</span> reviews an action's script before you save or run it (destructive commands, missing <code className="font-mono">set -e</code>, distro assumptions, secrets in the script, idempotency) and drafts new actions in Forgemill's conventions from a description.</p>
            <p><span className="text-foreground">What it never does:</span> run anything. Saving and running actions stay the same admin-only steps. Every call is a button you click; nothing happens in the background.</p>
            <p><span className="text-foreground">What is sent:</span> the script, name, description and parameter list you are editing, after redaction of passwords, tokens, private keys and URL credentials (optionally also IPs and hostnames). Your provider and key are used; nothing goes to Forgemill. Every call is audit-logged (provider, model, sizes — never content).</p>
          </div>

          {loading ? (
            <p className="text-sm text-muted-foreground flex items-center gap-2"><Loader2 className="h-4 w-4 animate-spin" /> Loading…</p>
          ) : (
            <>
              <label className="flex items-center gap-2 text-sm cursor-pointer">
                <input type="checkbox" className="h-4 w-4" checked={form.enabled} onChange={(e) => update({ enabled: e.target.checked })} />
                Enable AI assistance in the action editor
              </label>

              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label>Provider</Label>
                  <Select value={form.provider} onChange={(e) => update({ provider: e.target.value as Form["provider"], base_url: "", model: "" })}>
                    <option value="anthropic">Anthropic</option>
                    <option value="openai">OpenAI-compatible (OpenAI, Ollama, vLLM, OpenRouter…)</option>
                  </Select>
                  <p className="text-xs text-muted-foreground">{d.hint}</p>
                </div>
                <div className="space-y-2">
                  <Label>Model</Label>
                  <Input list="ai-model-suggestions" value={form.model} onChange={(e) => update({ model: e.target.value })} placeholder={d.models[0]} className="font-mono" />
                  <datalist id="ai-model-suggestions">{d.models.map((m) => <option key={m} value={m} />)}</datalist>
                  <p className="text-xs text-muted-foreground">Any model id your provider accepts. Suggestions are examples, not a list of what's available.</p>
                </div>
                <div className="space-y-2 sm:col-span-2">
                  <Label>Base URL <span className="text-muted-foreground font-normal">(leave empty for the provider default)</span></Label>
                  <Input value={form.base_url} onChange={(e) => update({ base_url: e.target.value })} placeholder={d.base} className="font-mono" />
                </div>
                <div className="space-y-2 sm:col-span-2">
                  <Label className="flex items-center gap-1.5"><KeyRound className="h-3.5 w-3.5" /> API key</Label>
                  <div className="flex items-center gap-2">
                    <Input type="password" autoComplete="new-password" value={form.api_key} onChange={(e) => update({ api_key: e.target.value })} placeholder={form.key_set ? "a key is set — enter a new one to replace it" : form.provider === "openai" ? "optional for local endpoints" : "required"} className="font-mono" />
                    {form.key_set && (
                      <Button size="sm" variant="ghost" className="text-muted-foreground shrink-0" onClick={clearKey} disabled={saving}><X className="h-3 w-3 mr-1" /> Remove key</Button>
                    )}
                  </div>
                  <p className="text-xs text-muted-foreground">Stored AES-256 encrypted; never shown again. {form.key_set && <Badge variant="success" className="ml-1">key set</Badge>}</p>
                </div>
              </div>

              <div className="space-y-2">
                <label className="flex items-center gap-2 text-sm cursor-pointer">
                  <input type="checkbox" className="h-4 w-4" checked={form.redact_hostnames} onChange={(e) => update({ redact_hostnames: e.target.checked })} />
                  Also redact IP addresses and hostnames before sending
                </label>
                <label className="flex items-center gap-2 text-sm cursor-pointer">
                  <input type="checkbox" className="h-4 w-4" checked={form.allow_private_endpoint} onChange={(e) => update({ allow_private_endpoint: e.target.checked })} />
                  Allow a private / LAN endpoint (needed for Ollama or vLLM on your network)
                </label>
              </div>

              <div className="flex items-center gap-2 flex-wrap">
                <Button size="sm" onClick={save} disabled={saving || !dirty && !form.api_key}>{saving && <Loader2 className="h-3 w-3 mr-1 animate-spin" />} Save</Button>
                <Button size="sm" variant="outline" onClick={runTest} disabled={saving || testing || !form.model.trim()}>
                  {testing ? <Loader2 className="h-3 w-3 mr-1 animate-spin" /> : <FlaskConical className="h-3 w-3 mr-1" />} Test connection
                </Button>
                <span className="text-xs text-muted-foreground">Test saves your changes first, then asks the model to reply "OK".</span>
              </div>

              {test && (
                <div className={`rounded-md border px-3 py-2 text-xs flex items-start gap-2 ${test.ok ? "border-success/30 bg-success/5 text-success" : "border-warning/30 bg-warning/5 text-warning"}`}>
                  {test.ok ? <ShieldCheck className="h-4 w-4 shrink-0 mt-px" /> : <ShieldAlert className="h-4 w-4 shrink-0 mt-px" />}
                  <div className="space-y-0.5 min-w-0">
                    {test.ok ? (
                      <p>Connected: {test.model} answered "{test.reply}" in {test.latency_ms} ms.</p>
                    ) : (
                      <p className="break-words">{test.error}</p>
                    )}
                  </div>
                </div>
              )}
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
