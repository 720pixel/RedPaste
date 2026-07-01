// Turns the line-level edit script into aligned side-by-side rows and adds
// word-level highlighting inside changed lines.

import { diffLines } from "./myers";

export interface Seg {
  text: string;
  changed: boolean;
}

export interface Side {
  num: number;
  segs: Seg[];
}

export type RowKind = "eq" | "change" | "add" | "del";

export interface Row {
  kind: RowKind;
  left?: Side;
  right?: Side;
}

export interface DiffResult {
  rows: Row[];
  added: number;
  removed: number;
}

// what a shared "diff" paste's envelope content decodes to (see routes/Diff.tsx)
export interface DiffPayload {
  a: { name: string; content: string };
  b: { name: string; content: string };
}

export type Precision = "smart" | "word" | "char";

export interface DiffOptions {
  precision?: Precision;
  ignoreWhitespace?: boolean;
}

const normalizeWhitespace = (s: string) => s.trim().replace(/\s+/g, " ");

export function buildDiff(aText: string, bText: string, opts: DiffOptions = {}): DiffResult {
  const precision = opts.precision ?? "smart";
  const keyFn = opts.ignoreWhitespace ? normalizeWhitespace : undefined;
  const edits = diffLines(aText, bText, keyFn);
  const rows: Row[] = [];
  let ln = 0; // left line number
  let rn = 0; // right line number
  let added = 0;
  let removed = 0;

  let i = 0;
  while (i < edits.length) {
    const e = edits[i];
    if (e.op === "eq") {
      ln++;
      rn++;
      rows.push({ kind: "eq", left: plain(ln, e.line), right: plain(rn, e.line) });
      i++;
      continue;
    }
    // gather a contiguous block of dels then adds
    const dels: string[] = [];
    const adds: string[] = [];
    while (i < edits.length && edits[i].op === "del") dels.push(edits[i++].line);
    while (i < edits.length && edits[i].op === "add") adds.push(edits[i++].line);

    const pairs = Math.min(dels.length, adds.length);
    for (let p = 0; p < pairs; p++) {
      ln++;
      rn++;
      const [ls, rs] = wordDiff(dels[p], adds[p], precision);
      rows.push({ kind: "change", left: { num: ln, segs: ls }, right: { num: rn, segs: rs } });
      added++;
      removed++;
    }
    for (let p = pairs; p < dels.length; p++) {
      ln++;
      removed++;
      rows.push({ kind: "del", left: plain(ln, dels[p]) });
    }
    for (let p = pairs; p < adds.length; p++) {
      rn++;
      added++;
      rows.push({ kind: "add", right: plain(rn, adds[p]) });
    }
  }

  return { rows, added, removed };
}

function plain(num: number, text: string): Side {
  return { num, segs: [{ text, changed: false }] };
}

// word-level diff between two changed lines via LCS over tokens
function wordDiff(a: string, b: string, precision: Precision): [Seg[], Seg[]] {
  const at = tokenize(a, precision);
  const bt = tokenize(b, precision);
  const lcs = lcsMatrix(at, bt);

  const left: Seg[] = [];
  const right: Seg[] = [];
  let i = at.length;
  let j = bt.length;
  const lstack: Seg[] = [];
  const rstack: Seg[] = [];
  while (i > 0 && j > 0) {
    if (at[i - 1] === bt[j - 1]) {
      lstack.push({ text: at[i - 1], changed: false });
      rstack.push({ text: bt[j - 1], changed: false });
      i--;
      j--;
    } else if (lcs[i - 1][j] >= lcs[i][j - 1]) {
      lstack.push({ text: at[i - 1], changed: true });
      i--;
    } else {
      rstack.push({ text: bt[j - 1], changed: true });
      j--;
    }
  }
  while (i > 0) lstack.push({ text: at[--i], changed: true });
  while (j > 0) rstack.push({ text: bt[--j], changed: true });
  left.push(...merge(lstack.reverse()));
  right.push(...merge(rstack.reverse()));
  return [left, right];
}

// "char" splits every character, "word" splits only on whitespace boundaries
// (punctuation stays glued to its word), "smart" also splits punctuation out
// on its own so e.g. a changed trailing comma doesn't highlight the whole word.
function tokenize(s: string, precision: Precision): string[] {
  if (precision === "char") return Array.from(s);
  if (precision === "word") return s.match(/\s+|\S+/g) ?? [];
  return s.match(/\s+|\w+|[^\s\w]/g) ?? [];
}

function lcsMatrix(a: string[], b: string[]): number[][] {
  const dp: number[][] = Array.from({ length: a.length + 1 }, () => new Array(b.length + 1).fill(0));
  for (let i = 1; i <= a.length; i++) {
    for (let j = 1; j <= b.length; j++) {
      dp[i][j] = a[i - 1] === b[j - 1] ? dp[i - 1][j - 1] + 1 : Math.max(dp[i - 1][j], dp[i][j - 1]);
    }
  }
  return dp;
}

// collapse adjacent segments of the same changed-ness
function merge(segs: Seg[]): Seg[] {
  const out: Seg[] = [];
  for (const s of segs) {
    const last = out[out.length - 1];
    if (last && last.changed === s.changed) last.text += s.text;
    else out.push({ ...s });
  }
  return out;
}
