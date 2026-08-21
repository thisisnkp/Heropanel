import { ref } from "vue";
import { defineStore } from "pinia";

export interface ToastAction {
  readonly label: string;
  readonly run: () => void;
}

export interface Toast {
  readonly id: number;
  readonly text: string;
  readonly tone: "success" | "danger" | "info";
  /**
   * An offer to take it back, as the design draws on its toasts.
   *
   * This is the difference between a message and an escape hatch: "Moved to
   * trash" tells you what happened, "Moved to trash · Undo" means you do not
   * have to go and find the file again. Only worth attaching where the action
   * really is reversible — a fake Undo is worse than none.
   */
  readonly action?: ToastAction;
}

/**
 * Chrome state that outlives a route change: which disclosure groups are open,
 * whether the mobile drawer is showing, and the toast queue.
 *
 * Kept out of the components because two shells and the site drawer all read it,
 * and because a group left open should stay open when you navigate between the
 * screens inside it.
 */
export const useUiStore = defineStore("ui", () => {
  const openGroups = ref<Record<string, boolean>>({});
  const mobileNavOpen = ref(false);
  const searchOpen = ref(false);
  const toasts = ref<Toast[]>([]);

  let nextToastId = 1;

  function toggleGroup(id: string) {
    openGroups.value[id] = !openGroups.value[id];
  }

  function isGroupOpen(id: string) {
    return openGroups.value[id] === true;
  }

  function openGroup(id: string) {
    openGroups.value[id] = true;
  }

  function toast(text: string, tone: Toast["tone"] = "info", action?: ToastAction) {
    const t: Toast = { id: nextToastId++, text, tone, action };
    toasts.value.push(t);
    // Long enough to read a sentence, short enough not to stack up during a
    // burst of job events.
    setTimeout(() => dismiss(t.id), 4500);
    return t.id;
  }

  function dismiss(id: number) {
    toasts.value = toasts.value.filter((t) => t.id !== id);
  }

  /** Runs the offer and clears the toast, so Undo cannot be pressed twice. */
  function act(id: number) {
    const t = toasts.value.find((x) => x.id === id);
    if (!t?.action) return;
    t.action.run();
    dismiss(id);
  }

  return {
    openGroups, mobileNavOpen, searchOpen, toasts,
    toggleGroup, isGroupOpen, openGroup, toast, dismiss, act,
  };
});
