import { useEffect, useState, useCallback, useRef, useMemo } from "react";
import { createPortal } from "react-dom";
import { useNavigate } from "react-router-dom";
import { Monitor, FileBox, Server, Zap, Search, Loader2, X, Power, Play, Square } from "lucide-react";
import { cn } from "@/lib/utils";
import { vms as vmApi, templates, targets, actions } from "@/api/client";
import type { ManagedVM, Template, Target, Action } from "@/types";

const MAX_RESULTS_PER_CATEGORY = 5;

type ResultCategory = "vms" | "templates" | "targets" | "actions";

interface SearchResult {
  id: number;
  type: ResultCategory;
  name: string;
  subtitle: string;
  badge?: string;
  badgeVariant?: "success" | "secondary" | "warning";
  powerState?: string;
}

interface SearchData {
  vms: ManagedVM[];
  templates: Template[];
  targets: Target[];
  actions: Action[];
}

const categoryConfig: Record<ResultCategory, { label: string; icon: typeof Monitor; route: string }> = {
  vms: { label: "VMs", icon: Monitor, route: "/vms" },
  templates: { label: "Templates", icon: FileBox, route: "/templates" },
  targets: { label: "Targets", icon: Server, route: "/targets" },
  actions: { label: "Actions", icon: Zap, route: "/actions" },
};

const powerStateVariant = (state: string): "success" | "secondary" | "warning" => {
  if (state === "poweredOn" || state === "running") return "success";
  if (state === "poweredOff" || state === "stopped") return "secondary";
  if (state === "suspended") return "warning";
  return "secondary";
};

const powerStateLabel = (state: string): string => {
  if (state === "poweredOn" || state === "running") return "Running";
  if (state === "poweredOff" || state === "stopped") return "Stopped";
  if (state === "suspended") return "Suspended";
  return state;
};

export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<SearchData | null>(null);
  const [poweringVm, setPoweringVm] = useState<number | null>(null);

  const inputRef = useRef<HTMLInputElement>(null);
  const resultsRef = useRef<HTMLDivElement>(null);
  const navigate = useNavigate();

  // Fetch all data when palette opens
  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const [vmsRes, templatesRes, targetsRes, actionsRes] = await Promise.all([
        vmApi.list(),
        templates.list(),
        targets.list(),
        actions.list(),
      ]);
      setData({
        vms: vmsRes.data || [],
        templates: templatesRes.data || [],
        targets: targetsRes.data || [],
        actions: actionsRes.data || [],
      });
    } catch (err) {
      console.error("Failed to fetch command palette data:", err);
      setData({ vms: [], templates: [], targets: [], actions: [] });
    } finally {
      setLoading(false);
    }
  }, []);

  // Open/close handlers
  const handleOpen = useCallback(() => {
    setOpen(true);
    setQuery("");
    setSelectedIndex(0);
    fetchData();
  }, [fetchData]);

  const handleClose = useCallback(() => {
    setOpen(false);
    setQuery("");
    setSelectedIndex(0);
  }, []);

  // Global keyboard shortcut
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        if (open) {
          handleClose();
        } else {
          handleOpen();
        }
      }
      if (e.key === "Escape" && open) {
        e.preventDefault();
        handleClose();
      }
    };

    // Listen for custom event from header search pill
    const handleOpenEvent = () => {
      if (!open) handleOpen();
    };

    document.addEventListener("keydown", handleKeyDown);
    document.addEventListener("openCommandPalette", handleOpenEvent);
    return () => {
      document.removeEventListener("keydown", handleKeyDown);
      document.removeEventListener("openCommandPalette", handleOpenEvent);
    };
  }, [open, handleOpen, handleClose]);

  // Focus input when opened
  useEffect(() => {
    if (open && inputRef.current) {
      inputRef.current.focus();
    }
  }, [open]);

  // Search and filter results
  const results = useMemo((): SearchResult[] => {
    if (!data) return [];

    const q = query.toLowerCase().trim();
    const allResults: SearchResult[] = [];

    // Filter VMs
    const filteredVms = data.vms.filter((vm) =>
      vm.vm_name.toLowerCase().includes(q) ||
      (vm.ip_address && vm.ip_address.toLowerCase().includes(q)) ||
      vm.target_name.toLowerCase().includes(q)
    );
    filteredVms.slice(0, MAX_RESULTS_PER_CATEGORY).forEach((vm) => {
      allResults.push({
        id: vm.id,
        type: "vms",
        name: vm.vm_name,
        subtitle: vm.ip_address || vm.target_name,
        badge: powerStateLabel(vm.power_state),
        badgeVariant: powerStateVariant(vm.power_state),
        powerState: vm.power_state,
      });
    });

    // Filter Templates
    const filteredTemplates = data.templates.filter((t) =>
      t.name.toLowerCase().includes(q) ||
      t.os_name?.toLowerCase().includes(q) ||
      t.target_name.toLowerCase().includes(q)
    );
    filteredTemplates.slice(0, MAX_RESULTS_PER_CATEGORY).forEach((t) => {
      allResults.push({
        id: t.id,
        type: "templates",
        name: t.name,
        subtitle: t.os_name || t.target_name,
      });
    });

    // Filter Targets
    const filteredTargets = data.targets.filter((t) =>
      t.name.toLowerCase().includes(q) ||
      t.hostname.toLowerCase().includes(q) ||
      t.type.toLowerCase().includes(q)
    );
    filteredTargets.slice(0, MAX_RESULTS_PER_CATEGORY).forEach((t) => {
      allResults.push({
        id: t.id,
        type: "targets",
        name: t.name,
        subtitle: `${t.type} - ${t.hostname}`,
        badge: t.status,
        badgeVariant: t.status === "connected" ? "success" : "secondary",
      });
    });

    // Filter Actions
    const filteredActions = data.actions.filter((a) =>
      a.name.toLowerCase().includes(q) ||
      a.description.toLowerCase().includes(q) ||
      a.category.toLowerCase().includes(q)
    );
    filteredActions.slice(0, MAX_RESULTS_PER_CATEGORY).forEach((a) => {
      allResults.push({
        id: a.id,
        type: "actions",
        name: a.name,
        subtitle: a.description || a.category,
        badge: a.platform,
      });
    });

    return allResults;
  }, [data, query]);

  // Group results by category
  const groupedResults = useMemo(() => {
    const groups: Record<ResultCategory, SearchResult[]> = {
      vms: [],
      templates: [],
      targets: [],
      actions: [],
    };
    results.forEach((r) => groups[r.type].push(r));
    return groups;
  }, [results]);

  // Keyboard navigation
  useEffect(() => {
    if (!open) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setSelectedIndex((i) => Math.min(i + 1, results.length - 1));
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setSelectedIndex((i) => Math.max(i - 1, 0));
      } else if (e.key === "Enter" && results[selectedIndex]) {
        e.preventDefault();
        handleSelect(results[selectedIndex]);
      }
    };

    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [open, results, selectedIndex]);

  // Reset selection when query changes
  useEffect(() => {
    setSelectedIndex(0);
  }, [query]);

  // Scroll selected item into view
  useEffect(() => {
    if (resultsRef.current) {
      const selected = resultsRef.current.querySelector('[data-selected="true"]');
      if (selected) {
        selected.scrollIntoView({ block: "nearest" });
      }
    }
  }, [selectedIndex]);

  // Handle result selection
  const handleSelect = useCallback((result: SearchResult) => {
    handleClose();
    if (result.type === "vms") {
      navigate(`/vms/${result.id}`);
    } else {
      navigate(categoryConfig[result.type].route);
    }
  }, [navigate, handleClose]);

  // Handle VM power action
  const handlePower = useCallback(async (e: React.MouseEvent, result: SearchResult, action: "on" | "off") => {
    e.stopPropagation();
    if (poweringVm) return;

    setPoweringVm(result.id);
    try {
      // Backend expects "start"/"stop", not "on"/"off"
      const apiAction = action === "on" ? "start" : "stop";
      await vmApi.power(result.id, apiAction);
      // Refresh VM data
      const vmsRes = await vmApi.list();
      setData((prev) => prev ? { ...prev, vms: vmsRes.data || [] } : null);
    } catch (err) {
      console.error("Power action failed:", err);
    } finally {
      setPoweringVm(null);
    }
  }, [poweringVm]);

  // Get flat index for a result item
  const getFlatIndex = (categoryIndex: number, itemIndex: number): number => {
    let index = 0;
    const categories: ResultCategory[] = ["vms", "templates", "targets", "actions"];
    for (let i = 0; i < categoryIndex; i++) {
      index += groupedResults[categories[i]].length;
    }
    return index + itemIndex;
  };

  if (!open) return null;

  const content = (
    <div
      className="fixed inset-0 z-50 bg-black/60 backdrop-blur-sm"
      onClick={handleClose}
      role="dialog"
      aria-modal="true"
      aria-label="Command palette"
    >
      <div className="flex min-h-full items-start justify-center p-4 pt-[15vh]">
        <div
          className="w-full max-w-xl rounded-xl border bg-card text-card-foreground shadow-2xl"
          onClick={(e) => e.stopPropagation()}
        >
          {/* Search input */}
          <div className="flex items-center border-b px-4">
            <Search className="h-5 w-5 text-muted-foreground" aria-hidden="true" />
            <input
              ref={inputRef}
              type="text"
              className="flex-1 bg-transparent px-4 py-4 text-sm outline-none placeholder:text-muted-foreground"
              placeholder="Search VMs, templates, targets..."
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              aria-label="Search"
              aria-autocomplete="list"
              aria-controls="command-results"
            />
            <button
              onClick={handleClose}
              className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
              aria-label="Close"
            >
              <X className="h-4 w-4" />
            </button>
          </div>

          {/* Results */}
          <div
            ref={resultsRef}
            id="command-results"
            className="max-h-[60vh] overflow-y-auto p-2"
            role="listbox"
            aria-label="Search results"
          >
            {loading ? (
              <div className="flex items-center justify-center py-12">
                <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
                <span className="sr-only">Loading...</span>
              </div>
            ) : results.length === 0 ? (
              <div className="py-12 text-center text-sm text-muted-foreground">
                {query ? "No results found" : "Start typing to search..."}
              </div>
            ) : (
              (["vms", "templates", "targets", "actions"] as ResultCategory[]).map((category, catIdx) => {
                const items = groupedResults[category];
                if (items.length === 0) return null;

                const config = categoryConfig[category];
                const Icon = config.icon;

                return (
                  <div key={category} className="mb-2">
                    <div className="px-2 py-1.5 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                      {config.label}
                    </div>
                    {items.map((item, itemIdx) => {
                      const flatIdx = getFlatIndex(catIdx, itemIdx);
                      const isSelected = flatIdx === selectedIndex;
                      const isVm = item.type === "vms";
                      const isPoweredOn = item.powerState === "poweredOn" || item.powerState === "running";

                      return (
                        <div
                          key={`${item.type}-${item.id}`}
                          data-selected={isSelected}
                          className={cn(
                            "group flex cursor-pointer items-center gap-3 rounded-lg px-3 py-2.5 transition-colors",
                            isSelected ? "bg-primary/20" : "hover:bg-muted"
                          )}
                          onClick={() => handleSelect(item)}
                          onMouseEnter={() => setSelectedIndex(flatIdx)}
                          role="option"
                          aria-selected={isSelected}
                        >
                          <Icon className="h-5 w-5 flex-shrink-0 text-muted-foreground" aria-hidden="true" />
                          <div className="flex-1 min-w-0">
                            <div className="font-medium truncate">{item.name}</div>
                            <div className="text-xs text-muted-foreground truncate">{item.subtitle}</div>
                          </div>

                          {/* Power buttons for VMs */}
                          {isVm && (
                            <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                              {isPoweredOn ? (
                                <button
                                  onClick={(e) => handlePower(e, item, "off")}
                                  disabled={poweringVm === item.id}
                                  className="rounded p-1.5 text-muted-foreground hover:bg-destructive/20 hover:text-destructive disabled:opacity-50"
                                  aria-label="Power off"
                                  title="Power off"
                                >
                                  {poweringVm === item.id ? (
                                    <Loader2 className="h-4 w-4 animate-spin" />
                                  ) : (
                                    <Square className="h-4 w-4" />
                                  )}
                                </button>
                              ) : (
                                <button
                                  onClick={(e) => handlePower(e, item, "on")}
                                  disabled={poweringVm === item.id}
                                  className="rounded p-1.5 text-muted-foreground hover:bg-green-500/20 hover:text-green-500 disabled:opacity-50"
                                  aria-label="Power on"
                                  title="Power on"
                                >
                                  {poweringVm === item.id ? (
                                    <Loader2 className="h-4 w-4 animate-spin" />
                                  ) : (
                                    <Play className="h-4 w-4" />
                                  )}
                                </button>
                              )}
                            </div>
                          )}

                          {/* Badge */}
                          {item.badge && (
                            <span
                              className={cn(
                                "flex-shrink-0 rounded px-2 py-0.5 text-xs font-medium",
                                item.badgeVariant === "success" && "bg-green-600/20 text-green-500",
                                item.badgeVariant === "warning" && "bg-yellow-600/20 text-yellow-500",
                                item.badgeVariant === "secondary" && "bg-muted text-muted-foreground",
                                !item.badgeVariant && "bg-muted text-muted-foreground"
                              )}
                            >
                              {item.badge}
                            </span>
                          )}
                        </div>
                      );
                    })}
                  </div>
                );
              })
            )}
          </div>

          {/* Footer with keyboard hints */}
          <div className="flex items-center justify-between border-t px-4 py-2 text-xs text-muted-foreground">
            <div className="flex items-center gap-4">
              <span className="flex items-center gap-1">
                <kbd className="rounded border bg-muted px-1.5 py-0.5 font-mono">↑</kbd>
                <kbd className="rounded border bg-muted px-1.5 py-0.5 font-mono">↓</kbd>
                <span>navigate</span>
              </span>
              <span className="flex items-center gap-1">
                <kbd className="rounded border bg-muted px-1.5 py-0.5 font-mono">↵</kbd>
                <span>select</span>
              </span>
            </div>
            <span className="flex items-center gap-1">
              <kbd className="rounded border bg-muted px-1.5 py-0.5 font-mono">esc</kbd>
              <span>close</span>
            </span>
          </div>
        </div>
      </div>
    </div>
  );

  return createPortal(content, document.body);
}
