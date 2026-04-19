import React, { useEffect, useState } from "react";
import { actions as actionsApi } from "@/api/client";
import type { Action, ActionParameter } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Plus, Trash2, Edit2, Package, Terminal, Shield, Activity, Puzzle, Search, X, Code2, ChevronDown, ChevronUp, Info, Copy, Check, Loader2, ArrowUp, ArrowDown, Settings2 } from "lucide-react";
import { Select } from "@/components/ui/select";
import { Pagination } from "@/components/ui/pagination";
import { useAuth } from "@/hooks/useAuth";
import { useToast } from "@/components/ui/toast";
import { useConfirm } from "@/components/ui/confirm-dialog";
import { getErrorMessage } from "@/lib/utils";
import { ViewToggle } from "@/components/ui/view-toggle";
import { usePreference } from "@/context/PreferencesContext";
import { SortableTh } from "@/components/ui/sortable-th";
import { useTableSort } from "@/hooks/useTableSort";
import { PageHeader } from "@/components/ui/page-header";
import { usePageSize } from "@/hooks/usePageSize";

const categoryIcons: Record<string, typeof Package> = {
  packages: Package,
  scripts: Terminal,
  security: Shield,
  monitoring: Activity,
  custom: Puzzle,
};

const categoryLabels: Record<string, string> = {
  packages: "Packages",
  scripts: "Scripts",
  security: "Security",
  monitoring: "Monitoring",
  custom: "Custom",
};

const categoryColors: Record<string, string> = {
  packages: "bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300",
  scripts: "bg-purple-100 text-purple-700 dark:bg-purple-900 dark:text-purple-300",
  security: "bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300",
  monitoring: "bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300",
  custom: "bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300",
};

export default function ActionsPage() {
  const { toast } = useToast();
  const { confirm: showConfirm } = useConfirm();
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";
  const [actionList, setActionList] = useState<Action[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [editingId, setEditingId] = useState<number | null>(null);
  const [form, setForm] = useState<{ name: string; description: string; category: Action["category"]; script: string; parameters: ActionParameter[] }>({ name: "", description: "", category: "custom", script: "", parameters: [] });
  const [configError, setConfigError] = useState("");
  const [search, setSearch] = useState("");
  const [categoryFilter, setCategoryFilter] = useState<string>("all");
  const [expandedId, setExpandedId] = useState<number | null>(null);
  const [copiedId, setCopiedId] = useState<number | null>(null);

  const fetchActions = async () => {
    try {
      const res = await actionsApi.list();
      setActionList(res.data || []);
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchActions(); }, []);

  const validateScript = (val: string): boolean => {
    if (!val.trim()) {
      setConfigError("Script is empty");
      return false;
    }
    if (val.length > 65536) {
      setConfigError("Script exceeds maximum size (64KB)");
      return false;
    }
    setConfigError("");
    return true;
  };

  const handleSave = async () => {
    if (!form.name || !form.script) return;
    if (!validateScript(form.script)) return;
    const payload = {
      ...form,
      parameters: form.parameters.length > 0 ? form.parameters : undefined,
    };
    try {
      if (editingId) {
        await actionsApi.update(editingId, payload);
      } else {
        await actionsApi.create(payload);
      }
      setShowForm(false);
      setEditingId(null);
      setForm({ name: "", description: "", category: "custom" as Action["category"], script: "", parameters: [] });
      fetchActions();
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { error?: string } } })?.response?.data?.error || "Failed to save action";
      toast(msg, "error");
    }
  };

  const handleEdit = (action: Action) => {
    setForm({
      name: action.name,
      description: action.description,
      category: action.category,
      script: action.script || "",
      parameters: action.parameters || [],
    });
    setEditingId(action.id);
    setShowForm(true);
    setConfigError("");
  };

  const handleDelete = async (id: number) => {
    const ok = await showConfirm({ title: "Delete Action", message: "Delete this action? This cannot be undone.", confirmLabel: "Delete", variant: "destructive" });
    if (!ok) return;
    try {
      await actionsApi.delete(id);
      fetchActions();
    } catch (e) {
      toast(getErrorMessage(e, "Failed to delete action"), "error");
    }
  };

  const filtered = actionList.filter((a) => {
    const matchSearch = !search || a.name.toLowerCase().includes(search.toLowerCase()) || (a.description || "").toLowerCase().includes(search.toLowerCase());
    const matchCategory = categoryFilter === "all" || a.category === categoryFilter;
    return matchSearch && matchCategory;
  });
  const viewMode = usePreference("view_mode", "cards");
  const { sorted: actionsSorted, sortField: actSortField, sortDir: actSortDir, toggleSort: actToggleSort } = useTableSort(filtered, "name");

  const categories = Array.from(new Set(actionList.map((a) => a.category)));

  // Group filtered actions
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = usePageSize("actions", 25);
  const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize));
  const paginated = filtered.slice((page - 1) * pageSize, page * pageSize);

  // Reset page on filter change
  useEffect(() => { setPage(1); }, [search, categoryFilter, pageSize]);

  const grouped = paginated.reduce<Record<string, Action[]>>((acc, a) => {
    (acc[a.category] = acc[a.category] || []).push(a);
    return acc;
  }, {});

  if (loading) {
    return <div className="flex items-center justify-center h-64"><Loader2 className="h-8 w-8 animate-spin text-primary" /></div>;
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={
          <span className="flex items-center gap-2">
            Actions
            {actionList.length > 0 && <Badge variant="outline">{actionList.length}</Badge>}
          </span>
        }
        description="Reusable post-deploy automation — install packages, configure services, run scripts."
        actions={
          <>
            {actionList.length > 0 && (
              <div className="relative w-full sm:w-64">
                <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                <Input
                  placeholder="Search actions..."
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  className="pl-9"
                />
                {search && (
                  <button className="absolute right-3 top-1/2 -translate-y-1/2" onClick={() => setSearch("")}>
                    <X className="h-4 w-4 text-muted-foreground hover:text-foreground" />
                  </button>
                )}
              </div>
            )}
            <ViewToggle />
            {isAdmin && (
              <Button onClick={() => { setShowForm(!showForm); setEditingId(null); setForm({ name: "", description: "", category: "custom" as Action["category"], script: "", parameters: [] }); setConfigError(""); }}>
                <Plus className="h-4 w-4 mr-2" /> Create Action
              </Button>
            )}
          </>
        }
      />

      {/* Category Filter */}
      {actionList.length > 0 && (
        <div className="flex flex-wrap gap-3">
          <div className="flex gap-1 flex-wrap">
            <Button
              size="sm"
              variant={categoryFilter === "all" ? "default" : "outline"}
              onClick={() => setCategoryFilter("all")}
            >
              All
            </Button>
            {categories.map((cat) => {
              const Icon = categoryIcons[cat] || Puzzle;
              return (
                <Button
                  key={cat}
                  size="sm"
                  variant={categoryFilter === cat ? "default" : "outline"}
                  onClick={() => setCategoryFilter(cat)}
                  className="gap-1"
                >
                  <Icon className="h-3 w-3" />
                  {categoryLabels[cat] || cat}
                  <Badge variant="secondary" className="ml-1 h-5 text-xs">{actionList.filter((a) => a.category === cat).length}</Badge>
                </Button>
              );
            })}
          </div>
        </div>
      )}

      {/* Create/Edit Form */}
      {showForm && (
        <Card className="border-primary/30">
          <CardHeader>
            <CardTitle>{editingId ? "Edit Action" : "Create Action"}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2 sm:col-span-2">
                <Label>Name *</Label>
                <Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="Install Nginx" />
              </div>
              <div className="space-y-2 sm:col-span-2">
                <Label>Description</Label>
                <Input value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} placeholder="What this action does..." />
              </div>
              <div className="space-y-2">
                <Label>Category</Label>
                <Select
                  value={form.category}
                  onChange={(e) => setForm({ ...form, category: e.target.value as Action["category"] })}
                >
                  <option value="packages">Packages</option>
                  <option value="scripts">Scripts</option>
                  <option value="security">Security</option>
                  <option value="monitoring">Monitoring</option>
                  <option value="custom">Custom</option>
                </Select>
              </div>
              <div className="space-y-2 sm:col-span-2">
                <Label>Script *</Label>
                <textarea
                  value={form.script}
                  onChange={(e) => { setForm({ ...form, script: e.target.value }); if (e.target.value) validateScript(e.target.value); }}
                  placeholder={"#!/bin/bash\nset -euo pipefail\n\napt-get update -y\napt-get install -y nginx\nsystemctl enable --now nginx"}
                  rows={10}
                  className="w-full rounded-md border border-input bg-gray-950 text-green-400 px-3 py-2 text-sm shadow-sm placeholder:text-gray-600 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring font-mono resize-y"
                />
                {configError && <p className="text-xs text-destructive">{configError}</p>}
                <p className="text-xs text-muted-foreground">Bash script that runs with sudo privileges on the target VM. Max 64KB.</p>
              </div>

              {/* Parameters Section */}
              <div className="space-y-3 sm:col-span-2">
                <div className="flex items-center justify-between">
                  <Label className="flex items-center gap-1.5"><Settings2 className="h-3.5 w-3.5" /> Parameters</Label>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => setForm({ ...form, parameters: [...form.parameters, { name: "", label: "", type: "string", required: false, default: "", placeholder: "", options: null, description: "" }] })}
                  >
                    <Plus className="h-3 w-3 mr-1" /> Add Parameter
                  </Button>
                </div>
                {form.parameters.length > 0 && (
                  <p className="text-xs text-muted-foreground">Parameters are exposed as PARAM_NAME environment variables in the script.</p>
                )}
                {form.parameters.map((param, idx) => (
                  <div key={idx} className="border rounded-md p-3 space-y-2 bg-muted/30">
                    <div className="flex items-center justify-between">
                      <span className="text-xs font-medium text-muted-foreground">Parameter {idx + 1}</span>
                      <div className="flex items-center gap-1">
                        {idx > 0 && (
                          <Button type="button" variant="ghost" size="sm" className="h-6 w-6 p-0" onClick={() => {
                            const p = [...form.parameters];
                            [p[idx - 1], p[idx]] = [p[idx], p[idx - 1]];
                            setForm({ ...form, parameters: p });
                          }}><ArrowUp className="h-3 w-3" /></Button>
                        )}
                        {idx < form.parameters.length - 1 && (
                          <Button type="button" variant="ghost" size="sm" className="h-6 w-6 p-0" onClick={() => {
                            const p = [...form.parameters];
                            [p[idx], p[idx + 1]] = [p[idx + 1], p[idx]];
                            setForm({ ...form, parameters: p });
                          }}><ArrowDown className="h-3 w-3" /></Button>
                        )}
                        <Button type="button" variant="ghost" size="sm" className="h-6 w-6 p-0 text-destructive" onClick={() => {
                          setForm({ ...form, parameters: form.parameters.filter((_, i) => i !== idx) });
                        }}><Trash2 className="h-3 w-3" /></Button>
                      </div>
                    </div>
                    <div className="grid gap-2 sm:grid-cols-3">
                      <div>
                        <Label className="text-xs">Name *</Label>
                        <Input
                          value={param.name}
                          onChange={(e) => {
                            const p = [...form.parameters];
                            p[idx] = { ...p[idx], name: e.target.value.toUpperCase().replace(/[^A-Z0-9_]/g, "") };
                            setForm({ ...form, parameters: p });
                          }}
                          placeholder="MY_PARAM"
                          className="h-8 text-xs font-mono"
                        />
                      </div>
                      <div>
                        <Label className="text-xs">Label *</Label>
                        <Input
                          value={param.label}
                          onChange={(e) => {
                            const p = [...form.parameters];
                            p[idx] = { ...p[idx], label: e.target.value };
                            setForm({ ...form, parameters: p });
                          }}
                          placeholder="My Parameter"
                          className="h-8 text-xs"
                        />
                      </div>
                      <div>
                        <Label className="text-xs">Type</Label>
                        <Select
                          value={param.type}
                          onChange={(e) => {
                            const p = [...form.parameters];
                            p[idx] = { ...p[idx], type: e.target.value as ActionParameter["type"] };
                            setForm({ ...form, parameters: p });
                          }}
                          className="h-8 text-xs"
                        >
                          <option value="string">String</option>
                          <option value="number">Number</option>
                          <option value="select">Select</option>
                          <option value="boolean">Boolean</option>
                          <option value="password">Password</option>
                        </Select>
                      </div>
                    </div>
                    <div className="grid gap-2 sm:grid-cols-3">
                      <div>
                        <Label className="text-xs">Default</Label>
                        <Input
                          value={param.default}
                          onChange={(e) => {
                            const p = [...form.parameters];
                            p[idx] = { ...p[idx], default: e.target.value };
                            setForm({ ...form, parameters: p });
                          }}
                          placeholder="default value"
                          className="h-8 text-xs"
                        />
                      </div>
                      <div>
                        <Label className="text-xs">Placeholder</Label>
                        <Input
                          value={param.placeholder}
                          onChange={(e) => {
                            const p = [...form.parameters];
                            p[idx] = { ...p[idx], placeholder: e.target.value };
                            setForm({ ...form, parameters: p });
                          }}
                          placeholder="placeholder text"
                          className="h-8 text-xs"
                        />
                      </div>
                      <div className="flex items-end gap-2">
                        <label className="flex items-center gap-1.5 text-xs cursor-pointer">
                          <input
                            type="checkbox"
                            checked={param.required}
                            onChange={(e) => {
                              const p = [...form.parameters];
                              p[idx] = { ...p[idx], required: e.target.checked };
                              setForm({ ...form, parameters: p });
                            }}
                            className="rounded"
                          />
                          Required
                        </label>
                      </div>
                    </div>
                    {param.type === "select" && (
                      <div>
                        <Label className="text-xs">Options (comma-separated)</Label>
                        <Input
                          value={(param.options || []).join(", ")}
                          onChange={(e) => {
                            const p = [...form.parameters];
                            p[idx] = { ...p[idx], options: e.target.value.split(",").map((s) => s.trim()).filter(Boolean) };
                            setForm({ ...form, parameters: p });
                          }}
                          placeholder="option1, option2, option3"
                          className="h-8 text-xs"
                        />
                      </div>
                    )}
                    <div>
                      <Label className="text-xs">Description</Label>
                      <Input
                        value={param.description}
                        onChange={(e) => {
                          const p = [...form.parameters];
                          p[idx] = { ...p[idx], description: e.target.value };
                          setForm({ ...form, parameters: p });
                        }}
                        placeholder="Help text for this parameter"
                        className="h-8 text-xs"
                      />
                    </div>
                  </div>
                ))}
              </div>

              <div className="sm:col-span-2 flex gap-2">
                <Button onClick={handleSave} disabled={!form.name || !form.script || !!configError}>
                  {editingId ? "Update" : "Create"}
                </Button>
                <Button variant="outline" onClick={() => { setShowForm(false); setEditingId(null); }}>Cancel</Button>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Actions List */}
      {actionList.length === 0 ? (
        <Card className="border-dashed">
          <CardContent className="flex flex-col items-center justify-center py-16 text-center">
            <Code2 className="h-12 w-12 text-muted-foreground/40 mb-4" />
            <h3 className="text-lg font-medium mb-1">No actions yet</h3>
            <p className="text-sm text-muted-foreground mb-4 max-w-md">
              Create reusable automation snippets to run on your VMs. Install packages, configure services, or run custom scripts.
            </p>
            {isAdmin && (
              <Button onClick={() => { setShowForm(true); setEditingId(null); setForm({ name: "", description: "", category: "custom" as Action["category"], script: "", parameters: [] }); }}>
                <Plus className="h-4 w-4 mr-2" /> Create Your First Action
              </Button>
            )}
          </CardContent>
        </Card>
      ) : filtered.length === 0 ? (
        <div className="text-center py-12">
          <p className="text-muted-foreground">No actions match your search</p>
          <Button variant="link" onClick={() => { setSearch(""); setCategoryFilter("all"); }}>Clear filters</Button>
        </div>
      ) : viewMode === "table" ? (
        <div className="rounded-md border">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b bg-muted/50">
                <SortableTh label="Name" field="name" currentField={actSortField} currentDir={actSortDir} onSort={actToggleSort} />
                <SortableTh label="Category" field="category" currentField={actSortField} currentDir={actSortDir} onSort={actToggleSort} className="hidden sm:table-cell" />
                <th className="text-left px-4 py-2 font-medium hidden md:table-cell">Description</th>
                <th className="text-left px-4 py-2 font-medium hidden lg:table-cell">Type</th>
                <th className="text-right px-4 py-2 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {actionsSorted.map((action) => (
                <React.Fragment key={action.id}>
                <tr className="border-b last:border-0 hover:bg-muted/30 transition-colors">
                  <td className="px-4 py-2.5 font-medium">{action.name}</td>
                  <td className="px-4 py-2.5 hidden sm:table-cell">
                    <span className={`text-xs px-1.5 py-0.5 rounded ${categoryColors[action.category] || categoryColors.custom}`}>
                      {action.category}
                    </span>
                  </td>
                  <td className="px-4 py-2.5 text-muted-foreground text-xs hidden md:table-cell max-w-xs truncate">{action.description || "—"}</td>
                  <td className="px-4 py-2.5 hidden lg:table-cell">
                    {action.builtin ? <Badge variant="outline" className="text-xs">Built-in</Badge> : <Badge variant="secondary" className="text-xs">Custom</Badge>}
                  </td>
                  <td className="px-4 py-2.5 text-right">
                    <div className="flex items-center justify-end gap-1">
                      <Button variant="ghost" size="sm" onClick={() => setExpandedId(expandedId === action.id ? null : action.id)} title="View script">
                        <Code2 className="h-3.5 w-3.5" />
                      </Button>
                      {isAdmin && !action.builtin && (
                        <>
                          <Button variant="ghost" size="sm" onClick={() => handleEdit(action)} title="Edit">
                            <Edit2 className="h-3.5 w-3.5" />
                          </Button>
                          <Button variant="ghost" size="sm" onClick={() => handleDelete(action.id)} title="Delete" className="text-destructive">
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        </>
                      )}
                    </div>
                  </td>
                </tr>
                {expandedId === action.id && (
                  <tr className="border-b last:border-0">
                    <td colSpan={5} className="px-4 py-3">
                      <pre className="text-xs bg-gray-950 text-green-400 p-3 rounded-md overflow-x-auto max-h-64 whitespace-pre-wrap">{action.script}</pre>
                    </td>
                  </tr>
                )}
                </React.Fragment>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        Object.entries(grouped).map(([category, categoryActions]) => {
          const Icon = categoryIcons[category] || Puzzle;
          return (
            <div key={category} className="space-y-3">
              <div className="flex items-center gap-2">
                <Icon className="h-4 w-4 text-muted-foreground" />
                <h2 className="text-lg font-semibold">{categoryLabels[category] || category}</h2>
                <Badge variant="secondary">{categoryActions.length}</Badge>
              </div>
              <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {categoryActions.map((action) => (
                  <Card key={action.id} className="group hover:border-primary/50 transition-colors">
                    <CardHeader className="pb-2">
                      <div className="flex items-start justify-between">
                        <div className="flex items-center gap-2">
                          <CardTitle className="text-base">{action.name}</CardTitle>
                        </div>
                        <div className="flex items-center gap-1">
                          {action.builtin && <Badge variant="outline" className="text-xs">Built-in</Badge>}
                          {action.parameters && action.parameters.length > 0 && (
                            <Badge variant="outline" className="text-xs"><Settings2 className="h-2.5 w-2.5 mr-0.5" />{action.parameters.length}</Badge>
                          )}
                          <span className={`text-xs px-1.5 py-0.5 rounded ${categoryColors[action.category] || categoryColors.custom}`}>
                            {action.category}
                          </span>
                        </div>
                      </div>
                    </CardHeader>
                    <CardContent>
                      <p className="text-sm text-muted-foreground mb-3">{action.description || "No description"}</p>
                      
                      {/* Collapsible config preview */}
                      <button
                        className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground mb-1"
                        onClick={() => setExpandedId(expandedId === action.id ? null : action.id)}
                      >
                        <Code2 className="h-3 w-3" />
                        <span>Script</span>
                        {expandedId === action.id ? <ChevronUp className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" />}
                      </button>
                      {expandedId === action.id && (
                        <div className="relative mb-3">
                          <pre className="text-xs bg-gray-950 text-green-400 p-3 pr-10 rounded-md overflow-x-auto max-h-64 whitespace-pre-wrap">{action.script}</pre>
                          <button
                            className="absolute top-2 right-2 p-1.5 rounded-md bg-gray-800 hover:bg-gray-700 text-gray-400 hover:text-gray-200 transition-colors"
                            onClick={() => {
                              navigator.clipboard.writeText(action.script);
                              setCopiedId(action.id);
                              setTimeout(() => setCopiedId(null), 2000);
                            }}
                            title="Copy script"
                          >
                            {copiedId === action.id ? <Check className="h-3.5 w-3.5 text-green-400" /> : <Copy className="h-3.5 w-3.5" />}
                          </button>
                        </div>
                      )}

                      {isAdmin && !action.builtin && (
                        <div className="flex gap-2 mt-2 pt-2 border-t opacity-0 group-hover:opacity-100 transition-opacity">
                          <Button variant="outline" size="sm" onClick={() => handleEdit(action)}>
                            <Edit2 className="h-3 w-3 mr-1" /> Edit
                          </Button>
                          <Button variant="outline" size="sm" onClick={() => handleDelete(action.id)} className="text-destructive hover:text-destructive">
                            <Trash2 className="h-3 w-3 mr-1" /> Delete
                          </Button>
                        </div>
                      )}
                    </CardContent>
                  </Card>
                ))}
              </div>
            </div>
          );
        })
      )}

      {/* Pagination */}
      <Pagination
        page={page}
        pageSize={pageSize}
        totalItems={filtered.length}
        onPageChange={setPage}
        onPageSizeChange={setPageSize}
        itemLabel="actions"
      />
    </div>
  );
}
