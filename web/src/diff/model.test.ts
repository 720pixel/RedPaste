import { describe, it, expect } from "vitest";
import { buildDiff } from "./model";
import { diffLines } from "./myers";

describe("diffLines", () => {
  it("reconstructs both sides from the edit script", () => {
    const a = "one\ntwo\nthree";
    const b = "one\ntwo changed\nthree\nfour";
    const edits = diffLines(a, b);
    const leftBack = edits.filter((e) => e.op !== "add").map((e) => e.line).join("\n");
    const rightBack = edits.filter((e) => e.op !== "del").map((e) => e.line).join("\n");
    expect(leftBack).toBe(a);
    expect(rightBack).toBe(b);
  });

  it("is all-equal for identical input", () => {
    const edits = diffLines("a\nb\nc", "a\nb\nc");
    expect(edits.every((e) => e.op === "eq")).toBe(true);
  });
});

describe("buildDiff", () => {
  it("counts added and removed lines", () => {
    const r = buildDiff("a\nb\nc", "a\nB\nc\nd");
    expect(r.removed).toBe(1); // b -> B is a change (counts as one removed + one added)
    expect(r.added).toBe(2); // changed line + appended d
  });

  it("marks identical text as identical", () => {
    const r = buildDiff("same\ntext", "same\ntext");
    expect(r.added).toBe(0);
    expect(r.removed).toBe(0);
    expect(r.rows.every((row) => row.kind === "eq")).toBe(true);
  });

  it("does word-level highlighting on changed lines", () => {
    const r = buildDiff("the quick brown fox", "the slow brown fox");
    const change = r.rows.find((row) => row.kind === "change");
    expect(change).toBeTruthy();
    const changedLeft = change!.left!.segs.filter((s) => s.changed).map((s) => s.text).join("");
    const changedRight = change!.right!.segs.filter((s) => s.changed).map((s) => s.text).join("");
    expect(changedLeft).toContain("quick");
    expect(changedRight).toContain("slow");
    // unchanged words stay unchanged
    expect(change!.left!.segs.some((s) => !s.changed && s.text.includes("brown"))).toBe(true);
  });
});
