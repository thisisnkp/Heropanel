<script setup lang="ts">
/**
 * ⌘K search over sites, domains and screens.
 *
 * The design draws a search field in the top bar and binds ⌘K to focusing it;
 * what it does once focused was never specified, because a mockup does not have
 * to answer that. Shipping the field as decoration is the worse option — a
 * control that looks like search and does nothing is a bug report waiting to be
 * filed — so it searches what this build actually knows: the site list, the
 * zones, and every screen the router can name.
 *
 * Screens come from `router.getRoutes()` rather than a hand-written index, so a
 * route added tomorrow is findable without anyone remembering to list it here.
 */
import { computed, nextTick, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { DNS_DOMAINS } from "@/data/dns";
import { useSitesStore } from "@/stores/sites";
import { useUiStore } from "@/stores/ui";

interface Hit {
  readonly id: string;
  readonly kind: "Website" | "Domain" | "Screen";
  readonly label: string;
  readonly sub: string;
  readonly icon: string;
  readonly go: () => void;
}

const router = useRouter();
const sites = useSitesStore();
const ui = useUiStore();

const query = ref("");
const active = ref(0);
const input = ref<HTMLInputElement | null>(null);

/**
 * Screens worth offering: the ones with a title and no parameters. A route that
 * needs an :id cannot be navigated to from a name alone, and offering it would
 * produce a link that throws.
 */
const screens = computed<Hit[]>(() =>
  router
    .getRoutes()
    .filter((r) => typeof r.name === "string" && r.meta?.title && !r.path.includes(":"))
    .map((r) => ({
      id: "screen:" + String(r.name),
      kind: "Screen" as const,
      label: String(r.meta.title),
      sub: r.path,
      icon: "chevron-right",
      go: () => router.push({ name: r.name as string }),
    })),
);

const all = computed<Hit[]>(() => [
  ...sites.sites.map((s) => ({
    id: "site:" + s.id,
    kind: "Website" as const,
    label: s.domain,
    sub: s.status === "live" ? "Live · " + s.deploy : "Building",
    icon: "language",
    go: () => router.push({ name: "site-overview", params: { id: String(s.id) } }),
  })),
  ...DNS_DOMAINS.map((d) => ({
    id: "zone:" + d,
    kind: "Domain" as const,
    label: d,
    sub: "DNS zone",
    icon: "dns",
    go: () => router.push({ name: "dns", query: { domain: d, section: "dns" } }),
  })),
  ...screens.value,
]);

const hits = computed(() => {
  const q = query.value.trim().toLowerCase();
  if (!q) return all.value.slice(0, 8);
  return all.value.filter((h) => h.label.toLowerCase().includes(q)).slice(0, 12);
});

watch(hits, () => (active.value = 0));

watch(
  () => ui.searchOpen,
  async (open) => {
    if (!open) return;
    query.value = "";
    active.value = 0;
    await nextTick();
    input.value?.focus();
  },
);

function choose(hit: Hit | undefined) {
  if (!hit) return;
  ui.searchOpen = false;
  void hit.go();
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === "Escape") {
    ui.searchOpen = false;
    return;
  }
  if (e.key === "ArrowDown") {
    e.preventDefault();
    active.value = Math.min(active.value + 1, hits.value.length - 1);
  } else if (e.key === "ArrowUp") {
    e.preventDefault();
    active.value = Math.max(active.value - 1, 0);
  } else if (e.key === "Enter") {
    e.preventDefault();
    choose(hits.value[active.value]);
  }
}
</script>

<template>
  <Teleport to="body">
    <Transition name="pal">
      <div v-if="ui.searchOpen" class="pal__scrim" @click.self="ui.searchOpen = false">
        <div class="pal" role="dialog" aria-modal="true" aria-label="Search">
          <div class="pal__field">
            <NxIcon name="search" size="md" class="pal__icon" />
            <input
              ref="input"
              v-model="query"
              type="text"
              placeholder="Search sites, files, domains"
              aria-label="Search sites, files, domains"
              @keydown="onKeydown"
            />
            <kbd class="pal__esc">esc</kbd>
          </div>

          <ul v-if="hits.length" class="pal__list">
            <li v-for="(h, i) in hits" :key="h.id">
              <button
                type="button"
                class="pal__hit"
                :class="{ 'is-active': i === active }"
                @click="choose(h)"
                @mouseenter="active = i"
              >
                <NxIcon :name="h.icon" size="md" class="pal__hit-icon" />
                <span class="pal__hit-text">
                  <span class="pal__hit-label">{{ h.label }}</span>
                  <span class="pal__hit-sub">{{ h.sub }}</span>
                </span>
                <span class="pal__kind">{{ h.kind }}</span>
              </button>
            </li>
          </ul>

          <p v-else class="pal__empty">Nothing matches “{{ query }}”.</p>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.pal__scrim {
  position: fixed;
  inset: 0;
  z-index: 70;
  background: rgba(27, 27, 25, 0.32);
  display: flex;
  justify-content: center;
  align-items: flex-start;
  padding: 12vh 16px 16px;
}
.pal {
  width: 100%;
  max-width: 560px;
  background: var(--nx-surface);
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-lg);
  box-shadow: 0 24px 60px rgba(27, 27, 25, 0.24);
  overflow: hidden;
}
.pal__field {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
  border-bottom: 1px solid var(--nx-border);
}
.pal__icon { color: var(--nx-text-muted); }
.pal__field input {
  flex: 1;
  min-width: 0;
  border: 0;
  outline: 0;
  background: transparent;
  font-family: inherit;
  font-size: var(--nx-text-md);
  color: var(--nx-text);
}
.pal__esc {
  font-family: "JetBrains Mono", ui-monospace, monospace;
  font-size: var(--nx-text-xs);
  color: var(--nx-text-muted);
  background: var(--nx-hover);
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-sm);
  padding: 0 6px;
}
.pal__list { margin: 0; padding: 8px; list-style: none; max-height: 52vh; overflow-y: auto; }
.pal__hit {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  text-align: left;
  border: 0;
  background: transparent;
  border-radius: var(--nx-radius-md);
  padding: 10px 12px;
  cursor: pointer;
  font-family: inherit;
  color: var(--nx-text);
}
.pal__hit.is-active { background: var(--nx-hover); }
.pal__hit-icon { color: var(--nx-text-muted); }
.pal__hit-text { flex: 1; min-width: 0; }
.pal__hit-label {
  display: block;
  font-size: var(--nx-text-base);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pal__hit-sub {
  display: block;
  font-size: var(--nx-text-xs);
  color: var(--nx-text-muted);
  padding-top: 2px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pal__kind {
  font-size: var(--nx-text-xs);
  color: var(--nx-text-muted);
  background: var(--nx-hover);
  border-radius: var(--nx-radius-sm);
  padding: 2px 8px;
  white-space: nowrap;
}
.pal__empty { margin: 0; padding: 24px 16px; font-size: var(--nx-text-base); color: var(--nx-text-muted); }

.pal-enter-active,
.pal-leave-active { transition: opacity 160ms ease; }
.pal-enter-from,
.pal-leave-to { opacity: 0; }
</style>
