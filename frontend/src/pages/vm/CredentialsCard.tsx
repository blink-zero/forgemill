import { useCallback, useEffect, useState } from "react";
import { vms as vmApi } from "@/api/client";
import type { VMCredentials, CredentialCheck, SetVMCredentialsRequest } from "@/types";
import { useToast } from "@/components/ui/toast";
import { useConfirm } from "@/components/ui/confirm-dialog";
import { useAuth } from "@/hooks/useAuth";
import { useTimezone } from "@/hooks/useTimezone";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select } from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { KeyRound, Eye, EyeOff, Copy, Terminal, Pencil, X, Loader2, ShieldCheck, ShieldAlert, ChevronDown, ChevronRight, FlaskConical } from "lucide-react";
import { getErrorMessage, copyText } from "@/lib/utils";

/*
  SSH credentials for one VM. What Forgemill will log in with when it runs an
  action: a login set here on the VM (adopted/registered VMs, or a rotated
  password) or, failing that, the deployment's initial credentials. The
  secret is fetched only on Reveal; a private key is never shown at all.
*/
export function CredentialsCard({ vmId, vmIp }: { vmId: number; vmIp?: string }) {
  const { toast } = useToast();
  const { confirm } = useConfirm();
  const { user } = useAuth();
  const { formatDateTime } = useTimezone();
  const [creds, setCreds] = useState<VMCredentials | null>(null);
  const [showPwd, setShowPwd] = useState(false);
  const [missing, setMissing] = useState(false);
  const [loading, setLoading] = useState(false);
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [form, setForm] = useState<{ username: string; kind: "password" | "private_key"; secret: string; sudoPassword: string }>({ username: "", kind: "password", secret: "", sudoPassword: "" });
  const [showSudoPw, setShowSudoPw] = useState(false);
  const [showHelp, setShowHelp] = useState(false);
  const [testing, setTesting] = useState(false);
  // Result of the last Test / refused Save, shown under the form.
  const [check, setCheck] = useState<CredentialCheck | null>(null);

  // Viewers can't change logins; operators may, depending on the adoption setting (a 403 explains).
  const canWrite = user?.role === "admin" || user?.role === "user";

  const reveal = useCallback(async () => {
    setLoading(true);
    setMissing(false);
    try {
      const res = await vmApi.credentials(vmId);
      setCreds(res.data);
    } catch {
      setCreds(null);
      setMissing(true);
    } finally {
      setLoading(false);
    }
  }, [vmId]);

  // Cheap probe on mount so an adopted VM shows "no credentials" up front
  // instead of hiding it behind Reveal. The secret itself stays masked.
  useEffect(() => { reveal(); }, [reveal]);

  const copyToClipboard = (text: string) =>
    copyText(text).then(() => toast("Copied to clipboard"), (e) => toast(getErrorMessage(e, "Failed to copy"), "error"));

  const permissionHint = (e: unknown, fallback: string) => {
    const msg = getErrorMessage(e, fallback);
    return /insufficient permissions/i.test(msg) ? "Setting VM credentials is limited to admins (change this under Settings → Preferences → VM adoption)" : msg;
  };

  const startEdit = () => {
    setForm({ username: creds?.username || "", kind: "password", secret: "", sudoPassword: "" });
    setShowSudoPw(false);
    setCheck(null);
    setEditing(true);
  };

  const body = (force = false): SetVMCredentialsRequest | null => {
    if (!form.username.trim() || !form.secret.trim()) {
      toast(form.kind === "password" ? "Username and password are required" : "Username and private key are required", "error");
      return null;
    }
    return {
      username: form.username.trim(),
      ...(form.kind === "password" ? { password: form.secret } : { private_key: form.secret }),
      ...(form.sudoPassword ? { sudo_password: form.sudoPassword } : {}),
      ...(force ? { force: true } : {}),
    };
  };

  const test = async () => {
    const b = body();
    if (!b) return;
    setTesting(true);
    setCheck(null);
    try {
      const res = await vmApi.testCredentials(vmId, b);
      setCheck(res.data);
    } catch (e: unknown) {
      toast(permissionHint(e, "Could not test credentials"), "error");
    } finally {
      setTesting(false);
    }
  };

  // Save tries the credentials on the VM first; a failed check is refused
  // with the result shown here, and "Save anyway" sends force.
  const save = async (force = false) => {
    const b = body(force);
    if (!b) return;
    setSaving(true);
    try {
      const res = await vmApi.setCredentials(vmId, b);
      const c = res.data.check;
      toast(c?.skipped ? "SSH credentials saved (not checked — the VM is off or has no address)" : "SSH credentials saved and verified — actions on this VM will use them");
      setEditing(false);
      setShowPwd(false);
      setCheck(null);
      await reveal();
    } catch (e: unknown) {
      const resp = (e as { response?: { status?: number; data?: { check?: CredentialCheck } } }).response;
      if (resp?.status === 422 && resp.data?.check) {
        setCheck(resp.data.check); // refused on evidence: show it, offer Save anyway
      } else {
        toast(permissionHint(e, "Failed to save credentials"), "error");
      }
    } finally {
      setSaving(false);
    }
  };

  const username = form.username.trim() || "USERNAME";
  const sudoersSnippet = `sudo visudo -f /etc/sudoers.d/forgemill-${username}
# add this line, then save:
${username} ALL=(ALL) NOPASSWD:ALL

sudo chmod 440 /etc/sudoers.d/forgemill-${username}
sudo visudo -c
sudo -n true && echo "passwordless sudo OK"`;

  const clear = async () => {
    const ok = await confirm({
      title: "Clear SSH credentials",
      message: "Remove the login set on this VM?",
      consequences: ["Actions will fall back to the deployment's initial credentials, if this VM has any; otherwise they can't run until a login is set again."],
      confirmLabel: "Clear credentials",
    });
    if (!ok) return;
    try {
      await vmApi.clearCredentials(vmId);
      toast("SSH credentials cleared");
      await reveal();
    } catch (e: unknown) {
      toast(permissionHint(e, "Failed to clear credentials"), "error");
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <KeyRound className="h-4 w-4" /> SSH Credentials
          {creds && (
            <Badge variant={creds.source === "vm" ? "info" : "secondary"} title={creds.source === "vm" ? `Set on this VM${creds.set_at ? ` ${formatDateTime(creds.set_at)}` : ""}` : "Initial credentials from the deployment"}>
              {creds.source === "vm" ? (creds.kind === "private_key" ? "key · set on VM" : "set on VM") : "from deployment"}
            </Badge>
          )}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {editing ? (
          <div className="space-y-3">
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">Username</Label>
              <Input value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} placeholder="root" className="font-mono" autoComplete="off" />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">Authentication</Label>
              <Select value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value as "password" | "private_key", secret: "" })}>
                <option value="password">Password</option>
                <option value="private_key">Private key</option>
              </Select>
            </div>
            {form.kind === "password" ? (
              <div className="space-y-1.5">
                <Label className="text-xs text-muted-foreground">Password</Label>
                <Input type="password" value={form.secret} onChange={(e) => setForm({ ...form, secret: e.target.value })} autoComplete="new-password" />
              </div>
            ) : (
              <div className="space-y-1.5">
                <Label className="text-xs text-muted-foreground">Private key (OpenSSH or PEM, unencrypted)</Label>
                <textarea
                  value={form.secret}
                  onChange={(e) => setForm({ ...form, secret: e.target.value })}
                  placeholder={"-----BEGIN OPENSSH PRIVATE KEY-----\n…"}
                  rows={5}
                  spellCheck={false}
                  className="flex w-full rounded-md border border-input bg-card px-3 py-2 text-xs shadow-xs placeholder:text-muted-foreground/80 focus-visible:outline-hidden focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30 font-mono"
                />
              </div>
            )}
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">
                Sudo password {form.kind === "password" ? "(only if different from the login password)" : "(if this user's sudo asks for one)"}
              </Label>
              <div className="flex items-center gap-1">
                <Input type={showSudoPw ? "text" : "password"} value={form.sudoPassword} onChange={(e) => setForm({ ...form, sudoPassword: e.target.value })} autoComplete="new-password" placeholder={form.kind === "password" ? "same as login password" : "leave empty if the user has passwordless sudo"} />
                <Button size="icon" variant="ghost" className="h-7 w-7 shrink-0" onClick={() => setShowSudoPw(!showSudoPw)} aria-label={showSudoPw ? "Hide sudo password" : "Show sudo password"}>
                  {showSudoPw ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
                </Button>
              </div>
            </div>

            <div className="rounded-md border bg-muted/40 px-3 py-2 text-xs text-muted-foreground space-y-1">
              <p>Forgemill uses these credentials to run actions over SSH. <span className="text-foreground">The user must be able to run sudo.</span></p>
              <p>With a password login (or a sudo password above) Forgemill supplies it to sudo automatically. Otherwise the user needs passwordless sudo — you can check with <code className="font-mono">sudo -n true</code>.</p>
              <button type="button" className="inline-flex items-center gap-1 text-primary hover:underline" onClick={() => setShowHelp(!showHelp)}>
                {showHelp ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />} How to set up passwordless sudo on Linux
              </button>
              {showHelp && (
                <div className="space-y-1.5 pt-1">
                  <p>On the VM, as a user that already has sudo:</p>
                  <div className="relative">
                    <pre className="rounded bg-background border px-2.5 py-2 font-mono text-[11px] leading-relaxed overflow-x-auto whitespace-pre">{sudoersSnippet}</pre>
                    <Button size="icon" variant="ghost" className="absolute top-1 right-1 h-6 w-6" onClick={() => copyToClipboard(sudoersSnippet)} aria-label="Copy sudoers instructions">
                      <Copy className="h-3 w-3" />
                    </Button>
                  </div>
                  <p>Replace <code className="font-mono">{username}</code> if you change the username. If sudoers has <code className="font-mono">Defaults requiretty</code> (older RHEL-family defaults), also add <code className="font-mono">Defaults:{username} !requiretty</code> — Forgemill runs without a terminal and cannot work around that.</p>
                </div>
              )}
            </div>

            {check && (
              <div className={`rounded-md border px-3 py-2 text-xs flex items-start gap-2 ${check.ok ? "border-success/30 bg-success/5 text-success" : check.skipped ? "border-border bg-muted/40 text-muted-foreground" : "border-warning/30 bg-warning/5 text-warning"}`}>
                {check.ok ? <ShieldCheck className="h-4 w-4 shrink-0 mt-px" /> : <ShieldAlert className="h-4 w-4 shrink-0 mt-px" />}
                <div className="space-y-1 min-w-0">
                  <p>{check.message}</p>
                  {!check.ok && !check.skipped && check.detail && <p className="font-mono opacity-80 break-all">{check.detail}</p>}
                  {!check.ok && !check.skipped && check.ssh_ok && (
                    <p className="opacity-90">SSH login itself worked{check.checked_via ? ` (${check.checked_via})` : ""}; only sudo is the problem.</p>
                  )}
                </div>
              </div>
            )}

            <div className="flex items-center gap-2 flex-wrap">
              <Button size="sm" onClick={() => save(false)} disabled={saving || testing}>
                {saving && <Loader2 className="h-3 w-3 mr-1 animate-spin" />} Save credentials
              </Button>
              <Button size="sm" variant="outline" onClick={test} disabled={saving || testing}>
                {testing ? <Loader2 className="h-3 w-3 mr-1 animate-spin" /> : <FlaskConical className="h-3 w-3 mr-1" />} Test
              </Button>
              {check && !check.ok && !check.skipped && (
                <Button size="sm" variant="ghost" className="text-warning" onClick={() => save(true)} disabled={saving || testing} title="Store these credentials even though the check failed">
                  Save anyway
                </Button>
              )}
              <Button size="sm" variant="ghost" onClick={() => { setEditing(false); setCheck(null); }} disabled={saving || testing}>
                <X className="h-3 w-3 mr-1" /> Cancel
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              Saving tries the credentials on the VM first (SSH login, then sudo) and refuses ones that can't run actions. Stored AES-256 encrypted; a private key or sudo password is never shown again.
            </p>
          </div>
        ) : missing ? (
          <div className="space-y-2">
            <p className="text-sm text-muted-foreground">No SSH credentials for this VM. Actions can't run until a login is set — the user must be able to run sudo (Forgemill supplies the password if sudo asks for one).</p>
            {canWrite && (
              <Button size="sm" variant="outline" onClick={startEdit}>
                <KeyRound className="h-3 w-3 mr-1" /> Set SSH credentials
              </Button>
            )}
          </div>
        ) : !creds ? (
          <Button size="sm" variant="outline" onClick={reveal} disabled={loading}>
            <Eye className="h-3 w-3 mr-1" /> {loading ? "Loading..." : "Reveal Credentials"}
          </Button>
        ) : (
          <div className="space-y-3">
            <div className="flex items-center gap-2">
              <Label className="w-20 text-xs text-muted-foreground">Username</Label>
              <code className="flex-1 bg-muted px-2 py-1 rounded text-sm">{creds.username}</code>
              <Button size="icon" variant="ghost" className="h-7 w-7" onClick={() => copyToClipboard(creds.username)} aria-label="Copy username">
                <Copy className="h-3 w-3" />
              </Button>
            </div>
            {creds.has_sudo_password && (
              <div className="flex items-center gap-2">
                <Label className="w-20 text-xs text-muted-foreground">Sudo</Label>
                <code className="flex-1 bg-muted px-2 py-1 rounded text-xs text-muted-foreground">sudo password stored · not shown</code>
              </div>
            )}
            {creds.kind === "private_key" ? (
              <div className="flex items-center gap-2">
                <Label className="w-20 text-xs text-muted-foreground">Key</Label>
                <code className="flex-1 bg-muted px-2 py-1 rounded text-xs text-muted-foreground">private key stored · not shown</code>
              </div>
            ) : (
              <div className="flex items-center gap-2">
                <Label className="w-20 text-xs text-muted-foreground">Password</Label>
                <code className="flex-1 bg-muted px-2 py-1 rounded text-sm font-mono">
                  {showPwd
                    ? (creds.password || "").split("").map((ch, i) => (
                        <span
                          key={i}
                          className={
                            /[0-9]/.test(ch)
                              ? "text-info"
                              : /[a-zA-Z]/.test(ch)
                                ? "text-success"
                                : "text-warning"
                          }
                        >
                          {ch}
                        </span>
                      ))
                    : "\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022"}
                </code>
                <Button size="icon" variant="ghost" className="h-7 w-7" onClick={() => setShowPwd(!showPwd)} aria-label={showPwd ? "Hide password" : "Show password"}>
                  {showPwd ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
                </Button>
                <Button size="icon" variant="ghost" className="h-7 w-7" onClick={() => copyToClipboard(creds.password || "")} aria-label="Copy password">
                  <Copy className="h-3 w-3" />
                </Button>
              </div>
            )}
            {vmIp && (
              <Button size="sm" variant="outline" className="w-full mt-1" onClick={() => copyToClipboard(`ssh ${creds.username}@${vmIp}`)}>
                <Terminal className="h-3 w-3 mr-1" /> Copy SSH Command
              </Button>
            )}
            {canWrite && (
              <div className="flex items-center gap-2">
                <Button size="sm" variant="ghost" onClick={startEdit}>
                  <Pencil className="h-3 w-3 mr-1" /> {creds.source === "vm" ? "Change" : "Override"}
                </Button>
                {creds.source === "vm" && (
                  <Button size="sm" variant="ghost" className="text-muted-foreground" onClick={clear}>
                    <X className="h-3 w-3 mr-1" /> Clear
                  </Button>
                )}
              </div>
            )}
            <p className="text-xs text-muted-foreground">
              {creds.source === "vm"
                ? "Login set on this VM — it takes precedence over any deployment credentials."
                : "Initial credentials from the deployment. Stored AES-256 encrypted, decrypted only on reveal."}
            </p>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
