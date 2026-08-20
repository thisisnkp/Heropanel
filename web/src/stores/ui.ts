import { ref } from "vue";
import { defineStore } from "pinia";

export interface Toast {
  readonly id: number;
  readonly text: string;
  readonly tone: "success" | "danger" | "info";
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
  const siteSwitcherOpen = ref(false);
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

  function toast(text: string, tone: Toast["tone"] = "info") {
    const t = { id: nextToastId++, text, tone };
    toasts.value.push(t);
    // Long enough to read a sentence, short enough not to stack up during a
    // burst of job events.
    setTimeout(() => dismiss(t.id), 4500);
    return t.id;
  }

  function dismiss(id: number) {
    toasts.value = toasts.value.filter((t) => t.id !== id);
  }

  return {
    openGroups, mobileNavOpen, siteSwitcherOpen, toasts,
    toggleGroup, isGroupOpen, openGroup, toast, dismiss,
  };
});
