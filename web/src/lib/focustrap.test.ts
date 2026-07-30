import { describe, expect, it } from "vitest";
import { nextFocusIndex } from "./focustrap";

describe("nextFocusIndex", () => {
  it("advances forward and wraps off the last element", () => {
    expect(nextFocusIndex(3, 0, false)).toBe(1);
    expect(nextFocusIndex(3, 1, false)).toBe(2);
    expect(nextFocusIndex(3, 2, false)).toBe(0); // wrap
  });

  it("advances backward and wraps off the first element", () => {
    expect(nextFocusIndex(3, 2, true)).toBe(1);
    expect(nextFocusIndex(3, 1, true)).toBe(0);
    expect(nextFocusIndex(3, 0, true)).toBe(2); // wrap
  });

  it("enters the set from outside (current = -1)", () => {
    expect(nextFocusIndex(3, -1, false)).toBe(0); // first on Tab
    expect(nextFocusIndex(3, -1, true)).toBe(2); // last on Shift+Tab
  });

  it("handles a single focusable by staying put", () => {
    expect(nextFocusIndex(1, 0, false)).toBe(0);
    expect(nextFocusIndex(1, 0, true)).toBe(0);
  });

  it("returns -1 when there is nothing to focus", () => {
    expect(nextFocusIndex(0, -1, false)).toBe(-1);
    expect(nextFocusIndex(0, 5, true)).toBe(-1);
  });
});
