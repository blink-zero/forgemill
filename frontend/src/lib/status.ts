/*
  Shared status → Badge-variant / label mappings.

  Each helper is the mapping its callers used before they were centralised
  here, verbatim. Deployment and action-execution status deliberately keep
  different mappings (a running deployment is `default`, a running execution
  is `warning`; a cancelled deployment is `warning`, a cancelled execution is
  `secondary`) — reconciling them is a visible change and belongs in its own PR.
*/

/** Hypervisor power state → badge tone. vSphere and Proxmox spellings both accepted. */
export const powerVariant = (state: string) => {
  if (state === "poweredOn" || state === "running") return "success" as const;
  if (state === "poweredOff" || state === "stopped") return "secondary" as const;
  if (state === "suspended") return "warning" as const;
  return "secondary" as const;
};

/** Hypervisor power state → human label. Unknown states pass through unchanged. */
export const powerLabel = (state: string) => {
  if (state === "poweredOn" || state === "running") return "Running";
  if (state === "poweredOff" || state === "stopped") return "Stopped";
  if (state === "suspended") return "Suspended";
  return state;
};

export const isPoweredOn = (state: string) => state === "poweredOn" || state === "running";
export const isPoweredOff = (state: string) => state === "poweredOff" || state === "stopped";

/** Deployment status (Dashboard, History, live deploy view). */
export const deploymentStatusVariant = (status: string) => {
  switch (status) {
    case "completed": return "success" as const;
    case "running": return "default" as const;
    case "failed": return "destructive" as const;
    case "cancelled": return "warning" as const;
    default: return "secondary" as const;
  }
};

/** Action-execution status (VM detail → Actions tab). */
export const executionStatusVariant = (status: string) => {
  if (status === "completed") return "success" as const;
  if (status === "failed") return "destructive" as const;
  if (status === "running" || status === "pending") return "warning" as const;
  if (status === "cancelled") return "secondary" as const;
  return "secondary" as const;
};

/** Template-factory build status → tint classes (Factory list and build progress page). */
export const buildStatusClasses: Record<string, string> = {
  pending: "bg-warning/10 text-warning",
  downloading: "bg-info/10 text-info",
  building: "bg-info/10 text-info",
  converting: "bg-info/10 text-info",
  completed: "bg-success/10 text-success",
  failed: "bg-destructive/10 text-destructive",
  cancelled: "bg-gray-500/10 text-gray-500",
};
