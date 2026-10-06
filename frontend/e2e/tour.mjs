// Screenshot tour of every primary route in both themes against a seeded
// Forgemill instance. Output is a directory of PNGs that diff.mjs can compare
// between two builds. Environment:
//   E2E_BASE_URL        default http://localhost:8097
//   E2E_SHOT_DIR        where PNGs go (required)
//   E2E_ADMIN_USER      default admin
//   E2E_ADMIN_PASSWORD  required
//   E2E_FIXED_TIME      ISO time to pin the browser clock to (optional; run.sh
//                       passes the seed's reference time so relative labels
//                       are stable)
// Uncaught page errors fail the run. The pauses between pages keep the tour
// under the API rate limit (5 req/s, burst 40 per IP).
import fs from "node:fs";
import { chromium } from "playwright";

const BASE = process.env.E2E_BASE_URL || "http://localhost:8097";
const OUT = process.env.E2E_SHOT_DIR;
const USER = process.env.E2E_ADMIN_USER || "admin";
const PASSWORD = process.env.E2E_ADMIN_PASSWORD;
const FIXED_TIME = process.env.E2E_FIXED_TIME;
if (!OUT || !PASSWORD) { console.error("E2E_SHOT_DIR and E2E_ADMIN_PASSWORD are required"); process.exit(2); }
fs.mkdirSync(OUT, { recursive: true });

const browser = await chromium.launch({ args: ["--no-sandbox"] });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let failed = false;

// Fixtures for the hypervisor-backed calls the seeded instance can't answer
// (the targets are fictional). Everything else hits the real API.
async function mockHypervisor(page) {
  await page.route("**/api/targets/*/resources", (route) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({
    platform: "vcenter", datacenters: [{ name: "Lab-DC", id: "dc" }], clusters: [{ name: "Lab-Cluster", id: "c" }], hosts: [],
    datastores: [{ name: "ds-nvme-01", id: "d1" }, { name: "ds-sata-01", id: "d2" }],
    networks: [{ name: "VM Network", id: "n1", path: "/Lab-DC/network/VM Network" }, { name: "dvPG-Backend", id: "n2", path: "/Lab-DC/network/dvPG-Backend" }],
    folders: [{ name: "Production", id: "f1" }], resource_pools: [], defaults: { datastore: "ds-nvme-01", network: "/Lab-DC/network/VM Network" } }) }));
  await page.route("**/api/deploy/preflight", (route) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ valid: true, blockers: [], warnings: [] }) }));
  await page.route("**/api/vms/*/nics", (route) => route.request().method() === "GET"
    ? route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify([
        { key: 4000, label: "Network adapter 1", adapter_type: "vmxnet3", network: "VM Network", mac_address: "00:50:56:AA:BB:01", connected: true, addresses: ["10.20.10.11", "fe80::250:56ff:feaa:bb01"] },
        { key: 4001, label: "Network adapter 2", adapter_type: "vmxnet3", network: "dvPG-Backend", mac_address: "00:50:56:AA:BB:02", connected: false, start_connected: true, addresses: [] },
        { key: 4002, label: "Network adapter 3", adapter_type: "e1000e", network: "dvPG-Backend", mac_address: "00:50:56:AA:BB:03", connected: false, pending: true, addresses: [] }]) })
    : route.continue());
  await page.route("**/api/vms/*/disks", (route) => route.request().method() === "GET"
    ? route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify([
        { key: 2000, label: "Hard disk 1", size_gb: 40, datastore: "ds-nvme-01", provisioning: "thin", backing: "[ds-nvme-01] web-01/web-01.vmdk" },
        { key: 2001, label: "Hard disk 2", size_gb: 100, datastore: "ds-sata-01", provisioning: "thick", backing: "[ds-nvme-01] web-01/web-01_1.vmdk" }]) })
    : route.continue());
  await page.route("**/api/targets/*/discover*", (route) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({
    target_id: 1, target_name: "vcenter-lab", computed_at: FIXED_TIME || new Date().toISOString(), managed: 5, unmanaged: 3, ignored: 1,
    vms: [
      { ref: "vm-4101", name: "jenkins-agent-02", power_state: "poweredOn", ip_address: "10.20.10.44", cpu: 4, memory_mb: 8192, disk_gb: 80, guest_id: "ubuntu64Guest", host: "esx-01", ignored: false },
      { ref: "vm-4102", name: "legacy-crm", power_state: "poweredOn", ip_address: "10.20.10.70", cpu: 2, memory_mb: 4096, disk_gb: 120, guest_id: "windows2019srvNext_64Guest", host: "esx-02", ignored: false },
      { ref: "vm-4103", name: "old-test-vm", power_state: "poweredOff", cpu: 2, memory_mb: 2048, disk_gb: 20, host: "esx-01", ignored: false }] }) }));
  await page.route("**/api/vms/*/credentials", (route) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ username: "forgemill", password: "s3cr3t-Pa55", kind: "password", source: "deployment" }) }));
  // The only server-clock value the tour renders: the admin's last login,
  // stamped at real time by the login the tour itself just performed. Pin it
  // so the Users table lays out identically on every run.
  await page.route("**/api/users", async (route) => {
    if (route.request().method() !== "GET") return route.continue();
    const res = await route.fetch();
    const users = await res.json();
    if (Array.isArray(users)) for (const u of users) if (u.last_login_at) u.last_login_at = FIXED_TIME || u.last_login_at;
    await route.fulfill({ response: res, body: JSON.stringify(users) });
  });
}

async function run(theme) {
  const ctx = await browser.newContext({ viewport: { width: 1360, height: 860 }, deviceScaleFactor: 1 });
  const page = await ctx.newPage();
  const errors = []; page.on("pageerror", (e) => errors.push(e.message));
  const consoleMsgs = new Set(); page.on("console", (m) => { if (["warning", "error"].includes(m.type())) consoleMsgs.add(m.type() + ": " + m.text().slice(0, 160)); });
  if (FIXED_TIME) await page.clock.setFixedTime(new Date(FIXED_TIME));
  // Theme is a class on <html> persisted in localStorage; main.tsx applies it on boot.
  await page.addInitScript((t) => { localStorage.setItem("forgemill_theme", t); }, theme);
  await mockHypervisor(page);
  // Pages show a Loader2 spinner (svg.animate-spin) until their data has
  // arrived. Waiting for it to disappear — rather than a fixed pause — is what
  // keeps a cold CI runner from screenshotting a half-loaded page.
  const settle = async () => {
    await page.waitForFunction(() => document.querySelectorAll("main svg.animate-spin").length === 0, null, { timeout: 20000 }).catch(() => {});
  };
  const shot = async (name, full = false) => { await settle(); await page.screenshot({ path: `${OUT}/${theme}-${name}.png`, fullPage: full }); console.log(`${theme}: ${name}`); };
  const go = async (path, waitFor) => { await page.goto(BASE + path); if (waitFor) await page.waitForSelector(waitFor, { timeout: 20000 }); await settle(); await sleep(2200); };
  // For pages that only exist in the head build: the same tour also runs
  // against the PR's base, where the route or text may not exist yet. Returns
  // false (and skips the shot) instead of crashing the whole tour.
  const goSoft = async (path, waitFor) => {
    await page.goto(BASE + path);
    const ok = await page.waitForSelector(waitFor, { timeout: 8000 }).then(() => true, () => false);
    if (ok) { await settle(); await sleep(2200); } else console.log(`skip: ${path} (no ${waitFor})`);
    return ok;
  };

  await page.goto(BASE + "/login"); await sleep(800);
  await shot("00-login");
  await page.getByPlaceholder("Username").fill(USER);
  await page.getByPlaceholder("Password").fill(PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.waitForURL(BASE + "/", { timeout: 15000 }).catch(() => {});
  await page.waitForSelector("text=staging-app-04", { timeout: 20000 }).catch(() => {});
  await sleep(2500);
  await shot("01-dashboard");
  await sleep(6000);

  await go("/targets", "text=vcenter-lab"); await shot("02-targets"); await sleep(5000);
  if (await goSoft("/targets/1/discover", "text=jenkins-agent-02")) {
    await shot("02b-discover");
    const firstRow = page.getByLabel("Select jenkins-agent-02");
    if (await firstRow.count()) { await firstRow.check(); await sleep(400); await shot("02c-discover-selected"); }
    await sleep(5000);
  }
  await go("/templates", "text=ubuntu-24.04-cloudinit"); await shot("03-templates"); await sleep(5000);
  await go("/vms", "text=web-01"); await shot("04-vms-cards");
  await page.locator("button[title='Table view']").click(); await sleep(800); await shot("05-vms-table");
  await page.locator("button[title='Card view']").click(); await sleep(5000);

  // VM detail: overview, operations panels, credentials, armed danger zone, snapshots, actions.
  await go("/vms/1", "text=Network Adapters"); await sleep(1500); await shot("06-vm-detail", true);
  await page.getByRole("button", { name: /^Resize$/ }).click(); await sleep(400);
  const addNic = page.getByRole("button", { name: /Add Network Adapter/ });
  if (await addNic.count()) { await addNic.click(); await sleep(1200); }
  const addDisk = page.getByRole("button", { name: /^Add Disk$/ });
  if (await addDisk.count()) { await addDisk.click(); await sleep(1200); }
  await shot("06b-vm-operations-open", true);
  await page.getByRole("button", { name: /^Resize$/ }).click(); await sleep(300);
  if (await addNic.count()) { await addNic.click(); await sleep(300); }
  if (await addDisk.count()) { await addDisk.click(); await sleep(300); }
  // Credentials load on mount (masked); show the password, then the set-credentials form.
  const showPwd = page.getByRole("button", { name: "Show password" }).first();
  if (await showPwd.count()) { await showPwd.click(); await sleep(400); await shot("06c-vm-credentials", true); await page.getByRole("button", { name: "Hide password" }).first().click(); }
  const override = page.getByRole("button", { name: /^(Override|Change)$/ }).first();
  if (await override.count()) {
    await override.click(); await sleep(400);
    const help = page.getByRole("button", { name: /How to set up passwordless sudo/ });
    if (await help.count()) { await help.click(); await sleep(300); await help.scrollIntoViewIfNeeded(); await sleep(200); }
    await shot("06d-vm-credentials-set", true);
    await page.getByRole("button", { name: /^Cancel$/ }).first().click(); await sleep(300);
  }
  await page.getByRole("button", { name: "Destroy VM" }).click(); await sleep(600);
  await page.getByPlaceholder("web-01").fill("web-0"); await sleep(300);
  await page.locator("text=Danger zone").first().scrollIntoViewIfNeeded(); await sleep(300);
  await shot("07-vm-danger-zone-armed", true);
  await page.getByRole("button", { name: "Destroy VM" }).click(); await sleep(400);
  await page.getByRole("button", { name: "Snapshots" }).first().click(); await sleep(1200);
  await shot("07b-vm-snapshots");
  // A VM on an inventory-only host: banner + disabled hypervisor controls.
  if (await goSoft("/vms/9", "text=Inventory-only host")) await shot("07c-vm-inventory-only");
  const revert = page.getByRole("button", { name: /Revert/ }).first();
  if (await revert.count()) { await revert.click(); await sleep(900); await shot("08-confirm-destructive"); await page.keyboard.press("Escape"); await sleep(300); }
  await sleep(6000);
  await go("/vms/1?tab=actions", "text=Execution History"); await sleep(1500); await shot("08b-vm-actions", true);
  await sleep(5000);

  await go("/deploy", "text=ubuntu-24.04-cloudinit"); await shot("09-deploy-template");
  await page.locator("text=ubuntu-24.04-cloudinit").first().click(); await sleep(2200);
  await page.getByPlaceholder("web-server-01").fill("web-03"); await sleep(400); await shot("10-deploy-configure", true);
  await sleep(6000);
  await go("/actions", "text=Security Hardening"); await shot("11-actions"); await sleep(5000);
  // Action editor: the Check panel (lint only — AI is off in the seed).
  const newAction = page.getByRole("button", { name: /Create Action/ }).first();
  if (await newAction.count()) {
    await newAction.click(); await sleep(500);
    await page.getByPlaceholder("Install Nginx").fill("Install nginx");
    await page.locator("textarea").first().fill("#!/bin/bash\napt-get update\napt-get install nginx\nDB_PASSWORD=hunter2\nrm -rf \"$TARGET_DIR\"/*\ncurl -fsSL https://get.docker.com | sh\n");
    const check = page.getByRole("button", { name: /Check script|Check with AI/ });
    if (await check.count()) { await check.click(); await page.waitForSelector("text=Script check", { timeout: 10000 }).catch(() => {}); await sleep(600); await shot("11b-actions-check", true); }
    const cancel = page.getByRole("button", { name: /^Cancel$/ }).first();
    if (await cancel.count()) await cancel.click();
    await sleep(2000);
  }
  await go("/factory", "text=Available Operating Systems"); await shot("15-factory"); await sleep(5000);
  await go("/history", "text=staging-app-04"); await shot("12-history"); await sleep(5000);
  await go("/settings", "text=Settings"); await sleep(1000); await shot("13-settings", true);
  // Deep link: the tab comes from ?tab=, so API Keys must be active on a fresh load.
  await sleep(4000);
  await go("/settings?tab=apikeys", "text=API Key Authentication"); await shot("13b-settings-deeplink-apikeys");
  await sleep(4000);
  await go("/settings?tab=diagnostics", "text=Recent server errors"); await shot("13c-settings-diagnostics", true);
  await sleep(4000);
  if (await goSoft("/settings?tab=preferences", "text=VM Adoption")) await shot("13d-settings-preferences", true);
  await sleep(4000);
  if (await goSoft("/settings?tab=ai", "text=AI assistance")) await shot("13e-settings-ai", true);
  const more = page.locator("button[aria-label*='ctions'], button:has(svg.lucide-ellipsis), button:has(svg.lucide-more-horizontal)").first();
  if (await more.count()) { await more.click(); await sleep(500); const del = page.getByRole("menuitem", { name: /Delete user/ }); if (await del.count()) { await del.click(); await sleep(900); await shot("14-confirm-typed"); await page.keyboard.press("Escape"); } }

  console.log(`${theme} page errors:`, errors.length ? errors : "none");
  console.log(`${theme} console warn/error:`, consoleMsgs.size ? [...consoleMsgs] : "none");
  if (errors.length) failed = true;
  await ctx.close();
}

await run("dark");
await run("light");
await browser.close();
process.exit(failed ? 1 : 0);
