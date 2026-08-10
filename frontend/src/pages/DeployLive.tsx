import { useTimezone } from "@/hooks/useTimezone";
import { useEffect, useState, useRef } from "react";
import { useParams, Link, useLocation, useNavigate } from "react-router-dom";
import { deploy as deployApi } from "@/api/client";
import type { DeploymentManifest, TimelineEvent } from "@/api/client";
import { useWebSocket } from "@/hooks/useWebSocket";
import type { Deployment } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ArrowLeft, CheckCircle, XCircle, Loader2, Copy, Key, AlertTriangle, Monitor, Rocket, History, FileText, User, Terminal } from "lucide-react";
import { useConfirm } from "@/components/ui/confirm-dialog";
import { useToast } from "@/components/ui/toast";
import { getErrorMessage } from "@/lib/utils";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/components/ui/empty-state";

const statusVariant = (status: string) => {
  switch (status) {
    case "completed": return "success" as const;
    case "running": return "default" as const;
    case "failed": return "destructive" as const;
    case "cancelled": return "warning" as const;
    default: return "secondary" as const;
  }
};

type Tab = "overview" | "timeline" | "receipt";

export default function DeployLive() {
  const { formatTime } = useTimezone();
  const navigate = useNavigate();
  const { confirm: showConfirm } = useConfirm();
  const { toast } = useToast();
  const { id } = useParams<{ id: string }>();
  const location = useLocation();
  const credentials = (location.state as { credentials?: { username: string; password: string; ssh_key_injected: boolean } })?.credentials;
  const deployId = Number(id);
  const [deployment, setDeployment] = useState<Deployment | null>(null);
  const [progress, setProgress] = useState(0);
  const [status, setStatus] = useState("pending");
  const [logs, setLogs] = useState<{ level: string; message: string; time: string }[]>([]);
  const [copied, setCopied] = useState(false);
  const [tab, setTab] = useState<Tab>("overview");
  const lastProcessed = useRef(0);
  // 8.13: Track message keys from REST to avoid duplicating with WebSocket
  const seenMessages = useRef(new Set<string>());
  const { messages, connected } = useWebSocket(isNaN(deployId) ? null : deployId);

  // 8.3: Guard against NaN deployment ID
  useEffect(() => {
    if (isNaN(deployId)) return;
    deployApi.get(deployId).then((res) => {
      setDeployment(res.data);
      setStatus(res.data.status);
      if (res.data.status === "completed") setProgress(100);
      if (res.data.logs) {
        const restLogs = res.data.logs.map((l) => {
          const key = `${l.level}:${l.message}`;
          seenMessages.current.add(key);
          return { level: l.level, message: l.message, time: formatTime(l.timestamp) };
        });
        setLogs(restLogs);
      }
    });
  }, [deployId]);

  // 8.2: Only process new messages using ref to track last processed index
  useEffect(() => {
    for (let i = lastProcessed.current; i < messages.length; i++) {
      const msg = messages[i];
      switch (msg.type) {
        case "progress": {
          const data = msg.data as { percent?: number; state?: string; message?: string };
          if (data.percent !== undefined) setProgress(data.percent);
          if (data.state) setStatus(data.state);
          break;
        }
        case "log": {
          const data = msg.data as { level: string; message: string };
          // 8.13: Deduplicate logs already loaded from REST API
          const key = `${data.level}:${data.message}`;
          if (!seenMessages.current.has(key)) {
            setLogs((prev) => [...prev, { ...data, time: formatTime(new Date()) }]);
          }
          seenMessages.current.add(key);
          break;
        }
        case "complete":
          setStatus("completed");
          setProgress(100);
          break;
        case "error":
          setStatus("failed");
          break;
      }
    }
    lastProcessed.current = messages.length;
  }, [messages]);

  const handleCancel = async () => {
    const ok = await showConfirm({ title: "Cancel Deployment", message: "Are you sure you want to cancel this deployment?", confirmLabel: "Cancel Deployment", variant: "destructive" });
    if (!ok) return;
    await deployApi.cancel(deployId);
    setStatus("cancelled");
  };

  const isFinished = ["completed", "failed", "cancelled"].includes(status);

  return (
    <div className="space-y-6">
      <Button variant="ghost" size="sm" onClick={() => navigate("/deploy")}>
        <ArrowLeft className="h-4 w-4 mr-1" /> Back to Deploy
      </Button>
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Deployment #{deployId}</h1>
        <div className="flex items-center gap-2">
          {connected && <span className="text-xs text-green-500">Live</span>}
          <Badge variant={statusVariant(status)}>{status}</Badge>
        </div>
      </div>

      <div className="flex border-b">
        {(["overview", "timeline", "receipt"] as Tab[]).map((t) => (
          <button
            key={t}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
              tab === t
                ? "border-primary text-primary"
                : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
            onClick={() => setTab(t)}
          >
            {t === "overview" ? "Overview" : t === "timeline" ? "Timeline" : "Receipt"}
          </button>
        ))}
      </div>

      {tab === "overview" && (
      <>
      {deployment && (
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle>{deployment.vm_name}</CardTitle>
              {!isFinished && (
                <Button variant="destructive" size="sm" onClick={handleCancel}>Cancel</Button>
              )}
            </div>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="text-sm text-muted-foreground">
              Template: {deployment.template_name} &middot; Target: {deployment.target_name}
            </div>

            <div className="space-y-2">
              <div className="flex items-center justify-between text-sm">
                <span>Progress</span>
                <span>{progress}%</span>
              </div>
              <div className="h-3 rounded-full bg-secondary overflow-hidden">
                <div
                  className={`h-full rounded-full transition-all duration-500 ${status === "failed" ? "bg-destructive" : "bg-primary"}`}
                  style={{ width: `${progress}%` }}
                />
              </div>
            </div>

            {isFinished && (
              <div className={`flex items-center gap-2 p-3 rounded-md ${status === "completed" ? "bg-green-500/10 text-green-500" : "bg-destructive/10 text-destructive"}`}>
                {status === "completed" ? <CheckCircle className="h-5 w-5" /> : <XCircle className="h-5 w-5" />}
                <span className="text-sm font-medium">
                  {status === "completed" ? "Deployment completed successfully" : status === "cancelled" ? "Deployment cancelled" : deployment.error_message || "Deployment failed"}
                </span>
              </div>
            )}

            {!isFinished && (
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <Loader2 className="h-4 w-4 animate-spin" />
                <span>Deploying...</span>
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {credentials && (
        <Card className="border-yellow-500/50">
          <CardHeader className="pb-3">
            <div className="flex items-center gap-2">
              <Key className="h-5 w-5 text-yellow-500" />
              <CardTitle>VM Credentials</CardTitle>
            </div>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex items-center gap-2 p-3 rounded-md bg-yellow-500/10 text-yellow-600 dark:text-yellow-400">
              <AlertTriangle className="h-4 w-4 shrink-0" />
              <span className="text-sm font-medium">Change this password immediately after first login. It will expire on first use.</span>
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1">
                <span className="text-xs text-muted-foreground">Username</span>
                <div className="font-mono text-sm bg-muted rounded-md px-3 py-2">{credentials.username}</div>
              </div>
              <div className="space-y-1">
                <span className="text-xs text-muted-foreground">Password</span>
                <div className="flex items-center gap-2">
                  <div className="font-mono text-sm bg-muted rounded-md px-3 py-2 flex-1">{credentials.password}</div>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      try {
                        if (navigator.clipboard && window.isSecureContext) {
                          navigator.clipboard.writeText(credentials.password);
                        } else {
                          const ta = document.createElement("textarea");
                          ta.value = credentials.password;
                          ta.style.position = "fixed";
                          ta.style.opacity = "0";
                          document.body.appendChild(ta);
                          ta.select();
                          document.execCommand("copy");
                          document.body.removeChild(ta);
                        }
                        toast("Copied to clipboard");
                      } catch (e) { toast(getErrorMessage(e, "Failed to copy"), "error"); }
                      setCopied(true);
                      setTimeout(() => setCopied(false), 2000);
                    }}
                  >
                    <Copy className="h-3 w-3 mr-1" />
                    {copied ? "Copied" : "Copy"}
                  </Button>
                </div>
              </div>
            </div>
            {credentials.ssh_key_injected && (
              <p className="text-xs text-muted-foreground">SSH public key has been injected. You can also connect via SSH key authentication.</p>
            )}
            <div className="space-y-1.5">
              <p className="text-xs text-muted-foreground">Save these credentials now — they are encrypted at rest and can be revealed later from the VM detail page.</p>
              <p className="text-xs text-muted-foreground">The password was auto-generated with cryptographic randomness and injected via cloud-init during deployment.</p>
            </div>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Deployment Log</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="bg-background rounded-md border p-4 max-h-96 overflow-auto font-mono text-xs space-y-1">
            {logs.length === 0 ? (
              <p className="text-muted-foreground">Waiting for logs...</p>
            ) : (
              logs.map((log, i) => (
                <div key={i} className="flex gap-2">
                  <span className="text-muted-foreground shrink-0">{log.time}</span>
                  <span className={log.level === "error" ? "text-destructive" : log.level === "warn" ? "text-yellow-500" : "text-foreground"}>
                    {log.message}
                  </span>
                </div>
              ))
            )}
          </div>
        </CardContent>
      </Card>

      {isFinished && (
        <div className="flex items-center gap-2">
          <Button asChild variant="outline" size="sm">
            <Link to="/vms">
              <Monitor className="h-4 w-4 mr-1" /> View VMs
            </Link>
          </Button>
          <Button asChild variant="outline" size="sm">
            <Link to="/deploy">
              <Rocket className="h-4 w-4 mr-1" /> Deploy Another
            </Link>
          </Button>
          <Button asChild variant="outline" size="sm">
            <Link to="/history">View History</Link>
          </Button>
        </div>
      )}
      </>
      )}

      {tab === "timeline" && <TimelineTab deployId={deployId} />}
      {tab === "receipt" && <ReceiptTab deployId={deployId} />}
    </div>
  );
}

function TimelineTab({ deployId }: { deployId: number }) {
  const [events, setEvents] = useState<TimelineEvent[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const { formatTime } = useTimezone();

  useEffect(() => {
    if (isNaN(deployId)) return;
    setEvents(null);
    setError(null);
    deployApi.timeline(deployId)
      .then((res) => setEvents(res.data || []))
      .catch((e) => setError(getErrorMessage(e, "Failed to load timeline")));
  }, [deployId]);

  if (error) {
    return <EmptyState icon={AlertTriangle} title="Couldn't load timeline" description={error} />;
  }
  if (events === null) {
    return (
      <Card>
        <CardContent className="p-6 space-y-3">
          <Skeleton className="h-4 w-2/3" />
          <Skeleton className="h-4 w-1/2" />
          <Skeleton className="h-4 w-3/4" />
        </CardContent>
      </Card>
    );
  }
  if (events.length === 0) {
    return <EmptyState icon={History} title="No timeline events yet" description="Provisioning log entries and audit events for this deployment will appear here as they happen." />;
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Timeline</CardTitle>
      </CardHeader>
      <CardContent>
        <ol className="space-y-3">
          {events.map((ev, i) => (
            <li key={i} className="flex gap-3">
              <div className="flex flex-col items-center pt-0.5">
                <div className={`h-2.5 w-2.5 rounded-full shrink-0 ${
                  ev.source === "audit" ? "bg-info" : ev.level === "error" ? "bg-destructive" : ev.level === "warn" ? "bg-warning" : "bg-primary"
                }`} />
                {i < events.length - 1 && <div className="w-px flex-1 bg-border mt-1" />}
              </div>
              <div className="pb-3 min-w-0">
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <span>{formatTime(ev.timestamp)}</span>
                  <Badge variant={ev.source === "audit" ? "info" : "secondary"}>{ev.source}</Badge>
                  {ev.actor && (
                    <span className="flex items-center gap-1"><User className="h-3 w-3" />{ev.actor}</span>
                  )}
                </div>
                <p className="text-sm mt-0.5 break-words">{ev.message}</p>
              </div>
            </li>
          ))}
        </ol>
      </CardContent>
    </Card>
  );
}

function ReceiptTab({ deployId }: { deployId: number }) {
  const [manifest, setManifest] = useState<DeploymentManifest | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [showInputs, setShowInputs] = useState(false);
  const { formatTime } = useTimezone();

  useEffect(() => {
    if (isNaN(deployId)) return;
    setManifest(null);
    setError(null);
    deployApi.manifest(deployId)
      .then((res) => setManifest(res.data))
      .catch((e) => setError(getErrorMessage(e, "Failed to load receipt")));
  }, [deployId]);

  if (error) {
    return <EmptyState icon={AlertTriangle} title="Couldn't load receipt" description={error} />;
  }
  if (manifest === null) {
    return (
      <Card>
        <CardContent className="p-6 space-y-3">
          <Skeleton className="h-4 w-2/3" />
          <Skeleton className="h-4 w-1/2" />
          <Skeleton className="h-4 w-3/4" />
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-2">
          <FileText className="h-5 w-5 text-muted-foreground" />
          <CardTitle>Deployment Receipt</CardTitle>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <p className="text-sm">{manifest.what}</p>

        <div className="grid gap-3 sm:grid-cols-2 text-sm">
          <div>
            <span className="text-xs text-muted-foreground block">Triggered by</span>
            <span>{manifest.triggered_by || "—"}</span>
          </div>
          <div>
            <span className="text-xs text-muted-foreground block">Outcome</span>
            <span>
              {manifest.status === "completed" ? "Completed successfully" :
                manifest.status === "failed" ? (manifest.error_message || "Failed") :
                manifest.status === "cancelled" ? "Cancelled by user" : manifest.status}
            </span>
          </div>
          <div>
            <span className="text-xs text-muted-foreground block">Started</span>
            <span>{manifest.started_at ? formatTime(manifest.started_at) : "—"}</span>
          </div>
          <div>
            <span className="text-xs text-muted-foreground block">Completed</span>
            <span>{manifest.completed_at ? formatTime(manifest.completed_at) : "—"}</span>
          </div>
        </div>

        {manifest.has_credentials && manifest.vm_id && (
          <div className="flex items-center gap-2 p-3 rounded-md bg-info/10 text-info text-sm">
            <Key className="h-4 w-4 shrink-0" />
            <span>Credentials were issued for this deployment.</span>
            <Button asChild variant="outline" size="sm" className="ml-auto shrink-0">
              <Link to={`/vms/${manifest.vm_id}`}>View on VM page</Link>
            </Button>
          </div>
        )}

        {manifest.undo_options && manifest.undo_options.length > 0 && (
          <div>
            <span className="text-xs text-muted-foreground block mb-1.5">What you can do next</span>
            <ul className="space-y-1 text-sm">
              {manifest.undo_options.map((opt, i) => (
                <li key={i} className="flex items-start gap-1.5 font-mono text-xs bg-muted rounded-md px-2 py-1.5">
                  <Terminal className="h-3.5 w-3.5 shrink-0 mt-0.5 text-muted-foreground" />
                  {opt}
                </li>
              ))}
            </ul>
            {manifest.vm_id && (
              <Button asChild variant="outline" size="sm" className="mt-2">
                <Link to={`/vms/${manifest.vm_id}`}>Manage this VM</Link>
              </Button>
            )}
          </div>
        )}

        <div>
          <button
            className="text-xs text-muted-foreground hover:text-foreground underline underline-offset-2"
            onClick={() => setShowInputs((v) => !v)}
          >
            {showInputs ? "Hide" : "Show"} raw inputs
          </button>
          {showInputs && (
            <pre className="mt-2 text-xs bg-muted rounded-md p-3 overflow-auto max-h-64">
              {JSON.stringify(manifest.inputs, null, 2)}
            </pre>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
