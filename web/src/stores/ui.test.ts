import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";

import { useUiStore } from "./ui";

describe("ui store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  describe("toasts", () => {
    it("expires on its own", () => {
      const ui = useUiStore();
      ui.toast("Saved.", "success");
      expect(ui.toasts).toHaveLength(1);

      vi.advanceTimersByTime(4500);
      expect(ui.toasts).toHaveLength(0);
    });

    it("runs the undo and clears the toast with it", () => {
      const ui = useUiStore();
      const run = vi.fn();
      const id = ui.toast("2 items moved to trash.", "success", { label: "Undo", run });

      ui.act(id);
      expect(run).toHaveBeenCalledTimes(1);
      // Cleared in the same step, so Undo cannot be pressed twice and restore
      // the same file two times over.
      expect(ui.toasts).toHaveLength(0);

      ui.act(id);
      expect(run).toHaveBeenCalledTimes(1);
    });

    it("does nothing when a toast carries no offer", () => {
      const ui = useUiStore();
      const id = ui.toast("Nothing to undo here.");
      ui.act(id);
      expect(ui.toasts).toHaveLength(1);
    });
  });

  describe("disclosure groups", () => {
    it("remembers what was opened, so navigating inside a group keeps it open", () => {
      const ui = useUiStore();
      expect(ui.isGroupOpen("site:adv")).toBe(false);

      ui.toggleGroup("site:adv");
      expect(ui.isGroupOpen("site:adv")).toBe(true);

      ui.toggleGroup("site:adv");
      expect(ui.isGroupOpen("site:adv")).toBe(false);
    });
  });
});
