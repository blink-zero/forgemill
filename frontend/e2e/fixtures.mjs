// In-browser fixtures shared by the screenshot tour (tour.mjs) and the demo
// recording (demo.mjs). The seeded instance's targets are fictional, so the
// calls that would reach a hypervisor — and the ones that would reach a model
// — are answered here; everything else hits the real API on the real build.

export const NFS_SCRIPT = "#!/bin/bash\nset -euo pipefail\nexport DEBIAN_FRONTEND=noninteractive\n: \"${NFS_SERVER:?NFS_SERVER is required}\"\n: \"${NFS_EXPORT:?NFS_EXPORT is required}\"\n: \"${MOUNT_POINT:?MOUNT_POINT is required}\"\n\n. /etc/os-release\ncase \"${ID_LIKE:-$ID}\" in\n  *debian*) command -v mount.nfs >/dev/null || apt-get install -y nfs-common ;;\n  *rhel*|*fedora*) command -v mount.nfs >/dev/null || dnf install -y nfs-utils ;;\nesac\n\nmkdir -p \"$MOUNT_POINT\"\nif ! grep -qs \" $MOUNT_POINT \" /etc/fstab; then\n  echo \"$NFS_SERVER:$NFS_EXPORT $MOUNT_POINT nfs defaults,_netdev 0 0\" >> /etc/fstab\nfi\nmountpoint -q \"$MOUNT_POINT\" || mount \"$MOUNT_POINT\"\n";

const NFS_PARAMS = [
  { name: "NFS_SERVER", label: "NFS server", type: "string", required: true, default: "", placeholder: "nas.lab.internal", options: null, description: "Hostname or IP of the NFS server" },
  { name: "NFS_EXPORT", label: "Export path", type: "string", required: true, default: "", placeholder: "/volume1/data", options: null, description: "" },
  { name: "MOUNT_POINT", label: "Mount point", type: "string", required: true, default: "/data", placeholder: "", options: null, description: "" },
];
const NFS_REVIEW = { summary: "Mounts an NFS export idempotently and persists it; safe to re-run.", risk: "low", idempotent: true, distro_support: { debian: true, rhel: true }, findings: [{ severity: "info", source: "model", line: 16, title: "fstab line is appended, never updated", detail: "Changing the export later leaves the old line in place.", suggestion: "Match on the mount point and replace the line with sed if it exists." }], lint_only: false, model: "claude-sonnet-5", redaction: { counts: {}, total: 0 }, duration_ms: 4100 };
const NFS_NOTES = ["Installs nfs-common (Debian/Ubuntu) or nfs-utils (RHEL family) only if the NFS client is missing."];
const NFS_WARNINGS = ["Edits /etc/fstab; a wrong export path makes the next boot wait on the mount (_netdev limits the damage)."];
export const NFS_PROMPT = "Mount an NFS export at a mount point given as a parameter and persist it in fstab. Install the NFS client if missing. Debian and RHEL families.";

// The saved draft, as the Actions page reloads it after the job.
export const DRAFT_ACTION = {
  id: 9001, name: "Mount NFS share", description: "Mounts an NFS export at a mount point and persists it in /etc/fstab. Debian and RHEL families.", category: "scripts", script: NFS_SCRIPT, script_type: "bash", platform: "linux", builtin: false,
  parameters: NFS_PARAMS, tags: ["nfs", "storage"], version: 1, created_at: "2026-10-10 03:00:00", updated_at: "2026-10-10 03:00:00", status: "draft", source: "ai", reviewed_at: "2026-10-10 03:00:05",
  review: { ...NFS_REVIEW, script_hash: "abc" },
  draft_meta: { prompt: NFS_PROMPT, model: "claude-sonnet-5", notes: NFS_NOTES, warnings: NFS_WARNINGS, drafted_at: "2026-10-10T03:00:00Z" },
};

const json = (route, body, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });

// Hypervisor-backed calls the seeded instance can't answer.
export async function mockHypervisor(page, fixedTime) {
  await page.route("**/api/targets/*/resources", (route) => json(route, {
    platform: "vcenter", datacenters: [{ name: "Lab-DC", id: "dc" }], clusters: [{ name: "Lab-Cluster", id: "c" }], hosts: [],
    datastores: [{ name: "ds-nvme-01", id: "d1" }, { name: "ds-sata-01", id: "d2" }],
    networks: [{ name: "VM Network", id: "n1", path: "/Lab-DC/network/VM Network" }, { name: "dvPG-Backend", id: "n2", path: "/Lab-DC/network/dvPG-Backend" }],
    folders: [{ name: "Production", id: "f1" }], resource_pools: [], defaults: { datastore: "ds-nvme-01", network: "/Lab-DC/network/VM Network" } }));
  await page.route("**/api/deploy/preflight", (route) => json(route, { valid: true, blockers: [], warnings: [] }));
  await page.route("**/api/vms/*/nics", (route) => route.request().method() === "GET"
    ? json(route, [
        { key: 4000, label: "Network adapter 1", adapter_type: "vmxnet3", network: "VM Network", mac_address: "00:50:56:AA:BB:01", connected: true, addresses: ["10.20.10.11", "fe80::250:56ff:feaa:bb01"] },
        { key: 4001, label: "Network adapter 2", adapter_type: "vmxnet3", network: "dvPG-Backend", mac_address: "00:50:56:AA:BB:02", connected: false, start_connected: true, addresses: [] },
        { key: 4002, label: "Network adapter 3", adapter_type: "e1000e", network: "dvPG-Backend", mac_address: "00:50:56:AA:BB:03", connected: false, pending: true, addresses: [] }])
    : route.continue());
  await page.route("**/api/vms/*/disks", (route) => route.request().method() === "GET"
    ? json(route, [
        { key: 2000, label: "Hard disk 1", size_gb: 40, datastore: "ds-nvme-01", provisioning: "thin", backing: "[ds-nvme-01] web-01/web-01.vmdk" },
        { key: 2001, label: "Hard disk 2", size_gb: 100, datastore: "ds-sata-01", provisioning: "thick", backing: "[ds-nvme-01] web-01/web-01_1.vmdk" }])
    : route.continue());
  await page.route("**/api/targets/*/discover*", (route) => json(route, {
    target_id: 1, target_name: "vcenter-lab", computed_at: fixedTime || new Date().toISOString(), managed: 5, unmanaged: 3, ignored: 1,
    vms: [
      { ref: "vm-4101", name: "jenkins-agent-02", power_state: "poweredOn", ip_address: "10.20.10.44", cpu: 4, memory_mb: 8192, disk_gb: 80, guest_id: "ubuntu64Guest", host: "esx-01", ignored: false },
      { ref: "vm-4102", name: "legacy-crm", power_state: "poweredOn", ip_address: "10.20.10.70", cpu: 2, memory_mb: 4096, disk_gb: 120, guest_id: "windows2019srvNext_64Guest", host: "esx-02", ignored: false },
      { ref: "vm-4103", name: "old-test-vm", power_state: "poweredOff", cpu: 2, memory_mb: 2048, disk_gb: 20, host: "esx-01", ignored: false }] }));
  await page.route("**/api/vms/*/credentials", (route) => json(route, { username: "forgemill", password: "s3cr3t-Pa55", kind: "password", source: "deployment" }));
  // The only server-clock value rendered: the admin's last login, stamped at
  // real time by the login just performed. Pin it so the Users table lays out
  // identically on every run.
  await page.route("**/api/users", async (route) => {
    if (route.request().method() !== "GET") return route.continue();
    const res = await route.fetch();
    const users = await res.json();
    if (Array.isArray(users)) for (const u of users) if (u.last_login_at) u.last_login_at = fixedTime || u.last_login_at;
    await route.fulfill({ response: res, body: JSON.stringify(users) });
  });
}

// AI assistance switched on, with the draft job answered by a fixture.
// Returns a function that removes the routes again.
export async function mockAI(page) {
  await page.route("**/api/ai/status", (route) => json(route, { enabled: true, configured: true, provider: "anthropic", model: "claude-sonnet-5", key_set: true, redact_hostnames: false }));
  await page.route("**/api/ai/jobs/draft", (route) => json(route, { id: "tourjob1", kind: "draft", status: "running", stage: "drafting", started_at: new Date().toISOString(), elapsed_ms: 0 }, 202));
  await page.route("**/api/ai/jobs/tourjob1", (route) => json(route, { id: "tourjob1", kind: "draft", status: "done", started_at: new Date().toISOString(), finished_at: new Date().toISOString(), elapsed_ms: 6200, draft: {
    name: DRAFT_ACTION.name, description: DRAFT_ACTION.description, category: "scripts", script: NFS_SCRIPT, parameters: NFS_PARAMS,
    tags: ["nfs", "storage"], notes: NFS_NOTES, warnings: NFS_WARNINGS, review: NFS_REVIEW,
    model: "claude-sonnet-5", redaction: { counts: {}, total: 0 }, duration_ms: 6200, action_id: 9001 } }));
  await page.route("**/api/actions?include_drafts=true", async (route) => {
    const res = await route.fetch(); const body = await res.json();
    return route.fulfill({ response: res, body: JSON.stringify([DRAFT_ACTION, ...body]) });
  });
  return async () => {
    await page.unroute("**/api/ai/status"); await page.unroute("**/api/ai/jobs/draft"); await page.unroute("**/api/ai/jobs/tourjob1"); await page.unroute("**/api/actions?include_drafts=true");
  };
}
