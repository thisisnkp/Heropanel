import { describe, expect, it } from "vitest";
import { parseEnv, parseMounts, parsePorts } from "./docker";

// The create form collects free text and turns it into a typed spec. These
// parsers are where a typo becomes a wrong container, so they are pinned here
// rather than discovered by an operator.

describe("parseEnv", () => {
  it("keeps everything after the first equals sign", () => {
    // A connection string is full of "=" — splitting on every one would truncate
    // the value at the first parameter and produce a container that cannot
    // connect, with nothing to indicate why.
    const env = parseEnv("DSN=postgres://u:p@h/db?sslmode=require&x=1");
    expect(env.DSN).toBe("postgres://u:p@h/db?sslmode=require&x=1");
  });

  it("skips blank lines, comments, and lines with no assignment", () => {
    const env = parseEnv("A=1\n\n# a comment\nJUST_A_WORD\n  B=2  ");
    // The line is trimmed before the split, so a value never carries stray
    // trailing whitespace from indentation.
    expect(env).toEqual({ A: "1", B: "2" });
  });

  it("does not invent a variable from a leading equals sign", () => {
    expect(parseEnv("=orphan")).toEqual({});
  });

  it("preserves whitespace inside a value", () => {
    expect(parseEnv("GREETING=hello world").GREETING).toBe("hello world");
  });
});

describe("parsePorts", () => {
  it("reads host:container and defaults to tcp", () => {
    expect(parsePorts("2368:2368")).toEqual([{ host: 2368, container: 2368, proto: "tcp" }]);
  });

  it("reads an explicit protocol", () => {
    expect(parsePorts("5353:53/udp")).toEqual([{ host: 5353, container: 53, proto: "udp" }]);
  });

  it("treats a bare port as the same port on both sides", () => {
    expect(parsePorts("8080")).toEqual([{ host: 8080, container: 8080, proto: "tcp" }]);
  });

  it("skips anything that is not a number rather than sending NaN", () => {
    // NaN would serialise as null and the server would reject the whole request
    // with a message about a port the operator never typed.
    expect(parsePorts("abc:80\n\n90:xyz")).toEqual([]);
  });

  it("reads several lines", () => {
    expect(parsePorts("80:80\n443:443")).toHaveLength(2);
  });
});

describe("parseMounts", () => {
  it("reads volume:path", () => {
    expect(parseMounts("ghost-content:/var/lib/ghost/content")).toEqual([
      { volume: "ghost-content", path: "/var/lib/ghost/content", read_only: false },
    ]);
  });

  it("marks read-only mounts", () => {
    expect(parseMounts("conf:/etc/app:ro")[0].read_only).toBe(true);
  });

  it("skips a line with no path", () => {
    expect(parseMounts("just-a-name")).toEqual([]);
  });

  // A host path typed here is not silently repaired into something else: it is
  // passed through so the server refuses it with a message that names the rule.
  // Quietly rewriting it would teach the operator that host mounts work.
  it("passes a host path through for the server to refuse", () => {
    const mounts = parseMounts("/var/run/docker.sock:/sock");
    expect(mounts).toHaveLength(1);
    expect(mounts[0].volume).toBe("/var/run/docker.sock");
  });
});
