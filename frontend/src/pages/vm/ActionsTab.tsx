import { useTimezone } from "@/hooks/useTimezone";
import { useEffect, useState, useRef, useCallback } from "react";
import { executions as execApi, actions as actionsApi } from "@/api/client";
import { usePageSize } from "@/hooks/usePageSize";
import { useConfirm } from "@/components/ui/confirm-dialog";
import { useToast } from "@/components/ui/toast";
import type { Action, ActionExecution } from "@/types";
import { Select } from "@/components/ui/select";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Play, RefreshCw, Copy, Terminal, X, CheckCircle, XCircle, Loader2, AlertTriangle, Settings2,
} from "lucide-react";
import { Pagination } from "@/components/ui/pagination";
import { getErrorMessage, copyText } from "@/lib/utils";
import { executionStatusVariant } from "@/lib/status";

/** Actions tab: run catalogue/ad-hoc scripts on the VM over the execution WebSocket and browse history. */
export function ActionsTab({ vmId, vmPowerState }: { vmId: number; vmPowerState: string }) {
  const { formatDateTime } = useTimezone();
  const { confirm: showConfirm } = useConfirm();
  const { toast } = useToast();
  const [availableActions, setAvailableActions] = useState<Action[]>([]);
  const [executionHistory, setExecutionHistory] = useState<ActionExecution[]>([]);
  const [adHocScript, setAdHocScript] = useState("");
  const [executing, setExecuting] = useState(false);
  const [error, setError] = useState("");
  const [activeExecution, setActiveExecution] = useState<ActionExecution | null>(null);
  const [outputLines, setOutputLines] = useState<string[]>([]);
  const [execStatus, setExecStatus] = useState<string>("");
  const [execExitCode, setExecExitCode] = useState<number | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const outputRef = useRef<HTMLDivElement>(null);
  const [expandedExecId, setExpandedExecId] = useState<number | null>(null);
  const [actionSearch, setActionSearch] = useState("");
  const [actionCategoryFilter, setActionCategoryFilter] = useState<string>("all");
  const [historyPage, setHistoryPage] = useState(1);
  const [historyPerPage, setHistoryPerPage] = usePageSize("vmdetail_executions", 10);
  // Action cards are paged so the tab doesn't become a wall of 20+ cards;
  // 9 / 12 fit the 3-column grid exactly. Filter first, then page.
  const [actionPage, setActionPage] = useState(1);
  const [actionsPerPage, setActionsPerPage] = usePageSize("vmdetail_actions", 9, [9, 12, 24]);
  useEffect(() => { setActionPage(1); }, [actionSearch, actionCategoryFilter, actionsPerPage]);
  const [paramAction, setParamAction] = useState<Action | null>(null);
  const [paramValues, setParamValues] = useState<Record<string, string>>({});

  const isPoweredOn = vmPowerState === "poweredOn" || vmPowerState === "running";

  const loadData = useCallback(() => {
    actionsApi.list().then((res) => setAvailableActions(res.data || []));
    execApi.list(vmId).then((res) => setExecutionHistory(res.data || []));
  }, [vmId]);

  useEffect(() => {
    loadData();
  }, [loadData]);

  // Auto-scroll output to bottom
  useEffect(() => {
    if (outputRef.current) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight;
    }
  }, [outputLines]);

  const connectWS = (executionId: number) => {
    const token = localStorage.getItem("forgemill_token") || "";
    if (!token) return;

    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(
      `${protocol}//${window.location.host}/api/ws/execution/${executionId}`,
      [`token.${token}`]
    );

    ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data);
        if (msg.type === "output" && msg.data?.line !== undefined) {
          setOutputLines((prev) => {
            const next = [...prev, msg.data.line];
            if (next.length > 5000) return next.slice(-5000);
            return next;
          });
        } else if (msg.type === "status" && msg.data?.status) {
          setExecStatus(msg.data.status);
          if (msg.data.exit_code !== undefined) {
            setExecExitCode(msg.data.exit_code);
          }
          if (["completed", "failed", "cancelled"].includes(msg.data.status)) {
            loadData();
          }
        } else if (msg.type === "error" && msg.data?.message) {
          setOutputLines((prev) => [...prev, `[ERROR] ${msg.data.message}`]);
          setExecStatus("failed");
          loadData();
        }
      } catch {
        // ignore malformed
      }
    };

    ws.onerror = (e) => {
      console.error("WS error:", e);
    };

    ws.onclose = (e) => {
      console.log("WS closed:", e.code, e.reason, "wasClean:", e.wasClean);
      // If closed before any output, try polling the execution once
      if (e.code !== 1000 && e.code !== 1005) {
        // Abnormal close — fetch execution output from API as fallback
        execApi.get(executionId).then((res) => {
          if (res.data?.output) {
            setOutputLines(res.data.output.split("\n"));
          }
          if (res.data?.status) {
            setExecStatus(res.data.status);
            if (res.data.exit_code !== undefined) {
              setExecExitCode(res.data.exit_code);
            }
          }
          loadData();
        }).catch((e) => {
          toast(getErrorMessage(e, "Failed to fetch execution output"), "error");
        });
      }
    };

    wsRef.current = ws;
  };

  const doExecuteAction = async (actionId: number) => {
    const action = availableActions.find((a) => a.id === actionId);
    if (action?.parameters && action.parameters.length > 0) {
      // Has parameters — show parameter modal instead of executing directly
      const defaults: Record<string, string> = {};
      for (const p of action.parameters) {
        if (p.default) defaults[p.name] = p.default;
        else if (p.type === "boolean") defaults[p.name] = "false";
        else defaults[p.name] = "";
      }
      setParamValues(defaults);
      setParamAction(action);
      return;
    }
    const runOk = await showConfirm({ title: "Run Action", message: "Run this action on the VM?", confirmLabel: "Run" });
    if (!runOk) return;
    setExecuting(true);
    setError("");
    try {
      const res = await execApi.execute(vmId, { action_id: actionId });
      setActiveExecution(res.data);
      setOutputLines([]);
      setExecStatus("pending");
      setExecExitCode(null);
      connectWS(res.data.id);
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : "Execution failed";
      const axiosErr = e as { response?: { data?: { error?: string } } };
      setError(axiosErr?.response?.data?.error || msg);
    } finally {
      setExecuting(false);
    }
  };

  const doExecuteWithParams = async () => {
    if (!paramAction) return;
    // Validate required params
    const missing = (paramAction.parameters || []).filter(
      (p) => p.required && !paramValues[p.name]?.trim()
    );
    if (missing.length > 0) {
      toast("Missing required parameters: " + missing.map((p) => p.label).join(", "), "error");
      return;
    }
    const actionId = paramAction.id;
    setParamAction(null);
    setExecuting(true);
    setError("");
    try {
      const res = await execApi.execute(vmId, { action_id: actionId, parameter_values: paramValues });
      setActiveExecution(res.data);
      setOutputLines([]);
      setExecStatus("pending");
      setExecExitCode(null);
      connectWS(res.data.id);
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : "Execution failed";
      const axiosErr = e as { response?: { data?: { error?: string } } };
      setError(axiosErr?.response?.data?.error || msg);
    } finally {
      setExecuting(false);
    }
  };

  const doExecuteAdHoc = async () => {
    if (!adHocScript.trim()) return;
    const adHocOk = await showConfirm({ title: "Run Ad-Hoc Script", message: "Run this script on the VM? Scripts run with sudo privileges.", confirmLabel: "Run Script", variant: "destructive" });
    if (!adHocOk) return;
    setExecuting(true);
    setError("");
    try {
      const res = await execApi.execute(vmId, { script: adHocScript });
      setActiveExecution(res.data);
      setOutputLines([]);
      setExecStatus("pending");
      setExecExitCode(null);
      connectWS(res.data.id);
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : "Execution failed";
      const axiosErr = e as { response?: { data?: { error?: string } } };
      setError(axiosErr?.response?.data?.error || msg);
    } finally {
      setExecuting(false);
    }
  };

  const doCancel = async () => {
    if (!activeExecution) return;
    try {
      await execApi.cancel(activeExecution.id);
    } catch {
      // silent
    }
  };

  const closeModal = () => {
    if (wsRef.current) {
      wsRef.current.close();
      wsRef.current = null;
    }
    setActiveExecution(null);
    setOutputLines([]);
    setExecStatus("");
    setExecExitCode(null);
    loadData();
  };

  const formatDuration = (start: string | null, end: string | null) => {
    if (!start) return "-";
    const s = new Date(start).getTime();
    const e = end ? new Date(end).getTime() : Date.now();
    const secs = Math.round((e - s) / 1000);
    if (secs < 60) return `${secs}s`;
    return `${Math.floor(secs / 60)}m ${secs % 60}s`;
  };

  const categoryColors: Record<string, string> = {
    packages: "bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300",
    scripts: "bg-purple-100 text-purple-700 dark:bg-purple-900 dark:text-purple-300",
    security: "bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300",
    monitoring: "bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300",
    custom: "bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300",
  };

  const isRunning = execStatus === "running" || execStatus === "pending";

  return (
    <div className="space-y-6">
      {/* Execution Modal */}
      {activeExecution && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={(e) => { if (e.target === e.currentTarget && !isRunning) closeModal(); }}>
          <div className="bg-background rounded-lg shadow-xl w-full max-w-5xl mx-4 flex flex-col max-h-[85vh]">
            <div className="flex items-center justify-between p-4 border-b">
              <div className="flex items-center gap-3">
                <Terminal className="h-5 w-5" />
                <span className="font-medium">{activeExecution.action_name}</span>
                {isRunning && <Loader2 className="h-4 w-4 animate-spin text-warning" />}
                {execStatus === "completed" && <CheckCircle className="h-4 w-4 text-success" />}
                {execStatus === "failed" && <XCircle className="h-4 w-4 text-destructive" />}
                {execStatus === "cancelled" && <X className="h-4 w-4 text-gray-500" />}
              </div>
              <div className="flex items-center gap-2">
                {isRunning && (
                  <Button size="sm" variant="destructive" onClick={doCancel}>
                    <X className="h-3 w-3 mr-1" /> Cancel
                  </Button>
                )}
                {execExitCode !== null && (
                  <span className={`inline-flex items-center gap-1.5 text-sm ${execExitCode === 0 ? "text-success" : "text-destructive"}`}>
                    {execExitCode === 0 ? <CheckCircle className="h-4 w-4" /> : <XCircle className="h-4 w-4" />}
                    {execExitCode === 0 ? "Completed successfully" : `Failed (exit code ${execExitCode})`}
                  </span>
                )}
                {outputLines.length > 0 && (
                  <Button size="sm" variant="outline" onClick={() => copyText(outputLines.join("\n")).then(() => toast("Output copied to clipboard"), () => toast("Failed to copy", "error"))}>
                    <Copy className="h-3 w-3 mr-1" /> Copy Output
                  </Button>
                )}
                {!isRunning && (
                  <Button size="sm" variant="outline" onClick={closeModal}>
                    Close
                  </Button>
                )}
                <Button size="icon" variant="ghost" onClick={closeModal}>
                  <X className="h-4 w-4" />
                </Button>
              </div>
            </div>
            <div
              ref={outputRef}
              className="flex-1 overflow-auto p-4 bg-gray-950 font-mono text-sm text-success min-h-[400px]"
            >
              {outputLines.length === 0 && isRunning && (
                <p className="text-gray-500">Waiting for output...</p>
              )}
              {outputLines.map((line, i) => (
                <div key={i} className="whitespace-pre-wrap break-all">{line || "\u00A0"}</div>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* Parameter Modal */}
      {paramAction && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={(e) => { if (e.target === e.currentTarget) setParamAction(null); }}>
          <div className="bg-background rounded-lg shadow-xl w-full max-w-lg mx-4 flex flex-col max-h-[85vh]">
            <div className="flex items-center justify-between p-4 border-b">
              <div className="flex items-center gap-2">
                <Settings2 className="h-5 w-5" />
                <span className="font-medium">{paramAction.name}</span>
              </div>
              <Button size="icon" variant="ghost" onClick={() => setParamAction(null)}>
                <X className="h-4 w-4" />
              </Button>
            </div>
            <div className="flex-1 overflow-auto p-4 space-y-4">
              {paramAction.description && (
                <p className="text-sm text-muted-foreground">{paramAction.description}</p>
              )}
              {(paramAction.parameters || []).map((param) => (
                <div key={param.name} className="space-y-1.5">
                  <Label className="text-sm font-medium">
                    {param.label}
                    {param.required && <span className="text-destructive ml-0.5">*</span>}
                  </Label>
                  {param.description && (
                    <p className="text-xs text-muted-foreground">{param.description}</p>
                  )}
                  {param.type === "select" && param.options ? (
                    <Select
                      value={paramValues[param.name] || ""}
                      onChange={(e) => setParamValues((prev) => ({ ...prev, [param.name]: e.target.value }))}
                    >
                      <option value="">-- Select --</option>
                      {param.options.map((opt) => (
                        <option key={opt} value={opt}>{opt}</option>
                      ))}
                    </Select>
                  ) : param.type === "boolean" ? (
                    <div className="flex items-center gap-2">
                      <input
                        type="checkbox"
                        checked={paramValues[param.name] === "true"}
                        onChange={(e) => setParamValues((prev) => ({ ...prev, [param.name]: e.target.checked ? "true" : "false" }))}
                        className="h-4 w-4 rounded border-gray-300"
                      />
                      <span className="text-sm text-muted-foreground">{paramValues[param.name] === "true" ? "Yes" : "No"}</span>
                    </div>
                  ) : (
                    <Input
                      type={param.type === "password" ? "password" : param.type === "number" ? "number" : "text"}
                      placeholder={param.placeholder || ""}
                      value={paramValues[param.name] || ""}
                      onChange={(e) => setParamValues((prev) => ({ ...prev, [param.name]: e.target.value }))}
                    />
                  )}
                </div>
              ))}
            </div>
            <div className="flex items-center justify-end gap-2 p-4 border-t">
              <Button variant="outline" onClick={() => setParamAction(null)}>Cancel</Button>
              <Button onClick={doExecuteWithParams} disabled={executing}>
                <Play className="h-3 w-3 mr-1" /> Run Action
              </Button>
            </div>
          </div>
        </div>
      )}

      {error && (
        <div className="bg-destructive/10 border border-destructive/30 rounded-md p-3 text-sm text-destructive flex items-center gap-2">
          <AlertTriangle className="h-4 w-4" />
          {error}
          <Button size="sm" variant="ghost" className="ml-auto h-6" onClick={() => setError("")}>
            <X className="h-3 w-3" />
          </Button>
        </div>
      )}

      {!isPoweredOn && (
        <div className="bg-warning/6 border border-warning/30 rounded-md p-3 text-13 text-warning flex items-center gap-2">
          <AlertTriangle className="h-4 w-4" />
          VM must be powered on to execute actions.
        </div>
      )}

      {/* Quick Actions */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle>Quick Actions</CardTitle>
            <Badge variant="secondary">{availableActions.length}</Badge>
          </div>
          {availableActions.length > 0 && (
            <div className="flex flex-col sm:flex-row gap-2 mt-2">
              <Input
                placeholder="Search actions..."
                value={actionSearch}
                onChange={(e) => setActionSearch(e.target.value)}
                className="sm:max-w-xs h-8 text-sm"
              />
              <div className="flex gap-1 flex-wrap">
                {["all", ...Array.from(new Set(availableActions.map((a) => a.category)))].map((cat) => (
                  <Button
                    key={cat}
                    size="sm"
                    variant={actionCategoryFilter === cat ? "default" : "outline"}
                    className="h-7 text-xs"
                    onClick={() => setActionCategoryFilter(cat)}
                  >
                    {cat === "all" ? "All" : cat}
                  </Button>
                ))}
              </div>
            </div>
          )}
        </CardHeader>
        <CardContent>
          {availableActions.length === 0 ? (
            <p className="text-sm text-muted-foreground">No actions available</p>
          ) : (() => {
            const filtered = availableActions.filter((a) => {
              const matchSearch = !actionSearch || a.name.toLowerCase().includes(actionSearch.toLowerCase()) || (a.description || "").toLowerCase().includes(actionSearch.toLowerCase());
              const matchCategory = actionCategoryFilter === "all" || a.category === actionCategoryFilter;
              return matchSearch && matchCategory;
            });
            const pageStart = (actionPage - 1) * actionsPerPage;
            const pagedActions = filtered.slice(pageStart, pageStart + actionsPerPage);
            return filtered.length === 0 ? (
              <p className="text-sm text-muted-foreground">No actions match your search</p>
            ) : (
              <>
              <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {pagedActions.map((action) => (
                  <button
                    key={action.id}
                    className="flex flex-col items-start gap-1 border rounded-lg p-3 hover:bg-muted/50 transition-colors text-left disabled:opacity-50 disabled:cursor-not-allowed"
                    disabled={!isPoweredOn || executing}
                    onClick={() => doExecuteAction(action.id)}
                  >
                    <div className="flex items-center gap-2 w-full">
                      <span className="font-medium text-sm">{action.name}</span>
                      <span className={`text-xs px-1.5 py-0.5 rounded ${categoryColors[action.category] || categoryColors.custom}`}>
                        {action.category}
                      </span>
                      {action.parameters && action.parameters.length > 0 && (
                        <Badge variant="secondary" className="text-xs">
                          <Settings2 className="h-3 w-3 mr-0.5" />{action.parameters.length} param{action.parameters.length !== 1 ? "s" : ""}
                        </Badge>
                      )}
                      {action.builtin && (
                        <Badge variant="outline" className="text-xs ml-auto">builtin</Badge>
                      )}
                    </div>
                    <p className="text-xs text-muted-foreground line-clamp-2">{action.description}</p>
                  </button>
                ))}
              </div>
              <Pagination
                page={actionPage}
                pageSize={actionsPerPage}
                totalItems={filtered.length}
                onPageChange={setActionPage}
                onPageSizeChange={setActionsPerPage}
                pageSizeOptions={[9, 12, 24]}
                itemLabel="actions"
              />
              </>
            );
          })()}
        </CardContent>
      </Card>

      {/* Ad-Hoc Script */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Terminal className="h-4 w-4" /> Ad-Hoc Script
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <p className="text-xs text-warning flex items-center gap-1">
            <AlertTriangle className="h-3 w-3" />
            Scripts run with sudo privileges on the target VM.
          </p>
          <textarea
            className="w-full h-32 bg-gray-950 text-success font-mono text-sm p-3 rounded-md border resize-y"
            placeholder="#!/bin/bash&#10;echo 'Hello from Forgemill'"
            value={adHocScript}
            onChange={(e) => setAdHocScript(e.target.value)}
            disabled={!isPoweredOn}
          />
          <Button
            size="sm"
            onClick={doExecuteAdHoc}
            disabled={!isPoweredOn || executing || !adHocScript.trim()}
          >
            <Play className="h-3 w-3 mr-1" /> Run Script
          </Button>
        </CardContent>
      </Card>

      {/* Execution History */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle>Execution History</CardTitle>
            <Button size="sm" variant="ghost" onClick={loadData}>
              <RefreshCw className="h-3 w-3 mr-1" /> Refresh
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {executionHistory.length === 0 ? (
            <p className="text-sm text-muted-foreground">No executions yet</p>
          ) : (() => {
            const paged = executionHistory.slice((historyPage - 1) * historyPerPage, historyPage * historyPerPage);
            return (
              <div className="space-y-2">
                {paged.map((exec) => (
                  <div key={exec.id} className="border rounded-md">
                    <button
                      className="w-full flex items-center justify-between p-3 text-left hover:bg-muted/30 transition-colors"
                      onClick={() => setExpandedExecId(expandedExecId === exec.id ? null : exec.id)}
                    >
                      <div className="flex items-center gap-3">
                        <span className="text-sm font-medium">{exec.action_name}</span>
                        <Badge variant={executionStatusVariant(exec.status)}>{exec.status}</Badge>
                        {exec.exit_code !== null && exec.exit_code !== 0 && (
                          <span className="text-xs text-destructive flex items-center gap-1">
                            <XCircle className="h-3.5 w-3.5" /> Exit code {exec.exit_code}
                          </span>
                        )}
                      </div>
                      <div className="flex items-center gap-3 text-xs text-muted-foreground">
                        <span>{formatDuration(exec.started_at, exec.completed_at)}</span>
                        <span>{formatDateTime(exec.created_at)}</span>
                      </div>
                    </button>
                    {expandedExecId === exec.id && exec.parameter_values && Object.keys(exec.parameter_values).length > 0 && (
                      <div className="border-t bg-muted/30 px-3 py-2">
                        <details className="text-xs">
                          <summary className="cursor-pointer text-muted-foreground font-medium flex items-center gap-1">
                            <Settings2 className="h-3 w-3" /> Parameters
                          </summary>
                          <div className="mt-1 grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 pl-4">
                            {Object.entries(exec.parameter_values).map(([k, v]) => (
                              <><span key={`${k}-label`} className="text-muted-foreground">{k}:</span><span key={`${k}-value`} className="font-mono">{v}</span></>
                            ))}
                          </div>
                        </details>
                      </div>
                    )}
                    {expandedExecId === exec.id && exec.output && (
                      <div className="border-t bg-gray-950 p-3 font-mono text-xs text-success max-h-60 overflow-auto whitespace-pre-wrap">
                        {exec.output}
                      </div>
                    )}
                  </div>
                ))}
                <Pagination
                  page={historyPage}
                  pageSize={historyPerPage}
                  totalItems={executionHistory.length}
                  onPageChange={setHistoryPage}
                  onPageSizeChange={(n) => { setHistoryPerPage(n); setHistoryPage(1); }}
                  itemLabel="executions"
                />
              </div>
            );
          })()}
        </CardContent>
      </Card>
    </div>
  );
}
