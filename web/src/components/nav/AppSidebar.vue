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
import { MAILBOXES } from "@/data/mail";
import { securityIssues } from "@/data/securitySpec";
import { useAccountStore } from "@/stores/account";
import { useFlagsStore } from "@/stores/flags";
import { useSitesStore } from "@/stores/sites";
import { useUiStore } from "@/stores/ui";

const route = useRoute();
const sites = useSitesStore();
const flags = useFlagsStore();
const account = useAccountStore();
const ui = useUiStore();

/**
 * Counts shown on the right of an entry. Only a few carry one, and each is
 * derived from the thing it counts — a sidebar that says "1" beside Security
 * while the security screen shows nothing wrong is worse than a bare label.
 * Zero is rendered as no count at all rather than as "0".
 */
const counts = computed<Record<string, string>>(() => {
  const critical = securityIssues(flags).filter((i) => i.severity === "critical").length;
  return {
    websites: String(sites.count),
    mail: String(MAILBOXES.length),
    ...(critical ? { security: String(critical) } : {}),
  };
});

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
            <span v-if="entry.badge" class="nx-nav-item__badge">{{ entry.badge }}</span>
            <span v-else-if="counts[entry.to]" class="nx-nav-item__count">{{ counts[entry.to] }}</span>
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

    <div class="nx-sidebar__spacer" />

    <!-- Who is signed in and on what plan. The plan is here rather than only on
         the billing screen because it decides which entries above do anything:
         paid apps and extra seats are a plan question, not a permission one. -->
    <RouterLink :to="{ name: 'billing' }" class="nx-sidebar__user">
      <span class="nx-sidebar__avatar" aria-hidden="true">{{ account.initials }}</span>
      <span class="nx-sidebar__user-text">
        <span class="nx-sidebar__user-name">{{ account.name }}</span>
        <span class="nx-sidebar__plan">{{ account.plan }} plan</span>
      </span>
    </RouterLink>
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
.nx-sidebar__spacer { flex: 1; min-height: 16px; }
.nx-sidebar__user {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px;
  border-top: 1px solid var(--nx-active);
  border-radius: 0;
  color: inherit;
}
.nx-sidebar__user:hover { background: var(--nx-hover); }
.nx-sidebar__avatar {
  width: 28px;
  height: 28px;
  flex: 0 0 28px;
  border-radius: var(--nx-radius-full);
  background: var(--nx-primary-soft);
  color: var(--nx-primary-text);
  font-size: var(--nx-text-sm);
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
}
.nx-sidebar__user-text { flex: 1; min-width: 0; }
.nx-sidebar__user-name {
  display: block;
  font-size: var(--nx-text-base);
  font-weight: 500;
  color: var(--nx-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.nx-sidebar__plan {
  display: inline-flex;
  align-items: center;
  margin-top: 4px;
  font-size: var(--nx-text-xs);
  font-weight: 500;
  color: var(--nx-gold-text);
  background: var(--nx-gold-soft);
  border: 1px solid var(--nx-warning-border);
  border-radius: var(--nx-radius-sm);
  padding: 0 6px;
}
.nx-nav-item__badge {
  font-size: var(--nx-text-xs);
  color: var(--nx-success);
  background: var(--nx-success-soft);
  padding: 4px 8px;
  border-radius: var(--nx-radius-sm);
  font-weight: 500;
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
