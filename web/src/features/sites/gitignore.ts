// A .gitignore matcher covering the patterns that actually appear in a web
// project's ignore file: comments, blank lines, negation (`!`), directory-only
// (trailing `/`), anchoring (leading `/`), the `*` / `**` / `?` wildcards, and
// nested ignore files with git's precedence (a deeper .gitignore overrides a
// shallower one).
//
// It is deliberately *not* a full reimplementation of git's ignore rules — no
// character classes, no `.git/info/exclude`, no global excludesfile. It exists
// so the file browser can grey out build output and vendor folders, which is a
// display hint, never a security or correctness boundary. Anything it gets wrong
// shows a file that git would hide; nothing more.

export interface IgnoreRule {
  re: RegExp;
  negate: boolean;
  dirOnly: boolean;
}

// patternToRegExp compiles one gitignore line into an anchored expression that
// is matched against a path relative to the ignore file's directory.
function patternToRegExp(pattern: string): RegExp {
  let p = pattern;
  // A pattern containing a slash anywhere but the end is anchored to this
  // directory; one without is matched against the basename at any depth.
  const anchored = p.includes("/") && !/^[^/]+\/$/.test(p);
  if (p.startsWith("/")) p = p.slice(1);

  let out = "";
  for (let i = 0; i < p.length; i++) {
    const c = p[i];
    if (c === "*") {
      if (p[i + 1] === "*") {
        // `**/` crosses directory boundaries (including none at all).
        if (p[i + 2] === "/") {
          out += "(?:.*/)?";
          i += 2;
        } else {
          out += ".*";
          i += 1;
        }
      } else {
        out += "[^/]*";
      }
    } else if (c === "?") {
      out += "[^/]";
    } else {
      out += c.replace(/[.+^${}()|[\]\\]/g, "\\$&");
    }
  }

  const prefix = anchored ? "^" : "^(?:.*/)?";
  // Matching a directory also matches everything beneath it.
  return new RegExp(`${prefix}${out}(?:/.*)?$`);
}

// parseGitignore compiles an ignore file's contents into ordered rules.
export function parseGitignore(text: string): IgnoreRule[] {
  const rules: IgnoreRule[] = [];
  for (const rawLine of text.split("\n")) {
    let line = rawLine.replace(/\r$/, "").trim();
    if (!line || line.startsWith("#")) continue;

    const negate = line.startsWith("!");
    if (negate) line = line.slice(1);

    const dirOnly = line.endsWith("/");
    if (dirOnly) line = line.slice(0, -1);
    if (!line) continue;

    try {
      rules.push({ re: patternToRegExp(line), negate, dirOnly });
    } catch {
      // A pattern we cannot compile is skipped rather than breaking the listing.
    }
  }
  return rules;
}

// decide returns whether one ignore file's rules ignore relPath, or null when
// none of them matched at all.
//
// The three-way answer is what makes nesting work: "no rule matched" must leave
// a parent .gitignore's decision standing, whereas a plain false would silently
// re-include everything a parent had excluded. Within one file git's own
// precedence applies — the last matching rule wins, so a later `!pattern`
// re-includes something an earlier rule excluded.
function decide(rules: IgnoreRule[], relPath: string, isDir: boolean): boolean | null {
  let result: boolean | null = null;
  for (const r of rules) {
    if (r.dirOnly && !isDir) continue;
    if (r.re.test(relPath)) result = !r.negate;
  }
  return result;
}

// isIgnored reports whether relPath (relative to the ignore file's directory) is
// ignored by a single ignore file.
export function isIgnored(rules: IgnoreRule[], relPath: string, isDir: boolean): boolean {
  return decide(rules, relPath, isDir) ?? false;
}

// IgnoreFile is one parsed .gitignore together with the directory it governs
// (site-relative; "" is the site root).
export interface IgnoreFile {
  dir: string;
  rules: IgnoreRule[];
}

// isIgnoredNested applies a chain of ignore files with git's precedence: each
// file governs its own directory and everything below it, patterns are matched
// relative to that directory, and the deepest file that has something to say
// wins. A `!node_modules/keep-me` in a subdirectory therefore beats a
// `node_modules/` at the root, which is exactly what git does and what an
// operator staring at a greyed-out file expects.
export function isIgnoredNested(files: IgnoreFile[], path: string, isDir: boolean): boolean {
  let ignored = false;
  // Shallowest first, so each deeper file's answer overwrites the one above it.
  const ordered = [...files].sort((a, b) => depth(a.dir) - depth(b.dir));
  for (const f of ordered) {
    const rel = relativeToDir(f.dir, path);
    if (rel === null) continue; // this ignore file does not govern the path
    const verdict = decide(f.rules, rel, isDir);
    if (verdict !== null) ignored = verdict;
  }
  return ignored;
}

// depth counts a directory's nesting level. The site root is 0 — splitting ""
// on "/" yields one empty segment, which would tie it with every top-level
// directory and leave the ordering above to chance.
function depth(dir: string): number {
  return dir === "" ? 0 : dir.split("/").length;
}

// relativeToDir returns path expressed relative to dir, or null when path does
// not sit under dir.
function relativeToDir(dir: string, path: string): string | null {
  if (dir === "") return path;
  if (path === dir) return "";
  return path.startsWith(dir + "/") ? path.slice(dir.length + 1) : null;
}
