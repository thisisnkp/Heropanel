<script setup lang="ts">
/**
 * The per-site navigation column.
 *
 * Its contents come from buildSiteNav(), which is a function of the site rather
 * than a constant — a static site has no runtime and WordPress tools only exist
 * on a WordPress site. See config/siteNavigation.ts for why that is built rather
 * than filtered.
 *
 * The switcher at the top is the reason this is not just a menu: once you are
 * inside a site every screen is scoped to it, so moving to another site would
 * otherwise mean going back to the list and losing the section you were in.
 * Switching here keeps the section and changes the site, which is what someone
 * comparing two sites' logs actually wants.
 *
 * Groups auto-open when they contain the active route, so arriving by deep link
 * shows you where you are instead of a collapsed list.
 */
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import ContextSidebar from "./ContextSidebar.vue";
import ContextNavRow from "./ContextNavRow.vue";
import NxSwitcher from "@/components/ui/NxSwitcher.vue";
import { buildSiteNav, isGroup, navExplain, type SiteNavGroup, type SiteNavLeaf, type SiteNavNode } from "@/config/siteNavigation";
import { STACKS } from "@/config/stacks";
import { useSitesStore } from "@/stores/sites";
import { useUiStore } from "@/stores/ui";

const route = useRoute();
const router = useRouter();
const sites = useSitesStore();
const ui = useUiStore();

const site = computed(() => sites.current);
const stack = computed(() => (site.value ? STACKS[site.value.stackKey] : null));

const nodes = computed<SiteNavNode[]>(() =>
  site.value ? buildSiteNav({ stackKey: site.value.stackKey, deploy: site.value.deploy }) : [],
);

const explain = computed(() =>
  site.value ? navExplain({ stackKey: site.value.stackKey, deploy: site.value.deploy }) : "",
);

const switcherItems = computed(() =>
  sites.sites.map((s) => ({ key: s.id, label: s.domain, sub: STACKS[s.stackKey].label })),
);

/**
 * Switching keeps the section you are on when the other site has it.
 *
 * A WordPress site has a plugins screen and a Python site does not, so carrying
 * the route blindly would land on a section that cannot exist there. Falling
 * back to the overview is the only honest answer in that case.
 */
function switchTo(id: string | number) {
  const next = sites.sites.find((s) => s.id === Number(id));
  if (!next) return;

  const here = String(route.name ?? "");
  const available = new Set<string>();
  const collect = (list: readonly SiteNavNode[]) => {
    for (const n of list) {
      if (isGroup(n)) collect(n.children);
      else available.add(n.to);
    }
  };
  collect(buildSiteNav({ stackKey: next.stackKey, deploy: next.deploy }));

  const target = available.has(here) ? here : "site-overview";
  void router.push({ name: target, params: { id: String(next.id) } });
}

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

const params = computed(() => ({ id: String(site.value?.id ?? "") }));

/**
 * "DNS zone editor" leaves the site scope, so it takes no :id — but it has to
 * carry the domain, or it lands on the zone picker having thrown away the one
 * thing the user had already chosen.
 */
const jumpQuery = computed(() => ({ domain: site.value?.domain ?? "", section: "dns" }));

/**
 * The third level of the drawer holds only leaves — the design nests Git inside
 * Advanced and stops there. Narrowing here rather than in the template keeps the
 * row markup free of type guards that would only ever take one branch.
 */
function leavesOf(group: SiteNavGroup): SiteNavLeaf[] {
  return group.children.filter((c): c is SiteNavLeaf => !isGroup(c));
}
</script>

<template>
  <ContextSidebar
    nav-label="Site sections"
    back-to="websites"
    back-label="All websites"
    footer-caption="SHOWN FOR THIS SITE"
    :footer-text="explain"
  >
    <template #top>
      <NxSwitcher
        v-if="site"
        :items="switcherItems"
        :current="site.id"
        placeholder="Search websites"
        empty-text="No website matches."
        label="Switch website"
        @pick="switchTo"
      >
        <template #trigger>
          <span class="drawer__name">{{ site.name }}</span>
          <span class="drawer__meta">
            <NxBadge v-if="stack" :bg="stack.bg" :fg="stack.fg">{{ stack.label }}</NxBadge>
            <span class="drawer__deploy">{{ site.deploy }}</span>
          </span>
        </template>
      </NxSwitcher>
    </template>

    <template v-for="node in nodes" :key="isGroup(node) ? node.id : node.to + node.label">
      <template v-if="isGroup(node)">
        <ContextNavRow
          :label="node.label"
          :icon="node.icon"
          expandable
          :expanded="groupOpen(node)"
          @activate="ui.toggleGroup(`site:${node.id}`)"
        />

        <template v-if="groupOpen(node)">
          <template v-for="child in node.children" :key="isGroup(child) ? child.id : child.to + child.label">
            <template v-if="isGroup(child)">
              <ContextNavRow
                :label="child.label"
                :icon="child.icon"
                :depth="1"
                expandable
                :expanded="groupOpen(child)"
                @activate="ui.toggleGroup(`site:${child.id}`)"
              />
              <template v-if="groupOpen(child)">
                <ContextNavRow
                  v-for="g in leavesOf(child)"
                  :key="g.label"
                  :label="g.label"
                  :icon="g.icon"
                  :depth="2"
                  :to="g.to"
                  :params="g.jump ? undefined : params"
                  :query="g.jump ? jumpQuery : undefined"
                  :new-tab="g.newTab"
                  :current="leafActive(g.to)"
                />
              </template>
            </template>

            <ContextNavRow
              v-else
              :label="child.label"
              :icon="child.icon"
              :depth="1"
              :to="child.to"
              :params="child.jump ? undefined : params"
              :query="child.jump ? jumpQuery : undefined"
              :new-tab="child.newTab"
              :current="leafActive(child.to)"
            />
          </template>
        </template>
      </template>

      <ContextNavRow
        v-else
        :label="node.label"
        :icon="node.icon"
        :to="node.to"
        :params="node.jump ? undefined : params"
        :query="node.jump ? jumpQuery : undefined"
        :new-tab="node.newTab"
        :current="leafActive(node.to)"
        :tone="node.to === 'site-danger' ? 'danger' : 'default'"
      />
    </template>
  </ContextSidebar>
</template>

<style scoped>
.drawer__name {
  display: block;
  font-size: var(--nx-text-base);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.drawer__meta { display: flex; align-items: center; gap: 6px; padding-top: 4px; }
.drawer__deploy {
  font-size: var(--nx-text-xs);
  color: var(--nx-text-muted);
  font-family: "JetBrains Mono", ui-monospace, monospace;
}
</style>
