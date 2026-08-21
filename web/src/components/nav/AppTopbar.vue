<script setup lang="ts">
/**
 * The sticky top bar: breadcrumb, search, help, Ask AI.
 *
 * The breadcrumb is derived from the matched route records rather than passed
 * down, so a screen cannot forget to set it and land the user on a page with no
 * idea where they are.
 */
import { computed } from "vue";
import { useRoute } from "vue-router";
import { useSitesStore } from "@/stores/sites";

const route = useRoute();
const sites = useSitesStore();

const crumb = computed(() => {
  const parts = route.matched
    .map((r) => r.meta.title as string | undefined)
    .filter((t): t is string => Boolean(t));

  const site = sites.current;
  if (site) return ["Sites", site.domain, ...parts].join("  /  ");
  return parts.length ? parts.join("  /  ") : "Home";
});

/** ⌘K on a Mac, Ctrl+K everywhere else. Purely a label. */
const shortcut = computed(() =>
  typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘K" : "Ctrl K",
);

const emit = defineEmits<{ (e: "ask-ai"): void; (e: "search"): void }>();
</script>

<template>
  <header class="nx-topbar">
    <div class="nx-topbar__crumb">{{ crumb }}</div>
    <div class="nx-topbar__spacer" />

    <button type="button" class="nx-topbar__search" @click="emit('search')">
      <NxIcon name="search" size="sm" class="nx-topbar__search-icon" />
      <span class="nx-topbar__search-text">Search sites, files, domains</span>
      <kbd class="nx-topbar__kbd">{{ shortcut }}</kbd>
    </button>

    <RouterLink :to="{ name: 'notifications' }" class="nx-topbar__bell" aria-label="Notifications">
      <NxIcon name="notifications" size="md" />
      <span class="nx-topbar__dot" aria-hidden="true" />
    </RouterLink>

    <NxButton variant="default" size="lg">Help</NxButton>

    <button type="button" class="nx-topbar__ai" @click="emit('ask-ai')">
      <NxIcon name="auto-awesome" size="md" />
      Ask AI
    </button>
  </header>
</template>

<style scoped>
.nx-topbar {
  position: sticky;
  top: 0;
  z-index: 5;
  background: rgba(246, 246, 244, 0.92);
  backdrop-filter: blur(8px);
  border-bottom: 1px solid var(--nx-border);
  padding: 0 32px;
  height: 60px;
  display: flex;
  align-items: center;
  gap: 16px;
}
.nx-topbar__crumb {
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
  font-family: "JetBrains Mono", ui-monospace, monospace;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 340px;
}
.nx-topbar__spacer { flex: 1; }
.nx-topbar__search {
  display: flex;
  align-items: center;
  gap: 8px;
  background: var(--nx-surface);
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-md);
  padding: 8px 12px;
  width: 280px;
  cursor: pointer;
  font-family: inherit;
  text-align: left;
}
.nx-topbar__search:hover { background: var(--nx-hover); }
.nx-topbar__search-icon { color: var(--nx-text-muted); }
.nx-topbar__search-text {
  flex: 1;
  min-width: 0;
  font-size: var(--nx-text-base);
  color: var(--nx-text-placeholder);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.nx-topbar__kbd {
  font-family: "JetBrains Mono", ui-monospace, monospace;
  font-size: var(--nx-text-xs);
  color: var(--nx-text-muted);
  background: var(--nx-hover);
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-sm);
  padding: 0 6px;
  white-space: nowrap;
}
.nx-topbar__bell {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  border-radius: var(--nx-radius-md);
  color: var(--nx-text-2);
}
.nx-topbar__bell:hover { background: var(--nx-hover); color: var(--nx-text); }
.nx-topbar__dot {
  position: absolute;
  top: 7px;
  right: 7px;
  width: 7px;
  height: 7px;
  border-radius: var(--nx-radius-full);
  background: var(--nx-danger);
}
.nx-topbar__ai {
  display: flex;
  align-items: center;
  gap: 8px;
  border: 0;
  background: var(--nx-text);
  color: var(--nx-primary-on);
  border-radius: var(--nx-radius-md);
  padding: 8px 16px;
  font-size: var(--nx-text-base);
  font-family: inherit;
  font-weight: 500;
  cursor: pointer;
  white-space: nowrap;
}
.nx-topbar__ai:hover { background: var(--nx-dark-2); }
</style>
