/*
  Plain line diff (longest common subsequence) for showing a proposed script
  next to the current one. Scripts are capped at 64 KB, so the quadratic
  table is fine; above a generous line count we give up and the caller shows
  the new script whole.
*/
export type DiffLine = { kind: "same" | "add" | "del"; text: string; oldNo?: number; newNo?: number };

export const DIFF_MAX_LINES = 2500;

export function lineDiff(before: string, after: string): DiffLine[] | null {
  const a = before.replace(/\r\n/g, "\n").replace(/\n$/, "").split("\n");
  const b = after.replace(/\r\n/g, "\n").replace(/\n$/, "").split("\n");
  if (a.length > DIFF_MAX_LINES || b.length > DIFF_MAX_LINES) return null;
  const n = a.length, m = b.length;
  // lcs[i][j] = LCS length of a[i..] and b[j..]
  const lcs: Uint32Array[] = [];
  for (let i = 0; i <= n; i++) lcs.push(new Uint32Array(m + 1));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
    }
  }
  const out: DiffLine[] = [];
  let i = 0, j = 0;
  while (i < n || j < m) {
    if (i < n && j < m && a[i] === b[j]) {
      out.push({ kind: "same", text: a[i], oldNo: i + 1, newNo: j + 1 });
      i++; j++;
    } else if (i < n && (j >= m || lcs[i + 1][j] >= lcs[i][j + 1])) {
      // Removals before additions, as a conventional diff reads.
      out.push({ kind: "del", text: a[i], oldNo: i + 1 });
      i++;
    } else {
      out.push({ kind: "add", text: b[j], newNo: j + 1 });
      j++;
    }
  }
  return out;
}

/** Collapse long unchanged runs so the diff shows context around changes only. */
export function collapseDiff(lines: DiffLine[], context = 3): (DiffLine | { kind: "skip"; count: number })[] {
  const keep = new Array<boolean>(lines.length).fill(false);
  lines.forEach((l, idx) => {
    if (l.kind === "same") return;
    for (let k = Math.max(0, idx - context); k <= Math.min(lines.length - 1, idx + context); k++) keep[k] = true;
  });
  const out: (DiffLine | { kind: "skip"; count: number })[] = [];
  let skipped = 0;
  lines.forEach((l, idx) => {
    if (keep[idx]) {
      if (skipped > 0) { out.push({ kind: "skip", count: skipped }); skipped = 0; }
      out.push(l);
    } else skipped++;
  });
  if (skipped > 0) out.push({ kind: "skip", count: skipped });
  return out;
}
