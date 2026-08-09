// A .gitignore matcher covering git's ignore rules as they apply to a working
// tree: comments, blank lines, negation (`!`), directory-only (trailing `/`),
// anchoring (leading `/`), the `*` / `**` / `?` wildcards, **character classes**
// (`[abc]`, `[a-z]`, `[!abc]`), nested ignore files with git's precedence (a
// deeper .gitignore overrides a shallower one), and the two lower-precedence
// sources git also consults: **`.git/info/exclude`** and the **global
// excludesfile** (`core.excludesfile`).
//
// It remains a display hint for the file browser — greying out build output and
// vendor folders — never a security or correctness boundary. What it now matches,
// though, is what git itself would hide, character classes and all.

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
    } else if (c === "[") {
      // A character class, fnmatch-style: [abc], [a-z], [!abc] (negation). It
      // never matches a "/". Scan to the closing "]" (a "]" right after the
      // opening bracket or its negation is a literal member).
      const cls = scanCharClass(p, i);
      if (cls) {
        out += cls.regex;
        i = cls.end;
      } else {
        out += "\\["; // unterminated — treat the bracket as a literal
      }
    } else {
      out += c.replace(/[.+^${}()|[\]\\]/g, "\\$&");
    }
  }

  const prefix = anchored ? "^" : "^(?:.*/)?";
  // Matching a directory also matches everything beneath it.
  return new RegExp(`${prefix}${out}(?:/.*)?$`);
}

// scanCharClass compiles a fnmatch character class starting at p[start] === "[".
// Returns the JS regex fragment (which never matches "/") and the index of the
// closing "]", or null when the class is unterminated.
function scanCharClass(p: string, start: number): { regex: string; end: number } | null {
  let i = start + 1;
  let negate = false;
  if (p[i] === "!" || p[i] === "^") {
    negate = true;
    i++;
  }
  let body = "";
  // A "]" as the very first member is a literal, not the terminator.
  if (p[i] === "]") {
    body += "\\]";
    i++;
  }
  for (; i < p.length; i++) {
    const ch = p[i];
    if (ch === "]") {
      // git never matches "/" with a class: drop it from a positive class (so
      // "[/]" matches nothing) and keep the "^/" guard on a negated one.
      if (negate) {
        return { regex: `[^/${body}]`, end: i };
      }
      if (body === "") {
        return { regex: "(?!)", end: i } // an empty positive class matches nothing
      }
      return { regex: `[${body}]`, end: i };
    }
    if (ch === "/") {
      continue; // a slash is never a class member in git
    }
    if (ch === "\\") {
      body += "\\\\";
    } else if (ch === "^" || ch === "]") {
      body += "\\" + ch;
    } else {
      body += ch; // ranges like a-z pass through unchanged
    }
  }
  return null;
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
export function isIgnoredNested(
  files: IgnoreFile[],
  path: string,
  isDir: boolean,
  extra?: ExtraExcludes,
): boolean {
  let ignored = false;
  // Lowest precedence first: the global excludesfile, then .git/info/exclude,
  // then the .gitignore chain (shallow → deep). Each later source that has an
  // opinion overwrites the one before it — exactly git's ordering.
  if (extra?.global) {
    const v = decide(extra.global, path, isDir);
    if (v !== null) ignored = v;
  }
  if (extra?.infoExclude) {
    const v = decide(extra.infoExclude, path, isDir);
    if (v !== null) ignored = v;
  }
  const ordered = [...files].sort((a, b) => depth(a.dir) - depth(b.dir));
  for (const f of ordered) {
    const rel = relativeToDir(f.dir, path);
    if (rel === null) continue; // this ignore file does not govern the path
    const verdict = decide(f.rules, rel, isDir);
    if (verdict !== null) ignored = verdict;
  }
  return ignored;
}

// ExtraExcludes carries the two lower-precedence ignore sources git also honors,
// both matched relative to the repository root: `.git/info/exclude` and the
// global excludesfile (core.excludesfile). Either may be omitted.
export interface ExtraExcludes {
  infoExclude?: IgnoreRule[];
  global?: IgnoreRule[];
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
