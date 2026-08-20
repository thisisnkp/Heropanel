import { computed, ref } from "vue";
import { defineStore } from "pinia";
import type { StackKey } from "@/config/stacks";
import { SEED_SITES } from "@/data/sites";

export interface Site {
  readonly id: number;
  name: string;
  domain: string;
  stackKey: StackKey;
  /** "GitHub" once a repo is connected, "Manual" otherwise. */
  deploy: string;
  status: "live" | "building" | "suspended";
  lastDeploy: string;
  branch: string;
  repo: string;
}

/**
 * The site list and whichever site is currently open.
 *
 * The list is held here rather than fetched per screen because the shell needs
 * it too — the sidebar shows a count, the site switcher searches it, and the
 * drawer needs the current site's stack before any child route renders.
 */
export const useSitesStore = defineStore("sites", () => {
  const sites = ref<Site[]>([]);
  const loading = ref(false);
  const error = ref<string | null>(null);
  const currentId = ref<number | null>(null);
  const loaded = ref(false);

  const current = computed(() => sites.value.find((s) => s.id === currentId.value) ?? null);
  const count = computed(() => sites.value.length);
  const building = computed(() => sites.value.filter((s) => s.status === "building"));

  function setCurrent(id: number | null) {
    currentId.value = id;
  }

  function byId(id: number) {
    return sites.value.find((s) => s.id === id) ?? null;
  }

  /** Replaces the list wholesale — used by the loader and by tests. */
  function hydrate(next: Site[]) {
    sites.value = next;
    loaded.value = true;
  }

  /**
   * Loads the list once per session.
   *
   * Idempotent because the shell, the site layout and the list screen all need
   * the sites and any of the three can mount first — without the guard, opening
   * a deep link would fetch the same list three times. The fixture import is the
   * seam the real `GET /api/v1/sites` replaces; nothing outside this function
   * knows where the data came from.
   */
  async function ensureLoaded() {
    if (loaded.value || loading.value) return;
    loading.value = true;
    error.value = null;
    try {
      hydrate([...SEED_SITES]);
    } catch (e) {
      error.value = e instanceof Error ? e.message : "Could not load your websites.";
    } finally {
      loading.value = false;
    }
  }

  function remove(id: number) {
    sites.value = sites.value.filter((s) => s.id !== id);
    if (currentId.value === id) currentId.value = null;
  }

  return { sites, loading, error, loaded, currentId, current, count, building, setCurrent, byId, hydrate, ensureLoaded, remove };
});
