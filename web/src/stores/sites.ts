import { computed, ref } from "vue";
import { defineStore } from "pinia";
import type { StackKey } from "@/config/stacks";
import { STACK_KEYS } from "@/config/stacks";
import { api, type Site as ApiSite } from "@/lib/api";

/**
 * A site as the screens use it.
 *
 * Keyed by `uid`, not a number. npd issues opaque uids and every route under
 * /sites/{uid} takes one; a client-side numeric id would have to be mapped back
 * before any request could be made, and would not survive a reload of a deep
 * link at all — which is exactly what the standalone file-manager window is.
 */
export interface Site {
  readonly uid: string;
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
 * npd's status vocabulary is about provisioning; the design's is about what the
 * operator sees. "provisioning" and "error" both mean "not serving yet", which
 * the design draws as building — an error still needs the row to exist and to
 * say something, and the site's own screen is where the failure is explained.
 */
function statusFor(s: ApiSite["status"]): Site["status"] {
  switch (s) {
    case "suspended":
    case "disabled":
      return "suspended";
    case "active":
      return "live";
    default:
      return "building";
  }
}

function stackFor(s: ApiSite): StackKey {
  return (STACK_KEYS as readonly string[]).includes(s.stack) ? (s.stack as StackKey) : "static";
}

/**
 * Maps npd's site to the screens' site.
 *
 * `branch`, `repo` and `lastDeploy` are em-dashes here rather than guesses:
 * they come from the git-deployment record, which is a separate endpoint and a
 * separate module. Showing "—" is honest; showing "main" because most sites use
 * main is not.
 */
export function fromApi(s: ApiSite): Site {
  return {
    uid: s.uid,
    name: s.name || s.primary_domain,
    domain: s.primary_domain,
    stackKey: stackFor(s),
    deploy: s.deploy_mode === "git" ? "GitHub" : "Manual",
    status: statusFor(s.status),
    lastDeploy: "—",
    branch: "—",
    repo: "—",
  };
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
  const currentUid = ref<string | null>(null);
  const loaded = ref(false);

  const current = computed(() => sites.value.find((s) => s.uid === currentUid.value) ?? null);
  const count = computed(() => sites.value.length);
  const building = computed(() => sites.value.filter((s) => s.status === "building"));

  function setCurrent(uid: string | null) {
    currentUid.value = uid;
  }

  function byUid(uid: string) {
    return sites.value.find((s) => s.uid === uid) ?? null;
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
   * a deep link would fetch the same list three times.
   */
  async function ensureLoaded() {
    if (loaded.value || loading.value) return;
    await reload();
  }

  /** Fetches the list again, whether or not it has been loaded before. */
  async function reload() {
    loading.value = true;
    error.value = null;
    try {
      const list = await api.get<ApiSite[]>("/sites");
      hydrate(list.map(fromApi));
    } catch (e) {
      error.value = e instanceof Error ? e.message : "Could not load your websites.";
    } finally {
      loading.value = false;
    }
  }

  /**
   * Drops a site from the list.
   *
   * The caller is responsible for having actually deleted it — this only keeps
   * the shell from showing a row for something that is gone while the list is
   * refetched.
   */
  function remove(uid: string) {
    sites.value = sites.value.filter((s) => s.uid !== uid);
    if (currentUid.value === uid) currentUid.value = null;
  }

  /** Adds a freshly created site without waiting for a refetch. */
  function add(site: Site) {
    sites.value = [...sites.value, site];
  }

  return {
    sites,
    loading,
    error,
    loaded,
    currentUid,
    current,
    count,
    building,
    setCurrent,
    byUid,
    hydrate,
    ensureLoaded,
    reload,
    remove,
    add,
  };
});
