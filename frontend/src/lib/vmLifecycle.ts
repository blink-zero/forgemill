import type { ManagedVM } from "@/types";

/** Formats a millisecond duration compactly: "3d 14h", "45m", "12s". */
export function formatDuration(ms: number): string {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000));
  const days = Math.floor(totalSeconds / 86400);
  const hours = Math.floor((totalSeconds % 86400) / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${minutes}m`;
  if (minutes > 0) return `${minutes}m`;
  return `${totalSeconds}s`;
}

const RUNNING_STATES = new Set(["poweredOn", "running"]);
const STOPPED_STATES = new Set(["poweredOff", "stopped"]);
const SUSPENDED_STATES = new Set(["suspended", "paused"]);

export interface VMLifecycleLabel {
  /** e.g. "Up 3d 14h", "Off 4h ago", "Paused (up 4d 2h)". Empty if unknown. */
  label: string;
  /** True only while actively running — the caller can restyle/animate live values. */
  isLive: boolean;
}

/**
 * Builds the relative-time subtext shown under a VM's status badge.
 * `now` is passed in (from the shared ticker) rather than read internally,
 * so a single 60s tick re-renders every row without each row polling the
 * clock itself.
 */
export function vmLifecycleLabel(vm: ManagedVM, now: number): VMLifecycleLabel {
  const state = vm.power_state;
  const onAt = vm.last_powered_on_at ? new Date(vm.last_powered_on_at).getTime() : null;
  const changedAt = vm.state_changed_at ? new Date(vm.state_changed_at).getTime() : null;

  if (RUNNING_STATES.has(state)) {
    const start = onAt ?? changedAt;
    if (start == null) return { label: "", isLive: false };
    return { label: `Up ${formatDuration(now - start)}`, isLive: true };
  }

  if (SUSPENDED_STATES.has(state)) {
    // Frozen at the point of suspension: the stretch that was running
    // immediately before suspend, not reset to zero and not still ticking.
    if (onAt != null && changedAt != null && changedAt > onAt) {
      return { label: `Paused (up ${formatDuration(changedAt - onAt)})`, isLive: false };
    }
    return { label: "Paused", isLive: false };
  }

  if (STOPPED_STATES.has(state)) {
    const offAt = vm.last_powered_off_at ? new Date(vm.last_powered_off_at).getTime() : changedAt;
    if (offAt == null) return { label: "", isLive: false };
    return { label: `Off ${formatDuration(now - offAt)} ago`, isLive: false };
  }

  return { label: "", isLive: false };
}

/**
 * Total lifetime runtime, live: the stored cumulative total plus the
 * elapsed time of the current stretch if the VM is running right now.
 */
export function totalLifetimeRuntimeMs(vm: ManagedVM, now: number): number {
  let totalMs = (vm.total_runtime_seconds || 0) * 1000;
  if (RUNNING_STATES.has(vm.power_state) && vm.last_powered_on_at) {
    totalMs += now - new Date(vm.last_powered_on_at).getTime();
  }
  return totalMs;
}

// --- Search-box qualifiers: "uptime>30d", "state:stopped", "age>14d" ---

const STATE_ALIASES: Record<string, string> = {
  running: "poweredOn",
  poweredon: "poweredOn",
  on: "poweredOn",
  stopped: "poweredOff",
  poweredoff: "poweredOff",
  off: "poweredOff",
  suspended: "suspended",
  paused: "suspended",
};

function unitToMs(n: number, unit: string): number {
  switch (unit.toLowerCase()) {
    case "d": return n * 86400_000;
    case "h": return n * 3600_000;
    case "m": return n * 60_000;
    default: return n * 1000;
  }
}

export interface DurationCmp {
  op: "<" | ">";
  ms: number;
}

export interface VMQuery {
  /** Remaining free text, lowercased, after qualifiers are stripped out. */
  text: string;
  state?: string;
  uptime?: DurationCmp;
  age?: DurationCmp;
}

/**
 * Parses qualifiers out of the VMs search box: `state:stopped`,
 * `uptime>30d`, `age>14d` (also accepts `<`, and h/m/s units). Anything
 * left over after stripping recognized qualifiers is treated as plain
 * substring search text, same as before this existed.
 */
export function parseVMQuery(raw: string): VMQuery {
  const tokens = raw.trim().split(/\s+/).filter(Boolean);
  const rest: string[] = [];
  const q: VMQuery = { text: "" };

  for (const tok of tokens) {
    let m = tok.match(/^state:(\w+)$/i);
    if (m) {
      q.state = STATE_ALIASES[m[1].toLowerCase()] ?? m[1];
      continue;
    }
    m = tok.match(/^uptime([<>])(\d+)([dhm]?)$/i);
    if (m) {
      q.uptime = { op: m[1] as "<" | ">", ms: unitToMs(Number(m[2]), m[3] || "d") };
      continue;
    }
    m = tok.match(/^age([<>])(\d+)([dhm]?)$/i);
    if (m) {
      q.age = { op: m[1] as "<" | ">", ms: unitToMs(Number(m[2]), m[3] || "d") };
      continue;
    }
    rest.push(tok);
  }

  q.text = rest.join(" ").toLowerCase();
  return q;
}

function cmpMatches(elapsedMs: number, cmp: DurationCmp): boolean {
  return cmp.op === ">" ? elapsedMs > cmp.ms : elapsedMs < cmp.ms;
}

/** Whether vm satisfies a parsed `uptime` qualifier. Non-running VMs never match. */
export function matchesUptimeQuery(vm: ManagedVM, cmp: DurationCmp, now: number): boolean {
  if (!RUNNING_STATES.has(vm.power_state) || !vm.last_powered_on_at) return false;
  return cmpMatches(now - new Date(vm.last_powered_on_at).getTime(), cmp);
}

/** Whether vm satisfies a parsed `age` qualifier (based on created_at). */
export function matchesAgeQuery(vm: ManagedVM, cmp: DurationCmp, now: number): boolean {
  if (!vm.created_at) return false;
  return cmpMatches(now - new Date(vm.created_at).getTime(), cmp);
}
