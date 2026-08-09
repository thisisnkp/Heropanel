import { describe, expect, it } from "vitest";
import { diffLines } from "./diff";

// The differ decides what an operator is told a save will change. If it is
// wrong, they approve a change they did not read — so its invariants (nothing
// invented, nothing dropped, counts that match the lines) are pinned here.

const render = (r: ReturnType<typeof diffLines>) =>
  r.lines.map((l) => `${l.kind === "add" ? "+" : l.kind === "del" ? "-" : " "}${l.text}`);

describe("diffLines", () => {
  it("reports no change for identical text", () => {
    const r = diffLines("a\nb\nc\n", "a\nb\nc\n");
    expect(r.lines).toEqual([]);
    expect(r.added).toBe(0);
    expect(r.removed).toBe(0);
  });

  it("ignores a trailing newline difference in line count", () => {
    // "a\nb" and "a\nb\n" are the same two lines; a phantom empty line would
    // show up as a change on every save from an editor that adds one.
    expect(diffLines("a\nb", "a\nb\n").lines).toEqual([]);
  });

  it("trims the common prefix and suffix so only the change is shown", () => {
    const r = diffLines("h1\nh2\nold\nf1\nf2\n", "h1\nh2\nnew\nf1\nf2\n");
    expect(render(r)).toEqual(["-old", "+new"]);
    expect(r.added).toBe(1);
    expect(r.removed).toBe(1);
  });

  it("numbers lines against the untrimmed original and new text", () => {
    const r = diffLines("h1\nh2\nold\nf1\n", "h1\nh2\nnew\nf1\n");
    const del = r.lines.find((l) => l.kind === "del");
    const add = r.lines.find((l) => l.kind === "add");
    expect(del?.a).toBe(3);
    expect(add?.b).toBe(3);
  });

  it("reports a pure insertion as additions only", () => {
    const r = diffLines("a\nc\n", "a\nb\nc\n");
    expect(render(r)).toEqual(["+b"]);
    expect(r.removed).toBe(0);
    expect(r.added).toBe(1);
  });

  it("reports a pure deletion as removals only", () => {
    const r = diffLines("a\nb\nc\n", "a\nc\n");
    expect(render(r)).toEqual(["-b"]);
    expect(r.added).toBe(0);
    expect(r.removed).toBe(1);
  });

  it("handles emptying a file and filling an empty one", () => {
    const cleared = diffLines("a\nb\n", "");
    expect(cleared.removed).toBe(2);
    expect(cleared.added).toBe(0);

    const filled = diffLines("", "a\nb\n");
    expect(filled.added).toBe(2);
    expect(filled.removed).toBe(0);
  });

  it("keeps counts consistent with the emitted lines", () => {
    const r = diffLines("a\nb\nc\nd\n", "a\nx\nc\ny\nz\n");
    expect(r.lines.filter((l) => l.kind === "add")).toHaveLength(r.added);
    expect(r.lines.filter((l) => l.kind === "del")).toHaveLength(r.removed);
  });

  it("never invents or drops content: kept lines reconstruct both sides", () => {
    const before = "one\ntwo\nthree\nfour\n";
    const after = "one\nTWO\nthree\nfour\nfive\n";
    const r = diffLines(before, after);
    // Context + deletions rebuild the changed window of the original, and
    // context + additions rebuild the new one. Anything else means the differ
    // showed the operator text that is not in either file. The window starts
    // after the shared "one" and runs to the end, because the files no longer
    // agree on their last line.
    const fromA = r.lines.filter((l) => l.kind !== "add").map((l) => l.text);
    const fromB = r.lines.filter((l) => l.kind !== "del").map((l) => l.text);
    expect(fromA).toEqual(["two", "three", "four"]);
    expect(fromB).toEqual(["TWO", "three", "four", "five"]);
  });

  it("gives up precisely, not silently, on a very large change", () => {
    const big = Array.from({ length: 2500 }, (_, i) => `line ${i}`).join("\n");
    const r = diffLines("", big);
    expect(r.tooLarge).toBe(true);
    expect(r.lines).toEqual([]);
    // The counts still have to be usable — "too large to show" must not mean
    // "we cannot tell you how much changed".
    expect(r.added).toBe(2500);
  });
});
