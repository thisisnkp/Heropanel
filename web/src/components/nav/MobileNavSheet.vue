<script setup lang="ts">
/**
 * The More sheet: everything the four tabs do not carry.
 *
 * A sheet rather than a pushed screen, so dismissing it returns you exactly
 * where you were. That is what makes it read as a menu instead of a navigation
 * step you then have to back out of.
 *
 * Inside a site it leads with that site's own sections. The site drawer is a
 * desktop column with nowhere to go on a phone, and without this the only way
 * to reach cron jobs or hotlink protection on mobile was the six shortcuts on
 * the overview — every other section was unreachable.
 */
import { computed, watch } from "vue";
import { useRoute } from "vue-router";
import { NAV_GROUPS } from "@/config/navigation";
import { buildSiteNav, isGroup, type SiteNavLeaf, type SiteNavNode } from "@/config/siteNavigation";
import { useSitesStore } from "@/stores/sites";
import { useUiStore } from "@/stores/ui";

const ui = useUiStore();
const route = useRoute();
const sites = useSitesStore();

const site = computed(() => (String(route.name ?? "").startsWith("site") ? sites.current : null));

/**
 * The site's sections, flattened.
 *
 * The desktop drawer nests them under disclosures; a sheet you are already
 * scrolling should not also make you expand three groups to find Logs. The group
 * label is kept as a prefix on the leaf so the grouping still reads.
 */
const siteSections = computed<{ leaf: SiteNavLeaf; group?: string }[]>(() => {
  const s = site.value;
  if (!s) return [];
  const out: { leaf: SiteNavLeaf; group?: string }[] = [];
  const walk = (nodes: readonly SiteNavNode[], group?: string) => {
    for (const n of nodes) {
      if (isGroup(n)) walk(n.children, group ?? n.label);
      else out.push({ leaf: n, group });
    }
  };
  walk(buildSiteNav({ stackKey: s.stackKey, deploy: s.deploy }));
  return out;
});

function sectionTarget(leaf: SiteNavLeaf) {
  if (leaf.jump) return { name: leaf.to, query: { domain: site.value?.domain ?? "", section: "dns" } };
  return { name: leaf.to, params: { uid: site.value?.uid ?? "" } };
}

// Any navigation closes it. Without this, tapping a destination leaves the
// sheet covering the screen it just opened.
watch(
  () => route.fullPath,
  () => {
    ui.mobileNavOpen = false;
  },
);
</script>

<template>
  <Teleport to="body">
    <Transition name="nx-sheet">
      <div v-if="ui.mobileNavOpen" class="nx-sheet-root">
        <div class="nx-sheet__scrim" @click="ui.mobileNavOpen = false" />

        <div class="nx-sheet" role="dialog" aria-modal="true" aria-label="More destinations">
          <div class="nx-sheet__grip" aria-hidden="true" />

          <div class="nx-sheet__body nxscroll">
            <template v-if="site">
              <div class="nx-sheet__caption">{{ site.domain }}</div>
              <RouterLink
                v-for="s in siteSections"
                :key="s.leaf.to + s.leaf.label"
                :to="sectionTarget(s.leaf)"
                class="nx-sheet__item"
                :class="{ 'is-danger': s.leaf.to === 'site-danger' }"
                :target="s.leaf.newTab ? '_blank' : undefined"
                :rel="s.leaf.newTab ? 'noopener' : undefined"
              >
                <NxIcon :name="s.leaf.icon" size="md" />
                <span class="nx-row__grow">{{ s.group ? s.group + " · " + s.leaf.label : s.leaf.label }}</span>
                <NxIcon v-if="s.leaf.newTab" name="open-in-new" size="sm" />
              </RouterLink>
            </template>

            <template v-for="group in NAV_GROUPS" :key="group.id">
              <div class="nx-sheet__caption">{{ group.label }}</div>

              <template v-for="entry in group.entries" :key="entry.label">
                <RouterLink :to="{ name: entry.to }" class="nx-sheet__item">
                  <NxIcon :name="entry.icon" size="md" />
                  <span>{{ entry.label }}</span>
                </RouterLink>

                <RouterLink
                  v-for="child in entry.children ?? []"
                  :key="child.label"
                  :to="{ name: child.to }"
                  class="nx-sheet__item is-child"
                >
                  <NxIcon :name="child.icon" size="md" />
                  <span>{{ child.label }}</span>
                </RouterLink>
              </template>
            </template>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.nx-sheet-root {
  position: fixed;
  inset: 0;
  z-index: 60;
}
.nx-sheet__item.is-danger { color: var(--nx-danger); }
.nx-sheet__scrim {
  position: absolute;
  inset: 0;
  background: rgba(27, 27, 25, 0.36);
}
.nx-sheet {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  max-height: 78dvh;
  display: flex;
  flex-direction: column;
  background: var(--nx-surface);
  border-top-left-radius: var(--nx-radius-lg);
  border-top-right-radius: var(--nx-radius-lg);
  padding-bottom: calc(56px + env(safe-area-inset-bottom));
}
.nx-sheet__grip {
  width: 36px;
  height: 4px;
  border-radius: var(--nx-radius-pill);
  background: var(--nx-border-strong);
  margin: 10px auto 4px;
}
.nx-sheet__body {
  overflow-y: auto;
  padding: 8px 12px 12px;
}
.nx-sheet__caption {
  font-size: var(--nx-text-xs);
  font-weight: 600;
  letter-spacing: var(--nx-ls-caps);
  color: var(--nx-text-muted);
  text-transform: uppercase;
  padding: 14px 10px 6px;
}
.nx-sheet__item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 10px;
  border-radius: var(--nx-radius-md);
  color: var(--nx-text-2);
  font-size: var(--nx-text-md);
  min-height: 44px;
}
.nx-sheet__item.is-child {
  padding-left: 30px;
  font-size: var(--nx-text-base);
  color: var(--nx-text-3);
}
.nx-sheet__item:active { background: var(--nx-active); }

.nx-sheet-enter-active .nx-sheet,
.nx-sheet-leave-active .nx-sheet {
  transition: transform 240ms cubic-bezier(0.16, 1, 0.3, 1);
}
.nx-sheet-enter-from .nx-sheet,
.nx-sheet-leave-to .nx-sheet {
  transform: translateY(100%);
}
.nx-sheet-enter-active .nx-sheet__scrim,
.nx-sheet-leave-active .nx-sheet__item.is-danger { color: var(--nx-danger); }
.nx-sheet__scrim {
  transition: opacity 240ms ease;
}
.nx-sheet-enter-from .nx-sheet__scrim,
.nx-sheet-leave-to .nx-sheet__item.is-danger { color: var(--nx-danger); }
.nx-sheet__scrim {
  opacity: 0;
}
</style>
