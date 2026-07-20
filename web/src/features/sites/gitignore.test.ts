import { describe, expect, it } from "vitest";
import { isIgnored, isIgnoredNested, parseGitignore, type IgnoreFile } from "./gitignore";

// The matcher is pure logic with no server to catch its mistakes, and it is the
// kind of code that silently drifts: a wrong answer greys out a file the
// operator can still see, so nothing breaks loudly. These pin the behaviour the
// documented subset promises.

const match = (patterns: string, path: string, isDir = false) => isIgnored(parseGitignore(patterns), path, isDir);

describe("parseGitignore", () => {
  it("skips comments, blank lines, and empty patterns", () => {
    const rules = parseGitignore("# a comment\n\n   \nnode_modules\n!\n");
    expect(rules).toHaveLength(1);
  });
});

describe("isIgnored", () => {
  it("matches a bare name at any depth", () => {
    expect(match("node_modules", "node_modules")).toBe(true);
    expect(match("node_modules", "web/node_modules")).toBe(true);
    expect(match("node_modules", "web/node_modules/react/index.js")).toBe(true);
  });

  it("anchors a pattern that contains a slash", () => {
    expect(match("/build", "build")).toBe(true);
    expect(match("build/output", "build/output")).toBe(true);
    // Anchored to the ignore file's own directory, so a nested copy is not hit.
    expect(match("build/output", "web/build/output")).toBe(false);
  });

  it("honours directory-only patterns", () => {
    expect(match("dist/", "dist", true)).toBe(true);
    // A *file* called dist is not a directory, so the rule does not apply.
    expect(match("dist/", "dist", false)).toBe(false);
  });

  it("applies the last matching rule, so negation re-includes", () => {
    const rules = "*.log\n!keep.log";
    expect(match(rules, "error.log")).toBe(true);
    expect(match(rules, "keep.log")).toBe(false);
  });

  it("treats * as within-one-segment and ** as across segments", () => {
    expect(match("*.css", "app.css")).toBe(true);
    expect(match("/*.css", "themes/app.css")).toBe(false);
    expect(match("themes/**/app.css", "themes/dark/deep/app.css")).toBe(true);
    expect(match("themes/**/app.css", "themes/app.css")).toBe(true);
  });

  it("matches ? as exactly one character", () => {
    expect(match("file?.txt", "file1.txt")).toBe(true);
    expect(match("file?.txt", "file12.txt")).toBe(false);
  });

  it("escapes regex metacharacters in literal patterns", () => {
    // Without escaping, "." would match any character and "a+b" would be a
    // quantifier — both would ignore files the operator never named.
    expect(match("a.txt", "axtxt")).toBe(false);
    expect(match("a+b.txt", "a+b.txt")).toBe(true);
  });

  it("returns false when nothing matched", () => {
    expect(match("node_modules", "src/index.ts")).toBe(false);
  });
});

describe("isIgnoredNested", () => {
  const files = (...entries: [string, string][]): IgnoreFile[] =>
    entries.map(([dir, text]) => ({ dir, rules: parseGitignore(text) }));

  it("lets a deeper ignore file override a shallower one", () => {
    const f = files(["", "*.log"], ["logs", "!keep.log"]);
    expect(isIgnoredNested(f, "app.log", false)).toBe(true);
    expect(isIgnoredNested(f, "logs/other.log", false)).toBe(true);
    expect(isIgnoredNested(f, "logs/keep.log", false)).toBe(false);
  });

  it("keeps a parent's verdict when the deeper file says nothing", () => {
    // The regression this guards: treating "no rule matched" as "not ignored"
    // would let an unrelated nested .gitignore silently un-ignore node_modules.
    const f = files(["", "node_modules"], ["web", "*.tmp"]);
    expect(isIgnoredNested(f, "web/node_modules", true)).toBe(true);
  });

  it("ignores files that do not govern the path", () => {
    const f = files(["api", "*.log"]);
    expect(isIgnoredNested(f, "web/app.log", false)).toBe(false);
    expect(isIgnoredNested(f, "api/app.log", false)).toBe(true);
  });

  it("matches a nested file's patterns relative to its own directory", () => {
    // "/dist" in web/.gitignore anchors to web/, not the site root.
    const f = files(["web", "/dist"]);
    expect(isIgnoredNested(f, "web/dist", true)).toBe(true);
    expect(isIgnoredNested(f, "dist", true)).toBe(false);
    expect(isIgnoredNested(f, "web/packages/dist", true)).toBe(false);
  });

  it("applies files shallowest-first regardless of the order given", () => {
    const f = files(["logs", "!keep.log"], ["", "*.log"]);
    expect(isIgnoredNested(f, "logs/keep.log", false)).toBe(false);
  });
});
