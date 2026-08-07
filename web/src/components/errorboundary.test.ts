import { describe, expect, it } from "vitest";
import { resetKeysChanged } from "./ErrorBoundary";

describe("resetKeysChanged", () => {
  it("is false for the same reference", () => {
    const a = ["/sites"];
    expect(resetKeysChanged(a, a)).toBe(false);
  });

  it("is false for equal shallow contents", () => {
    expect(resetKeysChanged(["/sites"], ["/sites"])).toBe(false);
    expect(resetKeysChanged([1, "x"], [1, "x"])).toBe(false);
  });

  it("is true when a value changes (route navigation)", () => {
    expect(resetKeysChanged(["/sites"], ["/dns"])).toBe(true);
  });

  it("is true when length changes", () => {
    expect(resetKeysChanged(["/sites"], ["/sites", "extra"])).toBe(true);
  });

  it("treats undefined vs defined as changed", () => {
    expect(resetKeysChanged(undefined, ["/sites"])).toBe(true);
    expect(resetKeysChanged(["/sites"], undefined)).toBe(true);
  });

  it("is false for both undefined", () => {
    expect(resetKeysChanged(undefined, undefined)).toBe(false);
  });
});
