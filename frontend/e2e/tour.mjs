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
import { mockHypervisor, mockAI, NFS_PROMPT } from "./fixtures.mjs";

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

// Hypervisor-backed calls are answered by in-browser fixtures (fixtures.mjs);
// everything else hits the real API.

async function run(theme) {
  const ctx = await browser.newContext({ viewport: { width: 1360, height: 860 }, deviceScaleFactor: 1 });
  const page = await ctx.newPage();
  const errors = []; page.on("pageerror", (e) => errors.push(e.message));
  const consoleMsgs = new Set(); page.on("console", (m) => { if (["warning", "error"].includes(m.type())) consoleMsgs.add(m.type() + ": " + m.text().slice(0, 160)); });
  if (FIXED_TIME) await page.clock.setFixedTime(new Date(FIXED_TIME));
  // Theme is a class on <html> persisted in localStorage; main.tsx applies it on boot.
  await page.addInitScript((t) => { localStorage.setItem("forgemill_theme", t); }, theme);
  await mockHypervisor(page, FIXED_TIME);
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
  await go("/actions", "text=Security Hardening"); await shot("11-actions");
  const scriptToggle = page.getByRole("button", { name: /^Script$/ }).first();
  if (await scriptToggle.count()) { await scriptToggle.click(); await sleep(500); await shot("11a-actions-script-preview"); await scriptToggle.click(); }
  await sleep(4000);
  // Action editor: the Check panel (lint only — AI is off in the seed).
  const newAction = page.getByRole("button", { name: /Create Action/ }).first();
  if (await newAction.count()) {
    await newAction.click(); await sleep(500);
    await page.getByPlaceholder("Install Nginx").fill("Install nginx");
    // The script input is the labelled code editor on head; the base build
    // may still have a bare textarea, so fall back to the first one.
    const scriptInput = (await page.getByLabel("Script", { exact: true }).count()) ? page.getByLabel("Script", { exact: true }) : page.locator("textarea").first();
    await scriptInput.fill("#!/bin/bash\napt-get update\napt-get install nginx\nDB_PASSWORD=hunter2\nrm -rf \"$TARGET_DIR\"/*\ncurl -fsSL https://get.docker.com | sh\n");
    const check = page.getByRole("button", { name: /Check script|Check with AI/ });
    if (await check.count()) { await check.click(); await page.waitForSelector("text=Script check", { timeout: 10000 }).catch(() => {}); await sleep(600); await shot("11b-actions-check", true); }
    // Fix the findings that have a deterministic fix: proposal shown as a diff, then applied.
    const selectFixable = page.getByRole("button", { name: /Select all fixable/ });
    if (await selectFixable.count()) {
      await selectFixable.click();
      await page.getByTestId("action-fix-selected").click();
      await page.waitForSelector('[data-testid="action-fix-panel"]', { timeout: 10000 }).catch(() => {});
      await sleep(500); await shot("11b2-actions-fix-proposal", true);
      const apply = page.getByTestId("action-fix-apply");
      if (await apply.count()) { await apply.click(); await sleep(600); await scriptInput.scrollIntoViewIfNeeded(); await sleep(200); await shot("11b3-actions-fix-applied"); }
    }
    const cancel = page.getByRole("button", { name: /^Cancel$/ }).first();
    if (await cancel.count()) await cancel.click();
    await sleep(2000);
  }
  // Same editor with AI assistance mocked on: the Draft panel and a drafted action with its review.
  const unmockAI = await mockAI(page);
  await go("/actions", "text=Security Hardening");
  const newAction2 = page.getByRole("button", { name: /Create Action/ }).first();
  if (await newAction2.count()) {
    await newAction2.click(); await sleep(500);
    const draftBtn = page.getByRole("button", { name: /Draft with AI/ });
    if (await draftBtn.count()) {
      await draftBtn.click(); await sleep(400);
      await page.getByPlaceholder(/Mount an NFS export/).fill(NFS_PROMPT);
      await page.getByRole("button", { name: /Generate draft/ }).click();
      await page.waitForSelector("text=Review draft", { timeout: 10000 }).catch(() => {});
      await sleep(600); await shot("11c-actions-draft", true);
      await page.getByRole("button", { name: /^Cancel$/ }).first().click(); await sleep(400);
      // Drafts filter on the list.
      const draftsChip = page.getByRole("button", { name: /^Drafts/ });
      if (await draftsChip.count()) { await draftsChip.click(); await sleep(500); await shot("11d-actions-drafts"); await draftsChip.click(); }
    }
    const cancel2 = page.getByRole("button", { name: /^Cancel$/ }).first();
    if (await cancel2.count()) await cancel2.click();
    await sleep(2000);
  }
  await unmockAI();
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
  await sleep(4000);
  if (await goSoft("/settings?tab=credentials", "text=Export VM logins")) await shot("13f-settings-credentials", true);
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
