import { useTimezone } from "@/hooks/useTimezone";
import { useState, useEffect, useRef } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { factoryApi } from "@/api/client";
import { useBuildWebSocket } from "@/hooks/useBuildWebSocket";
import type { TemplateBuild, BuildWSMessage } from "@/types";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { ArrowLeft, Square, Loader2, ShieldCheck } from "lucide-react";
import { useToast } from "@/components/ui/toast";
import { getErrorMessage } from "@/lib/utils";

const statusColors: Record<string, string> = {
  pending: "bg-yellow-500/10 text-yellow-500",
  downloading: "bg-blue-500/10 text-blue-500",
  building: "bg-blue-500/10 text-blue-500",
  converting: "bg-blue-500/10 text-blue-500",
  completed: "bg-green-500/10 text-green-500",
  failed: "bg-red-500/10 text-red-500",
  cancelled: "bg-gray-500/10 text-gray-500",
};

const phaseLabels: Record<string, string> = {
  "Preparing build environment": "Setup",
  "Generating Packer template": "Template Generation",
  "Writing build files": "Writing Files",
  "Initializing Packer plugins": "Plugin Init",
  "Running Packer build": "Packer Build",
  "Build completed successfully": "Complete",
};

// Estimate progress percentage from phase + log content
const phaseProgress: Record<string, number> = {
  "Preparing build environment": 5,
  "Generating Packer template": 10,
  "Writing build files": 15,
  "Initializing Packer plugins": 20,
  "Running Packer build": 25,
  "Build completed successfully": 100,
};

function estimateProgress(phase: string, lines: string[], status: string): number {
  if (status === "completed") return 100;
  if (status === "failed" || status === "cancelled") return 0;

  let pct = phaseProgress[phase] || 0;

  // Scan log lines for Packer milestones to refine progress
  for (const line of lines) {
    if (line.includes("Creating CD disk")) pct = Math.max(pct, 28);
    else if (line.includes("Uploading") && line.includes("packer_cache")) pct = Math.max(pct, 32);
    else if (line.includes("Creating virtual machine") || line.includes("Creating VM")) pct = Math.max(pct, 35);
    else if (line.includes("Powering on") || line.includes("Starting VM")) pct = Math.max(pct, 40);
    else if (line.includes("Waiting") && line.includes("boot")) pct = Math.max(pct, 42);
    else if (line.includes("Typing") && line.includes("boot")) pct = Math.max(pct, 45);
    else if (line.includes("Waiting for IP")) pct = Math.max(pct, 48);
    else if (line.includes("IP address:")) pct = Math.max(pct, 55);
    else if (line.includes("Waiting for SSH")) pct = Math.max(pct, 58);
    else if (line.includes("Connected to SSH")) pct = Math.max(pct, 65);
    else if (line.includes("Provisioning with shell")) pct = Math.max(pct, 70);
    else if (line.includes("apt-get") || line.includes("Reading package")) pct = Math.max(pct, 75);
    else if (line.includes("passwd:")) pct = Math.max(pct, 80);
    else if (line.includes("shutdown") || line.includes("Stopping VM")) pct = Math.max(pct, 85);
    else if (line.includes("Converting VM to template") || line.includes("Ejecting CD")) pct = Math.max(pct, 90);
    else if (line.includes("Closing sessions") || line.includes("cloud-init cdrom")) pct = Math.max(pct, 93);
    else if (line.includes("finished after")) pct = Math.max(pct, 97);
    else if (line.includes("Build completed successfully")) pct = 100;
  }

  return pct;
}

export default function FactoryBuildProgress() {
  const { formatDateTime } = useTimezone();
  const { id } = useParams();
  const navigate = useNavigate();
  const { toast } = useToast();
  const buildId = id ? parseInt(id) : null;

  const [build, setBuild] = useState<TemplateBuild | null>(null);
  const [logLines, setLogLines] = useState<string[]>([]);
  const [currentPhase, setCurrentPhase] = useState("");
  const [loading, setLoading] = useState(true);
  const logRef = useRef<HTMLDivElement>(null);
  // I-12: Track last processed message index to handle all intermediate messages
  const lastProcessed = useRef(0);

  const isActive =
    build?.status === "pending" ||
    build?.status === "building" ||
    build?.status === "downloading" ||
    build?.status === "converting";

  const { messages } = useBuildWebSocket(isActive ? buildId : null);

  // Track last WS message time for fallback polling
  const lastWSTime = useRef(0);

  useEffect(() => {
    if (!buildId) return;
    loadBuild();

    // Fallback poll: only fetch from DB when no WS messages in last 5s
    // Covers: returning to tab, WS disconnect, mid-build page open
    const poll = setInterval(() => {
      if (!buildId) return;
      if (Date.now() - lastWSTime.current < 5000) return; // WS is active, skip
      factoryApi.getBuild(buildId).then((res) => {
        setBuild((prev) => {
          // Stop polling once build is terminal
          if (prev && ["completed", "failed", "cancelled"].includes(prev.status)) return prev;
          return res.data;
        });
        if (res.data.packer_log) {
          const dbLines = res.data.packer_log.split("\n").filter((l: string) => l);
          setLogLines((prev) => dbLines.length > prev.length ? dbLines : prev);
        }
      }).catch((e) => {
        toast(getErrorMessage(e, "Failed to fetch build status"), "error");
      });
    }, 5000);

    return () => clearInterval(poll);
  }, [buildId]);

  // I-12: Process all messages since last render, not just the latest
  useEffect(() => {
    if (messages.length === 0) return;

    const newLines: string[] = [];
    let needsReload = false;
    let latestPhase = "";

    lastWSTime.current = Date.now();
    for (let i = lastProcessed.current; i < messages.length; i++) {
      const msg = messages[i] as BuildWSMessage;
      if (msg.type === "log" && msg.data.line) {
        newLines.push(msg.data.line);
      }
      if (msg.type === "progress" && msg.data.phase) {
        latestPhase = msg.data.phase;
      }
      if (msg.type === "complete" || msg.type === "error") {
        needsReload = true;
      }
    }
    lastProcessed.current = messages.length;

    if (newLines.length > 0) {
      setLogLines((prev) => {
        const next = [...prev, ...newLines];
        // Cap at 5000 lines to prevent unbounded memory growth
        if (next.length > 5000) return next.slice(-5000);
        return next;
      });
    }
    if (latestPhase) {
      setCurrentPhase(latestPhase);
    }
    if (needsReload) {
      reloadBuild();
    }
  }, [messages]);

  useEffect(() => {
    if (logRef.current) {
      logRef.current.scrollTop = logRef.current.scrollHeight;
    }
  }, [logLines]);

  const loadBuild = async () => {
    if (!buildId) return;
    try {
      const res = await factoryApi.getBuild(buildId);
      setBuild(res.data);
      // Always populate log from DB — covers returning to tab after navigating away
      if (res.data.packer_log) {
        setLogLines(res.data.packer_log.split("\n").filter((l: string) => l));
      }
    } catch {
      // handle error silently
    } finally {
      setLoading(false);
    }
  };

  const reloadBuild = async () => {
    if (!buildId) return;
    try {
      const res = await factoryApi.getBuild(buildId);
      setBuild(res.data);
      // Update log lines from DB on reload (build complete/error) so log is never lost
      if (res.data.packer_log) {
        setLogLines(res.data.packer_log.split("\n").filter((l: string) => l));
      }
    } catch {
      // handle error silently
    }
  };

  const handleCancel = async () => {
    if (!buildId) return;
    try {
      await factoryApi.cancelBuild(buildId);
      reloadBuild();
    } catch {
      // handle error silently
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader2 className="h-8 w-8 animate-spin text-primary" />
      </div>
    );
  }

  if (!build) {
    return (
      <div className="text-center text-muted-foreground py-12">
        Build not found.
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Button
            variant="ghost"
            size="icon"
            onClick={() => navigate("/factory")}
          >
            <ArrowLeft className="h-4 w-4" />
          </Button>
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-2xl font-bold">{build.template_name}</h1>
              <Badge
                variant="secondary"
                className={statusColors[build.status] || ""}
              >
                {build.status}
              </Badge>
            </div>
            <p className="text-muted-foreground">
              {build.os_definition_id} &middot; {build.target_name} &middot;
              Build #{build.id}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          {isActive && (
            <Button variant="destructive" size="sm" onClick={handleCancel}>
              <Square className="h-4 w-4 mr-1" />
              Cancel Build
            </Button>
          )}
        </div>
      </div>

      {/* Progress bar */}
      {(isActive || build.status === "completed") && (() => {
        const pct = estimateProgress(currentPhase, logLines, build.status);
        return (
          <Card className="p-4 space-y-3">
            <div className="flex items-center justify-between text-sm">
              <div className="flex items-center gap-2">
                {isActive && <Loader2 className="h-4 w-4 animate-spin text-primary" />}
                <span className="font-medium">
                  {build.status === "completed" ? "Build Complete" : phaseLabels[currentPhase] || currentPhase || "Starting..."}
                </span>
              </div>
              <span className="text-muted-foreground">{pct}%</span>
            </div>
            <div className="h-2.5 rounded-full bg-secondary overflow-hidden">
              <div
                className={`h-full rounded-full transition-all duration-700 ease-out ${
                  build.status === "completed" ? "bg-green-500" : "bg-primary"
                }`}
                style={{ width: `${pct}%` }}
              />
            </div>
          </Card>
        );
      })()}

      {/* Error display */}
      {build.status === "failed" && build.error_message && (
        <Card className="p-4 border-red-500/50 bg-red-500/5">
          <p className="text-sm text-red-500 font-medium">Build Failed</p>
          <p className="text-sm text-red-400 mt-1">{build.error_message}</p>
        </Card>
      )}

      {/* Build details */}
      <Card className="p-4">
        <h3 className="font-medium mb-3">Build Details</h3>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-2 text-sm">
          <span className="text-muted-foreground">Started</span>
          <span>
            {build.started_at
              ? formatDateTime(build.started_at)
              : "Not started"}
          </span>
          <span className="text-muted-foreground">Completed</span>
          <span>
            {build.completed_at
              ? formatDateTime(build.completed_at)
              : "-"}
          </span>
        </div>
      </Card>

      {/* Security info */}
      <Card className="p-4">
        <div className="flex items-start gap-2">
          <ShieldCheck className="h-4 w-4 text-green-500 mt-0.5 shrink-0" />
          <div className="text-xs text-muted-foreground space-y-1">
            <p><strong className="text-foreground">Credential handling:</strong> Build credentials are randomly generated per build and automatically locked before the template is finalised. They cannot be used to access deployed VMs.</p>
            <p><strong className="text-foreground">Log redaction:</strong> Hypervisor passwords and sensitive values are automatically redacted from build logs before storage.</p>
            <p><strong className="text-foreground">Target credentials:</strong> Your hypervisor credentials are encrypted at rest (AES-256-GCM) and transmitted only over HTTPS to the target.</p>
          </div>
        </div>
      </Card>

      {/* Build log */}
      <Card className="overflow-hidden">
        <div className="px-4 py-3 border-b border-border">
          <h3 className="font-medium">Build Log</h3>
        </div>
        <div
          ref={logRef}
          className="bg-zinc-950 p-4 font-mono text-xs text-zinc-300 overflow-auto max-h-[500px] min-h-[200px]"
        >
          {logLines.length === 0 ? (
            <span className="text-zinc-500">
              {isActive
                ? "Waiting for build output..."
                : "No log output available."}
            </span>
          ) : (
            logLines.map((line, i) => (
              <div key={i} className="whitespace-pre-wrap">
                {line}
              </div>
            ))
          )}
        </div>
      </Card>
    </div>
  );
}
