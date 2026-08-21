<script setup lang="ts">
/**
 * The Apps context sidebar.
 *
 * The catalogue's category tree lives here rather than as a chip rail above the
 * grid, which is where this build put it before. Forty apps across nine
 * categories is a tree, and a horizontal rail of nine chips either wraps to two
 * rows or scrolls sideways — both of which hide the category you are looking
 * for. Putting it in the sidebar also means the category is visible while you
 * scroll the grid, which is the whole point of browsing by category.
 *
 * Counts come from the fixtures they describe, so "6 installed" cannot drift
 * away from the list on the next screen.
 */
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import ContextSidebar from "./ContextSidebar.vue";
import ContextNavRow from "./ContextNavRow.vue";
import { APP_CATEGORIES, INSTALLED_APPS, catalogLeaves } from "@/data/apps";
import { useUiStore } from "@/stores/ui";

const route = useRoute();
const router = useRouter();
const ui = useUiStore();

const installed = INSTALLED_APPS.length;
const updates = INSTALLED_APPS.filter((a) => a.state === "Update ready").length;

const onInstall = computed(() => route.name === "apps-install");

const leaves = catalogLeaves();
const activeCategory = computed(() => {
  const q = route.query.category;
  const key = typeof q === "string" ? q : "";
  return leaves.some((c) => c.key === key) ? key : leaves[0].key;
});

function goCategory(key: string) {
  void router.push({ name: "apps-install", query: { category: key } });
}

function groupOpen(key: string) {
  // A parent category opens when one of its children is the one being browsed,
  // so a link straight to /apps/install?category=php arrives expanded.
  const cat = APP_CATEGORIES.find((c) => c.key === key);
  const holdsActive = cat?.children?.some((c) => c.key === activeCategory.value) ?? false;
  return ui.isGroupOpen(`apps:${key}`) || holdsActive;
}
</script>

<template>
  <ContextSidebar
    nav-label="Apps sections"
    back-to="home"
    back-label="Back to panel"
    title="Apps"
    footer-caption="INCLUDED IN YOUR PLAN"
    footer-text="OpenClaw and n8n run free on Business. Paid apps are billed monthly."
  >
    <template #chips>
      <span class="apps__chip is-installed">{{ installed }} installed</span>
      <span v-if="updates" class="apps__chip is-updates">
        {{ updates }} {{ updates === 1 ? "update" : "updates" }}
      </span>
    </template>

    <ContextNavRow
      label="Installed apps"
      icon="grid-view"
      to="apps-installed"
      :current="route.name === 'apps-installed'"
    />

    <ContextNavRow
      label="Install new app"
      icon="add-circle"
      expandable
      :expanded="onInstall"
      @activate="router.push({ name: 'apps-install' })"
    />

    <template v-if="onInstall">
      <template v-for="cat in APP_CATEGORIES" :key="cat.key">
        <template v-if="cat.children?.length">
          <ContextNavRow
            :label="cat.label"
            :icon="cat.icon"
            :depth="1"
            expandable
            :expanded="groupOpen(cat.key)"
            @activate="ui.toggleGroup(`apps:${cat.key}`)"
          />
          <template v-if="groupOpen(cat.key)">
            <ContextNavRow
              v-for="child in cat.children"
              :key="child.key"
              :label="child.label"
              :icon="child.icon"
              :depth="2"
              :current="child.key === activeCategory"
              @activate="goCategory(child.key)"
            />
          </template>
        </template>

        <ContextNavRow
          v-else
          :label="cat.label"
          :icon="cat.icon"
          :depth="1"
          :current="cat.key === activeCategory"
          @activate="goCategory(cat.key)"
        />
      </template>
    </template>

    <ContextNavRow
      label="Paid app licenses"
      icon="vpn-key"
      to="apps-licenses"
      :current="route.name === 'apps-licenses'"
    />
  </ContextSidebar>
</template>

<style scoped>
.apps__chip {
  font-size: var(--nx-text-xs);
  padding: 4px 8px;
  border-radius: var(--nx-radius-sm);
  font-weight: 500;
  white-space: nowrap;
}
.apps__chip.is-installed { background: var(--nx-success-soft); color: var(--nx-success); }
.apps__chip.is-updates { background: var(--nx-gold-soft); color: var(--nx-warning); }

</style>
