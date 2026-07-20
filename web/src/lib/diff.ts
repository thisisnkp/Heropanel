// A minimal line differ, used by the editor to show what a save is about to
// change. It is deliberately dependency-free: the whole diff we need is
// "which lines were added or removed", and pulling a diff library in for that
// would cost more than it is worth.
//
// The algorithm is a classic LCS, with two practical guards: the common prefix
// and suffix are trimmed first (most edits touch a small middle), and the
// remaining window is size-capped so a huge file cannot pin the tab building an
// O(n·m) table.

export type DiffKind = "ctx" | "add" | "del";

export interface DiffLine {
  kind: DiffKind;
  text: string;
  /** 1-based line number in the original (absent for additions). */
  a?: number;
  /** 1-based line number in the new text (absent for deletions). */
  b?: number;
}

export interface DiffResult {
  lines: DiffLine[];
  added: number;
  removed: number;
  /** True when the change was too large to diff precisely. */
  tooLarge: boolean;
}

const MAX_WINDOW = 2000; // lines per side in the LCS window

function splitLines(s: string): string[] {
  // An empty file has no lines at all. "".split("\n") yields one empty string,
  // which would make "clear this file" show a phantom added blank line.
  if (s === "") return [];
  // A trailing newline should not read as an extra empty line in the diff.
  const lines = s.split("\n");
  if (lines.length > 1 && lines[lines.length - 1] === "") lines.pop();
  return lines;
}

export function diffLines(before: string, after: string): DiffResult {
  const a = splitLines(before);
  const b = splitLines(after);

  // Trim the common prefix and suffix — they are context, not change.
  let start = 0;
  while (start < a.length && start < b.length && a[start] === b[start]) start++;
  let endA = a.length;
  let endB = b.length;
  while (endA > start && endB > start && a[endA - 1] === b[endB - 1]) {
    endA--;
    endB--;
  }

  const midA = a.slice(start, endA);
  const midB = b.slice(start, endB);

  if (midA.length === 0 && midB.length === 0) {
    return { lines: [], added: 0, removed: 0, tooLarge: false };
  }
  if (midA.length > MAX_WINDOW || midB.length > MAX_WINDOW) {
    return {
      lines: [],
      added: midB.length,
      removed: midA.length,
      tooLarge: true,
    };
  }

  // LCS table over the changed window only.
  const n = midA.length;
  const m = midB.length;
  const table: number[][] = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      table[i][j] = midA[i] === midB[j] ? table[i + 1][j + 1] + 1 : Math.max(table[i + 1][j], table[i][j + 1]);
    }
  }

  const lines: DiffLine[] = [];
  let added = 0;
  let removed = 0;
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (midA[i] === midB[j]) {
      lines.push({ kind: "ctx", text: midA[i], a: start + i + 1, b: start + j + 1 });
      i++;
      j++;
    } else if (table[i + 1][j] >= table[i][j + 1]) {
      lines.push({ kind: "del", text: midA[i], a: start + i + 1 });
      removed++;
      i++;
    } else {
      lines.push({ kind: "add", text: midB[j], b: start + j + 1 });
      added++;
      j++;
    }
  }
  while (i < n) {
    lines.push({ kind: "del", text: midA[i], a: start + i + 1 });
    removed++;
    i++;
  }
  while (j < m) {
    lines.push({ kind: "add", text: midB[j], b: start + j + 1 });
    added++;
    j++;
  }

  return { lines, added, removed, tooLarge: false };
}
