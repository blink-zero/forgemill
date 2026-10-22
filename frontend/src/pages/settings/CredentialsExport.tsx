import { useEffect, useMemo, useRef, useState } from "react";
import axios from "axios";
import { credentialExport as exportApi, targets as targetsApi, vms as vmsApi, auditLogs as auditLogsApi } from "@/api/client";
import type { AuditLog } from "@/api/client";
import type { CredentialExportFormat, CredentialExportPreview, ManagedVM, Target } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select } from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { useToast } from "@/components/ui/toast";
import { vmAddress } from "@/lib/vmAddress";
import { TimeWithTooltip } from "@/components/ui/time-with-tooltip";
import { getErrorMessage, timeAgo } from "@/lib/utils";
import { KeyRound, Download, Loader2, ShieldAlert, Search, Check } from "lucide-react";

/*
  Settings → Credentials. Exports every managed VM's SSH login as a file
  1Password imports natively (.1pux) or as a generic CSV. The server owns
  the gate: admin role, an interactive session (never an API key), the
  admin's own password re-entered on every export, a lockout after five
  wrong passwords, and an audit entry written before the first byte. This
  page only makes those rules visible. See docs/design/credential-export.md.
*/

type ScopeKind = "all" | "target" | "vms";

const EXPORT_ACTION = "credentials.export";

// Reads the filename the server chose; falls back to something sensible.
function filenameFrom(disposition: string | undefined, format: CredentialExportFormat): string {
  const m = /filename="([^"]+)"/.exec(disposition ?? "");
  return m?.[1] ?? `forgemill-credentials.${format}`;
}

// Server errors come back as a JSON blob when responseType is "blob".
async function errorFromBlobResponse(e: unknown, fallback: string): Promise<{ message: string; code?: string; status?: number }> {
  if (axios.isAxiosError(e) && e.response?.data instanceof Blob) {
    try {
      const parsed = JSON.parse(await e.response.data.text()) as { error?: string; code?: string };
      return { message: parsed.error || fallback, code: parsed.code, status: e.response.status };
    } catch {
      return { message: fallback, status: e.response.status };
    }
  }
  return { message: getErrorMessage(e, fallback), status: axios.isAxiosError(e) ? e.response?.status : undefined };
}

export function CredentialsExport() {
  const { toast } = useToast();

  // Scope
  const [scopeKind, setScopeKind] = useState<ScopeKind>("all");
  const [targetId, setTargetId] = useState<number>(0);
  const [selectedVMs, setSelectedVMs] = useState<Set<number>>(new Set());
  const [vmSearch, setVmSearch] = useState("");
  const [targetList, setTargetList] = useState<Target[]>([]);
  const [vmList, setVmList] = useState<ManagedVM[]>([]);

  // Options
  const [format, setFormat] = useState<CredentialExportFormat>("1pux");
  const [includeSudo, setIncludeSudo] = useState(false);
  const [includeKeys, setIncludeKeys] = useState(false);
  const [includeDeployment, setIncludeDeployment] = useState(true);

  // Preview, export, history
  const [preview, setPreview] = useState<CredentialExportPreview | null>(null);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [password, setPassword] = useState("");
  const [exporting, setExporting] = useState(false);
  const inFlight = useRef(false);
  const [lockedUntil, setLockedUntil] = useState<number | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const [lastResult, setLastResult] = useState<{ exported: number; skipped: number; filename: string } | null>(null);
  const [history, setHistory] = useState<AuditLog[]>([]);

  useEffect(() => {
    targetsApi.list().then((r) => setTargetList(r.data)).catch((e: unknown) => toast(getErrorMessage(e, "Failed to load targets"), "error"));
    vmsApi.list().then((r) => setVmList(r.data)).catch((e: unknown) => toast(getErrorMessage(e, "Failed to load VMs"), "error"));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const loadHistory = () => {
    auditLogsApi.list({ action: EXPORT_ACTION, page_size: 25 })
      .then((r) => setHistory(r.data.logs.filter((l) => l.action === EXPORT_ACTION).slice(0, 10)))
      .catch((e: unknown) => toast(getErrorMessage(e, "Failed to load export history"), "error"));
  };
  useEffect(loadHistory, []); // eslint-disable-line react-hooks/exhaustive-deps

  // The lockout countdown ticks once a second while it applies.
  useEffect(() => {
    if (lockedUntil === null) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [lockedUntil]);
  const locked = lockedUntil !== null && lockedUntil > now;
  useEffect(() => { if (lockedUntil !== null && !locked) setLockedUntil(null); }, [locked, lockedUntil]);

  const previewParams = useMemo(() => {
    const p: { target_id?: number; vm_ids?: string; include_deployment_logins?: boolean } = { include_deployment_logins: includeDeployment };
    if (scopeKind === "target" && targetId) p.target_id = targetId;
    if (scopeKind === "vms") p.vm_ids = [...selectedVMs].join(",");
    return p;
  }, [scopeKind, targetId, selectedVMs, includeDeployment]);

  // Preview follows the scope; a change in flight cancels the previous call
  // so a slow answer for the old scope cannot land on top of the new one.
  const scopeIncomplete = (scopeKind === "target" && !targetId) || (scopeKind === "vms" && selectedVMs.size === 0);
  useEffect(() => {
    if (scopeIncomplete) { setPreview(null); setPreviewError(null); return; }
    const ctrl = new AbortController();
    const t = setTimeout(() => {
      exportApi.preview(previewParams, ctrl.signal)
        .then((r) => { setPreview(r.data); setPreviewError(null); })
        .catch((e: unknown) => { if (!axios.isCancel(e)) setPreviewError(getErrorMessage(e, "Could not count exportable logins")); });
    }, 250);
    return () => { clearTimeout(t); ctrl.abort(); };
  }, [previewParams, scopeIncomplete]);

  const filteredVMs = useMemo(() => {
    const q = vmSearch.trim().toLowerCase();
    return vmList.filter((vm) => !q || vm.vm_name.toLowerCase().includes(q) || (vm.target_name ?? "").toLowerCase().includes(q) || vmAddress(vm).includes(q));
  }, [vmList, vmSearch]);

  const toggleVM = (id: number) => setSelectedVMs((prev) => { const next = new Set(prev); if (next.has(id)) next.delete(id); else next.add(id); return next; });

  const exportable = preview?.exportable ?? 0;
  const overLimit = preview !== null && exportable > preview.per_request_limit;
  const canExport = !exporting && !locked && password.length > 0 && exportable > 0 && !overLimit && !scopeIncomplete;

  const doExport = async () => {
    if (inFlight.current || !canExport) return;
    inFlight.current = true;
    setExporting(true);
    const pw = password;
    setPassword("");
    try {
      const res = await exportApi.export({
        password: pw,
        format,
        scope: scopeKind === "all" ? { all: true } : scopeKind === "target" ? { target_id: targetId } : { vm_ids: [...selectedVMs] },
        include_sudo_passwords: includeSudo,
        include_private_keys: includeKeys,
        include_deployment_logins: includeDeployment,
      });
      const filename = filenameFrom(res.headers["content-disposition"], format);
      const url = URL.createObjectURL(res.data);
      const a = document.createElement("a");
      a.href = url;
      a.download = filename;
      document.body.appendChild(a);
      a.click();
      a.remove();
      setTimeout(() => URL.revokeObjectURL(url), 0);
      const exported = Number(res.headers["x-forgemill-exported"] ?? exportable);
      const skipped = Number(res.headers["x-forgemill-skipped"] ?? 0);
      setLastResult({ exported, skipped, filename });
      toast(`Exported ${exported} login${exported === 1 ? "" : "s"} to ${filename}`, "success");
      loadHistory();
    } catch (e: unknown) {
      const err = await errorFromBlobResponse(e, "Export failed");
      if (err.status === 429) {
        const retry = axios.isAxiosError(e) ? Number(e.response?.headers?.["retry-after"]) : NaN;
        setLockedUntil(Date.now() + (Number.isFinite(retry) && retry > 0 ? retry : 900) * 1000);
        toast("Too many failed password attempts. Try again later.", "error");
      } else if (err.code === "password") {
        toast("Password not accepted.", "error");
      } else {
        toast(err.message, "error");
      }
    } finally {
      inFlight.current = false;
      setExporting(false);
    }
  };

  const secondsLeft = locked && lockedUntil !== null ? Math.max(1, Math.ceil((lockedUntil - now) / 1000)) : 0;

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2"><KeyRound className="h-5 w-5" />Export VM logins</CardTitle>
        </CardHeader>
        <CardContent className="space-y-5">
          <div className="rounded-md border bg-muted/40 px-3 py-2.5 text-13 text-muted-foreground space-y-1">
            <p><span className="text-foreground">What it exports:</span> the SSH login Forgemill uses for each VM — the credential set on the VM page, or the cloud-init user and password from its deployment. Hypervisor, LDAP and API secrets are never included.</p>
            <p><span className="text-foreground">What it needs:</span> your own password, every time. API keys cannot export. Five wrong passwords lock the export for 15 minutes.</p>
            <p><span className="text-foreground">What it leaves behind:</span> an audit entry (written before the file is sent), a notification to every admin and a <code className="font-mono">credentials.exported</code> webhook.</p>
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label>Scope</Label>
              <Select value={scopeKind} onChange={(e) => setScopeKind(e.target.value as ScopeKind)}>
                <option value="all">All VMs</option>
                <option value="target">One target</option>
                <option value="vms">Selected VMs</option>
              </Select>
            </div>
            {scopeKind === "target" && (
              <div className="space-y-2">
                <Label>Target</Label>
                <Select value={targetId} onChange={(e) => setTargetId(Number(e.target.value))}>
                  <option value={0} disabled>Choose a target…</option>
                  {targetList.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
                </Select>
              </div>
            )}
            <div className="space-y-2">
              <Label>Format</Label>
              <Select value={format} onChange={(e) => setFormat(e.target.value as CredentialExportFormat)}>
                <option value="1pux">1Password (.1pux) — recommended</option>
                <option value="csv">CSV (generic)</option>
              </Select>
              <p className="text-xs text-muted-foreground">
                {format === "1pux"
                  ? "Import in 1Password: File → Import → 1Password Unencrypted Export. Keeps URLs, tags, notes and the extra fields."
                  : "Columns: title, website, username, password, notes. Key logins cannot be represented and are skipped."}
              </p>
            </div>
          </div>

          {scopeKind === "vms" && (
            <div className="space-y-2">
              <Label className="flex items-center justify-between">
                <span>VMs <span className="text-muted-foreground font-normal">({selectedVMs.size} selected)</span></span>
                <span className="flex gap-3 text-xs font-normal">
                  <button type="button" className="text-primary hover:underline" onClick={() => setSelectedVMs(new Set(filteredVMs.map((v) => v.id)))}>Select shown</button>
                  <button type="button" className="text-primary hover:underline" onClick={() => setSelectedVMs(new Set())}>Clear</button>
                </span>
              </Label>
              <div className="relative">
                <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input value={vmSearch} onChange={(e) => setVmSearch(e.target.value)} placeholder="Filter by name, target or IP" className="pl-8" />
              </div>
              <div className="max-h-56 overflow-y-auto rounded-md border divide-y">
                {filteredVMs.length === 0 && <p className="px-3 py-2 text-13 text-muted-foreground">No VMs match.</p>}
                {filteredVMs.map((vm) => (
                  <label key={vm.id} className="flex items-center gap-3 px-3 py-1.5 text-13 cursor-pointer hover:bg-muted/50">
                    <input type="checkbox" className="h-4 w-4" checked={selectedVMs.has(vm.id)} onChange={() => toggleVM(vm.id)} />
                    <span className="font-medium truncate">{vm.vm_name}</span>
                    <span className="text-muted-foreground truncate">{vm.target_name}</span>
                    {vmAddress(vm) && <span className="ml-auto font-mono text-xs text-muted-foreground">{vmAddress(vm)}</span>}
                  </label>
                ))}
              </div>
            </div>
          )}

          <div className="grid gap-2 sm:grid-cols-3">
            <label className="flex items-center gap-2 text-sm cursor-pointer">
              <input type="checkbox" className="h-4 w-4" checked={includeDeployment} onChange={(e) => setIncludeDeployment(e.target.checked)} />
              Include logins inherited from the deployment
            </label>
            <label className="flex items-center gap-2 text-sm cursor-pointer">
              <input type="checkbox" className="h-4 w-4" checked={includeSudo} onChange={(e) => setIncludeSudo(e.target.checked)} />
              Include sudo passwords
            </label>
            <label className="flex items-center gap-2 text-sm cursor-pointer">
              <input type="checkbox" className="h-4 w-4" checked={includeKeys} onChange={(e) => setIncludeKeys(e.target.checked)} disabled={format === "csv"} title={format === "csv" ? "CSV cannot carry private keys" : undefined} />
              Include private keys
            </label>
          </div>

          <div className="rounded-md border px-3 py-2.5 text-13" aria-live="polite">
            {previewError ? (
              <p className="text-destructive">{previewError}</p>
            ) : scopeIncomplete ? (
              <p className="text-muted-foreground">{scopeKind === "target" ? "Choose a target" : "Select at least one VM"} to see what would be exported.</p>
            ) : preview === null ? (
              <p className="text-muted-foreground flex items-center gap-2"><Loader2 className="h-4 w-4 animate-spin" /> Counting…</p>
            ) : (
              <div className="space-y-1">
                <p>
                  <span className="font-medium">{preview.exportable}</span> of {preview.total_vms} VM{preview.total_vms === 1 ? "" : "s"} {preview.exportable === 1 ? "has" : "have"} a login to export
                  {preview.exportable > 0 && (
                    <span className="text-muted-foreground">
                      {" "}— {preview.password_logins} password, {preview.key_logins} key{preview.with_sudo_password > 0 ? `, ${preview.with_sudo_password} with a separate sudo password` : ""}{preview.from_deployment > 0 ? `, ${preview.from_deployment} inherited from a deployment` : ""}
                    </span>
                  )}
                  {preview.skipped_no_login > 0 && <span className="text-muted-foreground">. {preview.skipped_no_login} with no stored login will be skipped</span>}
                  {format === "csv" && preview.key_logins > 0 && <span className="text-muted-foreground">. {preview.key_logins} key login{preview.key_logins === 1 ? "" : "s"} cannot go in a CSV</span>}
                  .
                </p>
                {preview.by_target.length > 1 && scopeKind === "all" && (
                  <p className="text-xs text-muted-foreground">{preview.by_target.map((t) => `${t.name}: ${t.exportable}`).join(" · ")}</p>
                )}
                {overLimit && <p className="text-destructive">More than {preview.per_request_limit} logins in one export. Narrow the scope to a target.</p>}
              </div>
            )}
          </div>

          <div className="rounded-md border border-warning/35 bg-warning/10 px-3 py-2.5 text-13 flex items-start gap-2">
            <ShieldAlert className="h-4 w-4 mt-0.5 shrink-0 text-warning" />
            <p>The file contains plaintext passwords. Import it into your password manager, then delete it. Every export is recorded in the audit log and every admin is notified.</p>
          </div>

          <form className="flex flex-col gap-3 sm:flex-row sm:items-end" onSubmit={(e) => { e.preventDefault(); void doExport(); }}>
            <div className="space-y-2 flex-1">
              <Label htmlFor="export-password">Confirm your password</Label>
              <Input id="export-password" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} disabled={exporting || locked} placeholder={locked ? `Locked for ${secondsLeft}s` : "Your Forgemill password"} />
            </div>
            <Button type="submit" disabled={!canExport} className="sm:w-auto">
              {exporting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Download className="h-4 w-4" />}
              {exporting ? "Exporting…" : locked ? `Locked (${secondsLeft}s)` : "Export"}
            </Button>
          </form>
          {lastResult && (
            <p className="text-13 text-muted-foreground flex items-center gap-1.5"><Check className="h-3.5 w-3.5 text-success" /> Exported {lastResult.exported} login{lastResult.exported === 1 ? "" : "s"}{lastResult.skipped > 0 ? `, skipped ${lastResult.skipped}` : ""} to <span className="font-mono">{lastResult.filename}</span>.</p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Recent exports</CardTitle>
        </CardHeader>
        <CardContent>
          {history.length === 0 ? (
            <p className="text-13 text-muted-foreground">No credential exports yet.</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-13">
                <thead>
                  <tr className="text-left text-muted-foreground">
                    <th className="pb-2 pr-4 font-medium">When</th>
                    <th className="pb-2 pr-4 font-medium">Who</th>
                    <th className="pb-2 pr-4 font-medium">Logins</th>
                    <th className="pb-2 pr-4 font-medium">Format</th>
                    <th className="pb-2 pr-4 font-medium">Scope</th>
                    <th className="pb-2 font-medium">Included</th>
                  </tr>
                </thead>
                <tbody className="divide-y">
                  {history.map((h) => {
                    const m = h.metadata ?? {};
                    const scope = m.scope as { all?: boolean; target_id?: number; vm_ids?: number[] } | undefined;
                    const scopeLabel = scope?.target_id ? (targetList.find((t) => t.id === scope.target_id)?.name ?? `target ${scope.target_id}`) : scope?.vm_ids ? `${scope.vm_ids.length} selected` : "all VMs";
                    const included = [m.include_sudo_passwords ? "sudo" : null, m.include_private_keys ? "keys" : null, m.include_deployment_logins === false ? "explicit only" : null].filter(Boolean).join(", ") || "—";
                    return (
                      <tr key={h.id}>
                        <td className="py-1.5 pr-4 whitespace-nowrap"><TimeWithTooltip iso={h.created_at}>{timeAgo(h.created_at)}</TimeWithTooltip></td>
                        <td className="py-1.5 pr-4">{h.actor}<span className="text-muted-foreground"> · {h.ip_address}</span></td>
                        <td className="py-1.5 pr-4">{String(m.vm_count ?? "?")}</td>
                        <td className="py-1.5 pr-4"><Badge variant="secondary">{String(m.format ?? "").toUpperCase()}</Badge></td>
                        <td className="py-1.5 pr-4">{scopeLabel}</td>
                        <td className="py-1.5 text-muted-foreground">{included}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
