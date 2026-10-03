// Pixel-compare two screenshot directories produced by tour.mjs and report the
// percentage of differing pixels per file. Writes a red-on-dimmed diff image
// for any pair above the threshold. Exit 0 = within threshold, 2 = regression.
// Usage: node diff.mjs <baselineDir> <candidateDir> [thresholdPct=0.5]
import fs from "node:fs";
import path from "node:path";
import { PNG } from "pngjs";

const [a, b, thrArg] = process.argv.slice(2);
if (!a || !b) { console.error("usage: diff.mjs <baselineDir> <candidateDir> [thresholdPct]"); process.exit(2); }
const threshold = Number(thrArg ?? 0.5);
const outDir = path.join(b, "_diff");
fs.mkdirSync(outDir, { recursive: true });

const files = fs.readdirSync(a).filter((f) => f.endsWith(".png")).sort();
let worst = 0; const rows = [];
for (const f of files) {
  const pb = path.join(b, f);
  if (!fs.existsSync(pb)) { rows.push([f, "MISSING in candidate"]); worst = 100; continue; }
  const A = PNG.sync.read(fs.readFileSync(path.join(a, f)));
  const B = PNG.sync.read(fs.readFileSync(pb));
  if (A.width !== B.width || A.height !== B.height) { rows.push([f, `SIZE ${A.width}x${A.height} vs ${B.width}x${B.height}`]); worst = 100; continue; }
  const diff = new PNG({ width: A.width, height: A.height });
  let n = 0;
  for (let i = 0; i < A.data.length; i += 4) {
    const d = Math.abs(A.data[i] - B.data[i]) + Math.abs(A.data[i + 1] - B.data[i + 1]) + Math.abs(A.data[i + 2] - B.data[i + 2]);
    const differs = d > 24; // ignore sub-perceptual anti-aliasing noise
    if (differs) n++;
    diff.data[i] = differs ? 255 : A.data[i] >> 2; diff.data[i + 1] = differs ? 0 : A.data[i + 1] >> 2; diff.data[i + 2] = differs ? 0 : A.data[i + 2] >> 2; diff.data[i + 3] = 255;
  }
  const pct = (n / (A.width * A.height)) * 100;
  worst = Math.max(worst, pct);
  rows.push([f, pct.toFixed(2) + "%"]);
  if (pct > threshold) fs.writeFileSync(path.join(outDir, f.replace(".png", ".diff.png")), PNG.sync.write(diff));
}
for (const f of fs.readdirSync(b).filter((f) => f.endsWith(".png"))) if (!files.includes(f)) rows.push([f, "NEW in candidate"]);
for (const [f, r] of rows) console.log(r.padStart(22), " ", f);
console.log(`\nworst: ${worst.toFixed(2)}%  (threshold ${threshold}% — diff images in ${outDir} for anything above)`);
process.exit(worst > threshold ? 2 : 0);
