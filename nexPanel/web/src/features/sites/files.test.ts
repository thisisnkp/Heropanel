import { describe, expect, it } from "vitest";
import { ancestorDirs, baseName, joinPath, parentPath } from "./files";

// Path helpers hold one invariant the whole File Manager depends on: paths are
// site-relative with no leading slash, and "" is the site root. Build an
// absolute path by accident and the server clamps it somewhere surprising, so
// the rules are pinned rather than assumed.

describe("joinPath", () => {
  it("returns a bare name at the site root", () => {
    expect(joinPath("", "index.php")).toBe("index.php");
  });

  it("joins without introducing a leading slash", () => {
    expect(joinPath("public", "index.php")).toBe("public/index.php");
    expect(joinPath("a/b", "c")).toBe("a/b/c");
  });

  it("does not double a separator when the directory already ends in one", () => {
    expect(joinPath("public/", "index.php")).toBe("public/index.php");
  });
});

describe("parentPath", () => {
  it("walks up one level", () => {
    expect(parentPath("a/b/c")).toBe("a/b");
    expect(parentPath("a/b")).toBe("a");
  });

  it("stops at the site root rather than going above it", () => {
    expect(parentPath("a")).toBe("");
    expect(parentPath("")).toBe("");
  });

  it("ignores a trailing slash", () => {
    expect(parentPath("a/b/")).toBe("a");
  });
});

describe("baseName", () => {
  it("returns the final element", () => {
    expect(baseName("a/b/c.txt")).toBe("c.txt");
    expect(baseName("c.txt")).toBe("c.txt");
  });

  it("ignores a trailing slash so folders name themselves", () => {
    expect(baseName("a/b/")).toBe("b");
  });

  it("returns empty for the site root", () => {
    expect(baseName("")).toBe("");
  });
});

describe("ancestorDirs", () => {
  it("lists the site root alone for the root itself", () => {
    expect(ancestorDirs("")).toEqual([""]);
  });

  it("lists every level shallowest-first", () => {
    // This ordering is what gives nested .gitignore files their precedence.
    expect(ancestorDirs("a/b/c")).toEqual(["", "a", "a/b", "a/b/c"]);
  });

  it("tolerates stray separators", () => {
    expect(ancestorDirs("/a//b/")).toEqual(["", "a", "a/b"]);
  });
});
