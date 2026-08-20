<script setup lang="ts">
/**
 * The per-site navigation column.
 *
 * Its contents come from buildSiteNav(), which is a function of the site rather
 * than a constant — a static site has no runtime and WordPress tools only exist
 * on a WordPress site. See config/siteNavigation.ts for why that is built rather
 * than filtered.
 *
 * Groups auto-open when they contain the active route, so arriving by deep link
 * shows you where you are instead of a collapsed list.
 */
import { computed } from "vue";
import { useRoute } from "vue-router";
import { buildSiteNav, isGroup, type SiteNavGroup, type SiteNavNode } from "@/config/siteNavigation";
import { STACKS } from "@/config/stacks";
import { useSitesStore } from "@/stores/sites";
import { useUiStore } from "@/stores/ui";

const route = useRoute();
const sites = useSitesStore();
const ui = useUiStore();

const site = computed(() => sites.current);
const stack = computed(() => (site.value ? STACKS[site.value.stackKey] : null));

const nodes = computed<SiteNavNode[]>(() =>
  site.value ? buildSiteNav({ stackKey: site.value.stackKey, deploy: site.value.deploy }) : [],
);

function leafActive(to: string) {
  return route.name === to;
}

/** True when any descendant leaf is the active route, at any depth. */
function containsActive(group: SiteNavGroup): boolean {
  return group.children.some((c) => (isGroup(c) ? containsActive(c) : leafActive(c.to)));
}

function groupOpen(group: SiteNavGroup) {
  return ui.isGroupOpen(`site:${group.id}`) || containsActive(group);
}

function siteLink(to: string) {
  return { name: to, params: { id: String(site.value?.id ?? "") } };
}
</script>

<template>
  <aside class="nx-drawer nxhide" aria-label="Site sections">
    <div v-if="site" class="nx-drawer__head">
      <div class="nx-drawer__domain">{{ site.domain }}</div>
      <NxBadge v-if="stack" :bg="stack.bg" :fg="stack.fg">{{ stack.tag }}</NxBadge>
    </div>

    <RouterLink :to="{ name: 'websites' }" class="nx-drawer__back">
      <NxIcon name="arrow-back" size="sm" />
      <span>All websites</span>
    </RouterLink>

    <ul class="nx-drawer__list">
      <li v-for="node in nodes" :key="isGroup(node) ? node.id : node.to + node.label">
        <!-- Group: a disclosure, with its children beneath. -->
        <template v-if="isGroup(node)">
          <button
            type="button"
            class="nx-drawer__item is-parent"
            :aria-expanded="groupOpen(node)"
            @click="ui.toggleGroup(`site:${node.id}`)"
          >
            <NxIcon :name="node.icon" size="md" class="nx-drawer__icon" />
            <span class="nx-drawer__label">{{ node.label }}</span>
            <NxIcon :name="groupOpen(node) ? 'arrow-drop-down' : 'arrow-right'" size="md" class="nx-drawer__caret" />
          </button>

          <ul v-if="groupOpen(node)" class="nx-drawer__list is-nested">
            <li v-for="child in node.children" :key="isGroup(child) ? child.id : child.to + child.label">
              <!-- One more level: the Git sub-group inside Advanced. -->
              <template v-if="isGroup(child)">
                <button
                  type="button"
                  class="nx-drawer__item is-parent is-child"
                  :aria-expanded="groupOpen(child)"
                  @click="ui.toggleGroup(`site:${child.id}`)"
                >
                  <NxIcon :name="child.icon" size="md" class="nx-drawer__icon" />
                  <span class="nx-drawer__label">{{ child.label }}</span>
                  <NxIcon :name="groupOpen(child) ? 'arrow-drop-down' : 'arrow-right'" size="md" class="nx-drawer__caret" />
                </button>
                <ul v-if="groupOpen(child)" class="nx-drawer__list is-nested">
                  <li v-for="g in child.children" :key="g.label">
                    <RouterLink
                      v-if="!isGroup(g)"
                      :to="siteLink(g.to)"
                      class="nx-drawer__item is-grandchild"
                      :class="{ 'is-current': leafActive(g.to) }"
                      :aria-current="leafActive(g.to) ? 'page' : undefined"
                    >
                      <NxIcon :name="g.icon" size="md" class="nx-drawer__icon" />
                      <span class="nx-drawer__label">{{ g.label }}</span>
                    </RouterLink>
                  </li>
                </ul>
              </template>

              <RouterLink
                v-else
                :to="siteLink(child.to)"
                class="nx-drawer__item is-child"
                :class="{ 'is-current': leafActive(child.to) }"
                :aria-current="leafActive(child.to) ? 'page' : undefined"
              >
                <NxIcon :name="child.icon" size="md" class="nx-drawer__icon" />
                <span class="nx-drawer__label">{{ child.label }}</span>
              </RouterLink>
            </li>
          </ul>
        </template>

        <RouterLink
          v-else
          :to="siteLink(node.to)"
          class="nx-drawer__item"
          :class="{ 'is-current': leafActive(node.to) }"
          :aria-current="leafActive(node.to) ? 'page' : undefined"
        >
          <NxIcon :name="node.icon" size="md" class="nx-drawer__icon" />
          <span class="nx-drawer__label">{{ node.label }}</span>
        </RouterLink>
      </li>
    </ul>
  </aside>
</template>

<style scoped>
.nx-drawer {
  width: 236px;
  flex: 0 0 236px;
  background: var(--nx-surface);
  border-right: 1px solid var(--nx-border);
  display: flex;
  flex-direction: column;
  padding: 20px 16px 16px;
  overflow-y: auto;
  min-height: 0;
  animation: nxSidebar 220ms cubic-bezier(0.16, 1, 0.3, 1) both;
}
.nx-drawer__head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 8px 10px;
}
.nx-drawer__domain {
  flex: 1;
  min-width: 0;
  font-size: var(--nx-text-base);
  font-weight: 600;
  color: var(--nx-text);
  font-family: "JetBrains Mono", ui-monospace, monospace;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.nx-drawer__back {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px 14px;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
}
.nx-drawer__back:hover { color: var(--nx-text-2); }
.nx-drawer__list {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.nx-drawer__list.is-nested { margin-top: 2px; }
.nx-drawer__item {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  text-align: left;
  border: 0;
  background: transparent;
  font-family: inherit;
  cursor: pointer;
  padding: 7px 10px;
  border-radius: var(--nx-radius-md);
  font-size: var(--nx-text-base);
  color: var(--nx-text-3);
  transition: background 130ms ease, color 130ms ease;
}
.nx-drawer__item.is-child { padding-left: 24px; }
.nx-drawer__item.is-grandchild { padding-left: 40px; }
.nx-drawer__item:hover { background: var(--nx-hover); }
.nx-drawer__item.is-current {
  background: var(--nx-primary-soft);
  color: var(--nx-primary-text);
  font-weight: 500;
}
.nx-drawer__icon { color: var(--nx-text-muted); }
.nx-drawer__item.is-current .nx-drawer__icon { color: var(--nx-primary-text); }
.nx-drawer__label { flex: 1; min-width: 0; }
.nx-drawer__caret { color: var(--nx-text-placeholder); }
</style>
