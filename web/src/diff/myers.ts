// Myers O(ND) diff over lines. Produces an edit script we turn into side-by-side
// rows. Runs entirely client-side; the diff checker never sends text anywhere.

export type Op = "eq" | "del" | "add";

export interface Edit {
  op: Op;
  line: string;
}

// Classic Myers greedy LCS. `a` -> `b`. `keyFn` controls line equality (e.g. to
// ignore whitespace-only changes) while the returned edits keep the original text.
export function diffLines(aText: string, bText: string, keyFn: (line: string) => string = (l) => l): Edit[] {
  const a = aText.split("\n");
  const b = bText.split("\n");
  return diffArrays(a.map(keyFn), b.map(keyFn)).map((e) => ({ op: e.op, line: e.op === "add" ? b[e.bi] : a[e.ai] }));
}

interface RawEdit {
  op: Op;
  ai: number;
  bi: number;
}

function diffArrays(a: string[], b: string[]): RawEdit[] {
  const n = a.length;
  const m = b.length;
  const max = n + m;
  const v: Record<number, number> = { 1: 0 };
  const trace: Record<number, number>[] = [];

  let reachedD = -1;
  outer: for (let d = 0; d <= max; d++) {
    trace.push({ ...v });
    for (let k = -d; k <= d; k += 2) {
      let x: number;
      if (k === -d || (k !== d && (v[k - 1] ?? -1) < (v[k + 1] ?? -1))) {
        x = v[k + 1] ?? 0;
      } else {
        x = (v[k - 1] ?? 0) + 1;
      }
      let y = x - k;
      while (x < n && y < m && a[x] === b[y]) {
        x++;
        y++;
      }
      v[k] = x;
      if (x >= n && y >= m) {
        reachedD = d;
        break outer;
      }
    }
  }

  // backtrack
  const edits: RawEdit[] = [];
  let x = n;
  let y = m;
  for (let d = reachedD; d > 0; d--) {
    const vv = trace[d];
    const k = x - y;
    let prevK: number;
    if (k === -d || (k !== d && (vv[k - 1] ?? -1) < (vv[k + 1] ?? -1))) {
      prevK = k + 1;
    } else {
      prevK = k - 1;
    }
    const prevX = vv[prevK] ?? 0;
    const prevY = prevX - prevK;
    while (x > prevX && y > prevY) {
      edits.push({ op: "eq", ai: x - 1, bi: y - 1 });
      x--;
      y--;
    }
    if (d > 0) {
      if (x === prevX) {
        edits.push({ op: "add", ai: x, bi: y - 1 });
      } else {
        edits.push({ op: "del", ai: x - 1, bi: y });
      }
    }
    x = prevX;
    y = prevY;
  }
  while (x > 0 && y > 0) {
    edits.push({ op: "eq", ai: x - 1, bi: y - 1 });
    x--;
    y--;
  }
  while (x > 0) {
    edits.push({ op: "del", ai: --x, bi: y });
  }
  while (y > 0) {
    edits.push({ op: "add", ai: x, bi: --y });
  }

  return edits.reverse();
}
