// Focus-trap arithmetic, kept pure so it can be reasoned about and unit-tested
// without a DOM. A dialog that traps focus needs exactly one decision on each
// Tab: given how many focusable elements it holds and which one has focus now,
// what should receive focus next — wrapping at the edges so focus never leaves.
//
// The DOM half (querying focusables, calling .focus()) lives in the Modal; this
// is only the index math, which is where the off-by-one and wrap-around bugs
// actually hide.

// The selector for elements that can hold keyboard focus inside a dialog. Used
// by the Modal to collect its focusables; exported so the rule lives in one
// place next to the arithmetic that consumes it.
export const FOCUSABLE_SELECTOR = [
  "a[href]",
  "button:not([disabled])",
  "input:not([disabled])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  '[tabindex]:not([tabindex="-1"])',
].join(",");

// nextFocusIndex returns the index that should receive focus after a Tab press.
// count is how many focusables exist; current is the focused one's index (or -1
// when focus is outside the set); shift is true for Shift+Tab (backwards). Focus
// wraps: Tab off the last element returns to the first, Shift+Tab off the first
// goes to the last. With no focusables it returns -1 (nothing to focus).
export function nextFocusIndex(count: number, current: number, shift: boolean): number {
  if (count <= 0) return -1;
  if (current < 0) return shift ? count - 1 : 0;
  if (shift) return current === 0 ? count - 1 : current - 1;
  return current === count - 1 ? 0 : current + 1;
}
