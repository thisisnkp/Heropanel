<script setup lang="ts">
/**
 * The 252px sidebar: brand, then the four navigation groups.
 *
 * Entries with children render a disclosure caret and are not themselves links —
 * clicking one opens the group. That matches the design and avoids the trap
 * where "Domains" both navigates and expands, so a mis-aimed click silently
 * changes the page.
 */
import { computed } from "vue";
import { useRoute } from "vue-router";
import { NAV_GROUPS, type NavEntry } from "@/config/navigation";
import { useSitesStore } from "@/stores/sites";
import { useUiStore } from "@/stores/ui";

const route = useRoute();
const sites = useSitesStore();
const ui = useUiStore();

/** Counts shown on the right of an entry. Only a few carry one. */
const counts = computed<Record<string, string>>(() => ({
  websites: String(sites.count),
}));

function isActive(entry: NavEntry) {
  return route.name === entry.to;
}

/** A parent is "current" when it or any child is the active route. */
function groupActive(entry: NavEntry) {
  return isActive(entry) || (entry.children?.some(isActive) ?? false);
}

function isOpen(entry: NavEntry) {
  return ui.isGroupOpen(entry.to) || groupActive(entry);
}
</script>

<template>
  <aside class="nx-sidebar nxhide">
    <div class="nx-sidebar__brand">
      <div class="nx-sidebar__mark" aria-hidden="true">N</div>
      <div class="nx-sidebar__name">NexPanel</div>
    </div>

    <template v-for="group in NAV_GROUPS" :key="group.id">
      <div :id="`nx-grp-${group.id}`" class="nx-sidebar__caption">{{ group.label }}</div>
      <ul class="nx-sidebar__list" :aria-labelledby="`nx-grp-${group.id}`">
        <li v-for="entry in group.entries" :key="entry.label">
          <!-- Parent with children: a disclosure, not a link. -->
          <button
            v-if="entry.children"
            type="button"
            class="nx-nav-item is-parent"
            :class="{ 'is-current': groupActive(entry) }"
            :aria-expanded="isOpen(entry)"
            @click="ui.toggleGroup(entry.to)"
          >
            <NxIcon :name="entry.icon" size="md" class="nx-nav-item__icon" />
            <span class="nx-nav-item__label">{{ entry.label }}</span>
            <NxIcon :name="isOpen(entry) ? 'arrow-drop-down' : 'arrow-right'" size="md" class="nx-nav-item__caret" />
          </button>

          <RouterLink
            v-else
            :to="{ name: entry.to }"
            class="nx-nav-item"
            :class="{ 'is-current': isActive(entry) }"
            :aria-current="isActive(entry) ? 'page' : undefined"
          >
            <span class="nx-nav-item__bar" aria-hidden="true" />
            <NxIcon :name="entry.icon" size="md" class="nx-nav-item__icon" />
            <span class="nx-nav-item__label">{{ entry.label }}</span>
            <span v-if="counts[entry.to]" class="nx-nav-item__count">{{ counts[entry.to] }}</span>
          </RouterLink>

          <ul v-if="entry.children && isOpen(entry)" class="nx-sidebar__list is-nested">
            <li v-for="child in entry.children" :key="child.label">
              <RouterLink
                :to="{ name: child.to }"
                class="nx-nav-item is-child"
                :class="{ 'is-current': isActive(child) }"
                :aria-current="isActive(child) ? 'page' : undefined"
              >
                <span class="nx-nav-item__bar" aria-hidden="true" />
                <NxIcon :name="child.icon" size="md" class="nx-nav-item__icon" />
                <span class="nx-nav-item__label">{{ child.label }}</span>
              </RouterLink>
            </li>
          </ul>
        </li>
      </ul>
    </template>
  </aside>
</template>

<style scoped>
.nx-sidebar {
  width: 252px;
  flex: 0 0 252px;
  background: var(--nx-surface);
  border-right: 1px solid var(--nx-border);
  display: flex;
  flex-direction: column;
  padding: 20px 16px 16px;
  overflow-y: auto;
  min-height: 0;
  animation: nxSidebar 220ms cubic-bezier(0.16, 1, 0.3, 1) both;
}
.nx-sidebar__brand { display: flex; align-items: center; gap: 12px; padding: 4px 8px 20px; }
.nx-sidebar__mark {
  width: 26px;
  height: 26px;
  flex: 0 0 26px;
  border-radius: var(--nx-radius-md);
  background: var(--nx-violet-900);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--nx-gold-400);
  font-size: var(--nx-text-md);
  font-weight: 600;
  line-height: 1;
}
.nx-sidebar__name {
  font-size: var(--nx-text-md);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
  color: var(--nx-text);
}
.nx-sidebar__caption {
  font-size: var(--nx-text-xs);
  font-weight: 600;
  letter-spacing: var(--nx-ls-caps);
  color: var(--nx-text-muted);
  padding: 12px 8px 8px;
  text-transform: uppercase;
}
.nx-sidebar__list {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.nx-sidebar__list.is-nested { margin-top: 4px; }

.nx-nav-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  text-align: left;
  border: 0;
  cursor: pointer;
  padding: 8px 12px;
  border-radius: var(--nx-radius-md);
  font-size: var(--nx-text-md);
  font-family: inherit;
  background: transparent;
  color: var(--nx-text-3);
  font-weight: 400;
  transition: background 130ms ease, color 130ms ease;
}
.nx-nav-item.is-child { padding-left: 24px; font-size: var(--nx-text-base); }
.nx-nav-item:hover { background: var(--nx-hover); }
.nx-nav-item.is-current {
  background: var(--nx-primary-soft);
  color: var(--nx-primary-text);
  font-weight: 500;
}
.nx-nav-item.is-parent.is-current { background: transparent; color: var(--nx-text); }
.nx-nav-item__bar {
  position: absolute;
  left: 0;
  top: 6px;
  bottom: 6px;
  width: 3px;
  border-radius: var(--nx-radius-pill);
  background: transparent;
}
.nx-nav-item.is-current .nx-nav-item__bar { background: var(--nx-primary); }
.nx-nav-item__icon { color: var(--nx-text-muted); transition: color 130ms ease; }
.nx-nav-item.is-current .nx-nav-item__icon { color: var(--nx-primary-text); }
.nx-nav-item__label { flex: 1; }
.nx-nav-item__count {
  font-size: var(--nx-text-xs);
  color: var(--nx-text-muted);
  font-family: "JetBrains Mono", ui-monospace, monospace;
}
.nx-nav-item__caret { color: var(--nx-text-placeholder); }
</style>
