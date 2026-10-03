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
  await page.route("**/api/vms/*/credentials", (route) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ username: "forgemill", password: "s3cr3t-Pa55" }) }));
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
  const shot = async (name, full = false) => { await page.screenshot({ path: `${OUT}/${theme}-${name}.png`, fullPage: full }); console.log(`${theme}: ${name}`); };
  const go = async (path, waitFor) => { await page.goto(BASE + path); if (waitFor) await page.waitForSelector(waitFor, { timeout: 20000 }); await sleep(2200); };

  await page.goto(BASE + "/login"); await sleep(800);
  await shot("00-login");
  await page.getByPlaceholder("Username").fill(USER);
  await page.getByPlaceholder("Password").fill(PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.waitForURL(BASE + "/", { timeout: 15000 }).catch(() => {});
  await sleep(2500);
  await shot("01-dashboard");
  await sleep(6000);

  await go("/targets", "text=vcenter-lab"); await shot("02-targets"); await sleep(5000);
  await go("/templates", "text=ubuntu-24.04-cloudinit"); await shot("03-templates"); await sleep(5000);
  await go("/vms", "text=web-01"); await shot("04-vms-cards");
  await page.locator("button[title='Table view']").click(); await sleep(800); await shot("05-vms-table");
  await page.locator("button[title='Card view']").click(); await sleep(5000);

  // VM detail: overview, operations panels, credentials, armed danger zone, snapshots, actions.
  await go("/vms/1", "text=Network Adapters"); await sleep(1500); await shot("06-vm-detail", true);
  await page.getByRole("button", { name: /^Resize$/ }).click(); await sleep(400);
  const addNic = page.getByRole("button", { name: /Add Network Adapter/ });
  if (await addNic.count()) { await addNic.click(); await sleep(1200); }
  await shot("06b-vm-operations-open", true);
  await page.getByRole("button", { name: /^Resize$/ }).click(); await sleep(300);
  if (await addNic.count()) { await addNic.click(); await sleep(300); }
  const reveal = page.getByRole("button", { name: /Reveal Credentials/ }).first();
  if (await reveal.count()) { await reveal.click(); await sleep(900); await shot("06c-vm-credentials", true); }
  await page.getByRole("button", { name: "Destroy VM" }).click(); await sleep(600);
  await page.getByPlaceholder("web-01").fill("web-0"); await sleep(300);
  await page.locator("text=Danger zone").first().scrollIntoViewIfNeeded(); await sleep(300);
  await shot("07-vm-danger-zone-armed", true);
  await page.getByRole("button", { name: "Destroy VM" }).click(); await sleep(400);
  await page.getByRole("button", { name: "Snapshots" }).first().click(); await sleep(1200);
  await shot("07b-vm-snapshots");
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
  await go("/factory", "text=Template Factory"); await shot("15-factory"); await sleep(5000);
  await go("/history", "text=staging-app-04"); await shot("12-history"); await sleep(5000);
  await go("/settings", "text=Settings"); await sleep(1000); await shot("13-settings", true);
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
