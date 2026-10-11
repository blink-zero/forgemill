// Narrated walkthrough of Forgemill, recorded as video for the README demo
// (docs/demo.gif). Runs against a seeded instance — the same fixtures and
// in-browser mocks as the screenshot tour — with a drawn cursor, click
// ripples and a caption bar injected into the page. demo.sh is the driver:
// seed, serve, record, convert.
//
//   E2E_BASE_URL        default http://localhost:8097
//   E2E_ADMIN_USER      default admin
//   E2E_ADMIN_PASSWORD  required
//   E2E_FIXED_TIME      ISO time to pin the browser clock to (relative labels)
//   DEMO_OUT            directory the .webm is written to (required)
//   DEMO_SHOTS          optional directory: a PNG is saved at every caption
//   DEMO_FAST=1         skip the holds (dry run for selectors)
import fs from "node:fs";
import path from "node:path";
import { chromium } from "playwright";
import { mockHypervisor, mockAI, NFS_PROMPT } from "./fixtures.mjs";

const BASE = process.env.E2E_BASE_URL || "http://localhost:8097";
const OUT = process.env.DEMO_OUT;
const SHOTS = process.env.DEMO_SHOTS;
const USER = process.env.E2E_ADMIN_USER || "admin";
const PASSWORD = process.env.E2E_ADMIN_PASSWORD;
const FIXED_TIME = process.env.E2E_FIXED_TIME;
const FAST = process.env.DEMO_FAST === "1";
if (!OUT || !PASSWORD) { console.error("DEMO_OUT and E2E_ADMIN_PASSWORD are required"); process.exit(2); }
fs.mkdirSync(OUT, { recursive: true });
if (SHOTS) fs.mkdirSync(SHOTS, { recursive: true });

const W = 1280, H = 800;
const sleep = (ms) => new Promise((r) => setTimeout(r, FAST ? Math.min(ms, 120) : ms));

// Cursor, click ripple and caption bar. Installed on every document (the app
// is an SPA, but deep links reload it); state survives a reload through
// sessionStorage so the cursor doesn't jump and the caption doesn't blink.
const overlay = () => {
  if (!sessionStorage.getItem("fm-demo-theme")) { localStorage.setItem("forgemill_theme", "dark"); sessionStorage.setItem("fm-demo-theme", "1"); }
  const install = () => {
    if (document.getElementById("fm-demo-cursor")) return;
    const style = document.createElement("style");
    style.textContent = `
      #fm-demo-cursor{position:fixed;left:0;top:0;z-index:2147483646;pointer-events:none;filter:drop-shadow(0 1px 2px rgba(0,0,0,.55));will-change:transform}
      #fm-demo-ripple{position:fixed;width:34px;height:34px;border-radius:50%;border:3px solid #60a5fa;z-index:2147483645;pointer-events:none;opacity:0;transform:translate(-50%,-50%) scale(.3)}
      #fm-demo-ripple.on{animation:fmripple .45s ease-out}
      @keyframes fmripple{0%{opacity:.95;transform:translate(-50%,-50%) scale(.3)}100%{opacity:0;transform:translate(-50%,-50%) scale(1.25)}}
      #fm-demo-caption{position:fixed;left:50%;bottom:20px;transform:translateX(-50%) translateY(8px);max-width:76%;padding:11px 20px;border-radius:12px;background:rgba(12,15,22,.93);color:#f3f4f6;font:500 18px/1.35 "Inter Variable",Inter,system-ui,sans-serif;box-shadow:0 10px 32px rgba(0,0,0,.5);border:1px solid rgba(96,165,250,.5);z-index:2147483647;pointer-events:none;opacity:0;transition:opacity .22s ease,transform .22s ease;text-align:center;letter-spacing:.005em}
      #fm-demo-caption.on{opacity:1;transform:translateX(-50%) translateY(0)}
      #fm-demo-caption b{color:#93c5fd;font-weight:650}
      #fm-demo-caption code{font:500 16px/1 "JetBrains Mono Variable","JetBrains Mono",ui-monospace,monospace;background:rgba(255,255,255,.09);padding:2px 6px;border-radius:5px}
      *{scroll-behavior:auto!important}`;
    document.head.appendChild(style);
    const cur = document.createElement("div"); cur.id = "fm-demo-cursor";
    cur.innerHTML = '<svg width="22" height="30" viewBox="0 0 22 30"><path d="M2 2 L2 24 L8 18.5 L12 28 L16 26.3 L12 17 L20 17 Z" fill="#fff" stroke="#111" stroke-width="1.6" stroke-linejoin="round"/></svg>';
    const rip = document.createElement("div"); rip.id = "fm-demo-ripple";
    const cap = document.createElement("div"); cap.id = "fm-demo-caption";
    document.body.append(cur, rip, cap);
    const place = (x, y) => { cur.style.transform = `translate(${x - 2}px,${y - 2}px)`; };
    const saved = sessionStorage.getItem("fm-demo-cursor");
    if (saved) place(...saved.split(",").map(Number)); else place(-50, -50);
    document.addEventListener("mousemove", (e) => { place(e.clientX, e.clientY); sessionStorage.setItem("fm-demo-cursor", e.clientX + "," + e.clientY); }, true);
    document.addEventListener("mousedown", (e) => { rip.style.left = e.clientX + "px"; rip.style.top = e.clientY + "px"; rip.classList.remove("on"); void rip.offsetWidth; rip.classList.add("on"); }, true);
    const text = sessionStorage.getItem("fm-demo-caption");
    if (text) { cap.innerHTML = text; cap.classList.add("on"); }
  };
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", install); else install();
};

const browser = await chromium.launch({ args: ["--no-sandbox", "--font-render-hinting=none"] });
const ctx = await browser.newContext({ viewport: { width: W, height: H }, deviceScaleFactor: 1, recordVideo: { dir: OUT, size: { width: W, height: H } } });
const page = await ctx.newPage();
const errors = []; page.on("pageerror", (e) => errors.push(e.message));
if (FIXED_TIME) await page.clock.setFixedTime(new Date(FIXED_TIME));
await page.addInitScript(overlay);
await mockHypervisor(page, FIXED_TIME);
// Packer is not installed on the recording machine; show the Factory as it looks on a set-up instance.
await page.route("**/api/factory/prerequisites", (route) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ packer_installed: true, packer_version: "1.12.0" }) }));
// Registered after the shared fixture so it wins: a slow pre-flight, so the
// "checking whether this deploy would succeed" state is on screen for a moment.
await page.route("**/api/deploy/preflight", async (route) => { await sleep(1500); await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ valid: true, blockers: [], warnings: [] }) }); });

// ---- helpers --------------------------------------------------------------
let shotN = 0;
const settle = () => page.waitForFunction(() => document.querySelectorAll("main svg.animate-spin").length === 0, null, { timeout: 20000 }).catch(() => {});
const caption = async (html, hold = 0) => {
  await page.evaluate((h) => { const c = document.getElementById("fm-demo-caption"); if (!c) return; c.innerHTML = h; c.classList.add("on"); sessionStorage.setItem("fm-demo-caption", h); }, html);
  console.log("caption:", html.replace(/<[^>]+>/g, ""));
  if (SHOTS) { await sleep(350); await page.screenshot({ path: path.join(SHOTS, `${String(++shotN).padStart(2, "0")}.png`) }); }
  if (hold) await sleep(hold);
};
const captionOff = async () => page.evaluate(() => { document.getElementById("fm-demo-caption")?.classList.remove("on"); sessionStorage.removeItem("fm-demo-caption"); });

let mouse = { x: -50, y: -50 };
const moveTo = async (target, { fx = 0.5, fy = 0.5, settleMs = 100 } = {}) => {
  const box = await target.boundingBox();
  if (!box) throw new Error("moveTo: target has no box");
  const x = box.x + box.width * fx, y = box.y + box.height * fy;
  const steps = FAST ? 2 : Math.max(5, Math.min(12, Math.round(Math.hypot(x - mouse.x, y - mouse.y) / 60)));
  await page.mouse.move(x, y, { steps }); mouse = { x, y };
  await sleep(settleMs);
};
const click = async (target, opts = {}) => {
  await target.scrollIntoViewIfNeeded(); await sleep(60);
  await moveTo(target, opts);
  await page.mouse.down(); await sleep(80); await page.mouse.up();
  await sleep(opts.after ?? 220);
};
const type = async (target, text, delay = 42) => { await click(target, { after: 150 }); await page.keyboard.type(text, { delay: FAST ? 0 : delay }); await sleep(250); };
const wheel = async (dy, chunks = 6) => { for (let i = 0; i < chunks; i++) { await page.mouse.wheel(0, dy / chunks); await sleep(FAST ? 5 : 35); } await sleep(200); };
const go = async (p, waitFor) => { await page.goto(BASE + p); if (waitFor) await page.waitForSelector(waitFor, { timeout: 20000 }); await settle(); await sleep(250); };
const nav = async (label, waitFor) => {
  await click(page.getByRole("navigation", { name: "Primary" }).getByRole("link", { name: label, exact: true }), { after: 200 });
  if (waitFor) await page.waitForSelector(waitFor, { timeout: 20000 });
  await settle(); await sleep(250);
};
const tab = async (label) => click(page.getByRole("button", { name: label, exact: true }).first(), { after: 700 });

// ---- 0. sign in -----------------------------------------------------------
await page.goto(BASE + "/login"); await sleep(900);
await caption("<b>Forgemill</b> — self-hosted VM deployment and lifecycle for vCenter, ESXi and Proxmox VE", 1800);
await type(page.getByPlaceholder("Username"), USER);
await type(page.getByPlaceholder("Password"), PASSWORD, 20);
await caption("Local accounts, <b>LDAP / Active Directory</b> with group → role mapping, scoped API keys");
await click(page.getByRole("button", { name: "Sign in" }));
await page.waitForURL(BASE + "/", { timeout: 15000 }).catch(() => {});
await page.waitForSelector("text=staging-app-04", { timeout: 20000 }).catch(() => {});
await settle(); await sleep(700);

// ---- 1. dashboard ---------------------------------------------------------
await caption("<b>Dashboard</b> — the fleet at a glance, what needs attention, live target health", 2600);

// ---- 2. targets + discover ------------------------------------------------
await nav("Targets", "text=vcenter-lab");
await caption("<b>Targets</b> — vCenter, standalone ESXi and Proxmox VE behind one API; licenses and evaluations watched for you", 2000);
await go("/targets/1/discover", "text=jenkins-agent-02");
await caption("<b>Discover & adopt</b> — VMs Forgemill didn't create, read live from the hypervisor", 900);
await click(page.getByLabel("Select jenkins-agent-02"));
await click(page.getByLabel("Select legacy-crm"));
await caption("Adopt the ones you want: nothing changes on the hypervisor, and they get power, snapshots, disks, NICs and actions right away");
await moveTo(page.getByRole("button", { name: /^Adopt/ }).first()); await sleep(1600);

// ---- 3. templates + factory -----------------------------------------------
await nav("Templates", "text=ubuntu-24.04-cloudinit");
await caption("<b>Templates</b> — synced from every target; Factory-built ones carry a version, lineage and update detection", 1800);
await nav("Template Factory", "text=Available Operating Systems");
await caption("<b>Template Factory</b> — ISO → golden template with Packer: Ubuntu and Rocky built-in, scheduled rebuilds, rebuild when the ISO changes", 2200);

// ---- 4. deploy wizard -----------------------------------------------------
await nav("Deploy", "text=ubuntu-24.04-cloudinit");
await caption("<b>Deploy</b> — pick a template…", 900);
await click(page.locator("text=ubuntu-24.04-cloudinit").first(), { after: 1400 });
await type(page.getByPlaceholder("web-server-01"), "web-03");
await caption("…size it, add extra disks and NICs, set cloud-init: the form adapts to what the hypervisor supports", 400);
await click(page.getByRole("button", { name: /Add extra disk/ }), { after: 700 });
await click(page.getByRole("button", { name: /Add extra network adapter/ }), { after: 700 });
const nicSelect = page.locator('select:has(option:text-is("Select network…"))').last();
await click(nicSelect, { after: 200 }); await nicSelect.selectOption({ label: "dvPG-Backend" }); await sleep(500);
await wheel(520); await sleep(900);
const review = page.getByRole("button", { name: /Review Deployment|^Next/ }).first();
await click(review, { after: 300 });
await caption("<b>Pre-flight</b> — name collisions, network, datastore and placement are checked before anything is created", 2600);
await moveTo(page.getByRole("button", { name: /^Deploy$/ })); await sleep(600);

// ---- 5. deployment receipt --------------------------------------------------
await go("/deploy/6", "text=Deployment Log");
await caption("Live progress over WebSocket while a deploy runs…", 1200);
await tab("Receipt");
await caption("…and afterwards a <b>receipt</b>: inputs, outcome, who triggered it, where the credentials live, undo options; the log and audit trail merge into one timeline", 2400);

// ---- 6. VM inventory --------------------------------------------------------
await nav("VMs", "text=web-01");
await caption("<b>VMs</b> — the whole estate: deployed, adopted and registered, with current session and lifetime runtime", 1800);
const search = page.getByPlaceholder(/Search, or state:stopped/);
await type(search, "state:stopped");
await caption("Search qualifiers — <code>state:stopped</code>, <code>uptime>30d</code>, <code>age>14d</code>", 1800);

// ---- 7. VM page -------------------------------------------------------------
await go("/vms/1", "text=Network Adapters");
await caption("<b>VM page</b> — power, console, resize, disks, network adapters, credentials and lifecycle in one place", 1800);
await wheel(330); await sleep(500);
await caption("Every NIC with its network, MAC, VLAN and guest-reported addresses; every disk with its datastore and provisioning", 300);
await click(page.getByRole("button", { name: /Add Network Adapter/ }), { after: 600 });
await caption("Resize, hot-add disks and NICs — VLAN tags on Proxmox, thin / thick on vSphere, adapter models from the provider itself", 1700);
await click(page.getByRole("button", { name: /Add Network Adapter/ }), { after: 200 });
const showPwd = page.getByRole("button", { name: "Show password" }).first();
if (await showPwd.count()) {
  await click(showPwd, { after: 600 });
  await caption("Deploy credentials: AES-256 at rest, decrypted only on reveal; per-VM SSH logins for adopted VMs", 1600);
  await click(page.getByRole("button", { name: "Hide password" }).first(), { after: 200 });
}
await wheel(-1400); await sleep(300);
await click(page.getByRole("button", { name: "Snapshots" }).first(), { after: 1100 });
await caption("<b>Snapshots</b> — create, revert, delete; anything destructive is confirmed, deletes are previewed first", 1800);
await click(page.getByRole("button", { name: "Actions", exact: true }).first(), { after: 300 });
await page.waitForSelector("text=Execution History", { timeout: 20000 }); await settle(); await sleep(300);
await caption("<b>Actions on a VM</b> — run scripts over SSH with live output; history keeps the logs with secrets redacted", 2000);

// ---- 8. actions library + script check + AI draft -------------------------------
await nav("Actions", "text=Security Hardening");
await caption("<b>Actions library</b> — 23 built-in actions, versioned custom scripts with rollback, import / export, tags", 2000);
await click(page.getByRole("button", { name: /Create Action/ }).first(), { after: 600 });
await type(page.getByPlaceholder("Install Nginx"), "Install nginx");
const scriptInput = page.getByLabel("Script", { exact: true });
await click(scriptInput, { after: 100 });
await page.keyboard.type("#!/bin/bash\napt-get update\napt-get install nginx\nDB_PASSWORD=hunter2\nrm -rf \"$TARGET_DIR\"/*\ncurl -fsSL https://get.docker.com | sh\n", { delay: FAST ? 0 : 8 });
await click(page.getByRole("button", { name: /Check script|Check with AI/ }), { after: 300 });
await page.waitForSelector("text=Script check", { timeout: 10000 }).catch(() => {});
await caption("<b>Script check</b> — destructive commands, secrets in the script, <code>curl | sh</code>, missing <code>set -e</code>, distro assumptions", 400);
await wheel(430); await sleep(1600);
const selectFixable = page.getByRole("button", { name: /Select all fixable/ });
if (await selectFixable.count()) {
  await click(selectFixable, { after: 300 });
  await click(page.getByTestId("action-fix-selected"), { after: 300 });
  await page.waitForSelector('[data-testid="action-fix-panel"]', { timeout: 10000 }).catch(() => {});
  await caption("Fixes are proposed as a <b>diff</b> and applied with one click", 1500);
  const apply = page.getByTestId("action-fix-apply");
  if (await apply.count()) { await click(apply, { after: 900 }); await scriptInput.scrollIntoViewIfNeeded(); await sleep(800); }
}
await click(page.getByRole("button", { name: /^Cancel$/ }).first(), { after: 400 });

const unmockAI = await mockAI(page);
await go("/actions", "text=Security Hardening");
await click(page.getByRole("button", { name: /Create Action/ }).first(), { after: 500 });
const draftBtn = page.getByRole("button", { name: /Draft with AI/ });
if (await draftBtn.count()) {
  await click(draftBtn, { after: 500 });
  await caption("Optional <b>AI assist</b> (Anthropic, OpenAI-compatible, Ollama) — describe the action…", 300);
  await type(page.getByPlaceholder(/Mount an NFS export/), NFS_PROMPT, 5);
  await click(page.getByRole("button", { name: /Generate draft/ }), { after: 400 });
  await page.waitForSelector("text=Review draft", { timeout: 10000 }).catch(() => {});
  await caption("…and get a reviewed draft in Forgemill's conventions, saved as a <b>non-runnable draft</b> until you read it and Publish", 2000);
  await wheel(380); await sleep(1200);
  await click(page.getByRole("button", { name: /^Cancel$/ }).first(), { after: 300 });
} else {
  await click(page.getByRole("button", { name: /^Cancel$/ }).first(), { after: 300 });
}
await unmockAI();

// ---- 9. history + settings + bell -----------------------------------------------
await nav("History", "text=staging-app-04");
await caption("<b>History</b> — every deployment with its status, receipt and timeline", 1600);
await nav("Settings", "text=Settings");
await tab("API Keys");
await caption("<b>Settings</b> — users and LDAP / AD, API keys with a role and scope of their own, HMAC-signed webhooks", 1800);
await tab("AI");
await caption("AI assistance is <b>off by default</b>: your own key, redaction before anything is sent, every call audit-logged", 1800);
await click(page.getByRole("button", { name: /^Notifications/ }), { after: 800 });
await caption("<b>Notifications</b> — deploys, action runs, template builds and AI drafts ring the bell; webhooks tell the outside world", 2000);
await page.keyboard.press("Escape"); await sleep(300);

// ---- 10. finale ---------------------------------------------------------------
await click(page.getByRole("button", { name: "Switch to light mode" }), { after: 300 });
await nav("Dashboard", "text=staging-app-04");
await caption("Light or dark. One container, MIT. Plus a <b>CLI</b> and an <b>MCP server</b> for Claude.<br><b style=\"white-space:nowrap\">github.com/blink-zero/forgemill</b>", 3500);
await captionOff(); await sleep(500);

console.log("page errors:", errors.length ? errors : "none");
await ctx.close();
const video = await page.video()?.path();
await browser.close();
if (video) { const dst = path.join(OUT, "demo.webm"); fs.renameSync(video, dst); console.log("video:", dst); }
process.exit(errors.length ? 1 : 0);
