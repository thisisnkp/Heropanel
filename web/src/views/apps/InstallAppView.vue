<script setup lang="ts">
/**
 * Install new app — the catalogue, browsed by category.
 *
 * The category is a query parameter rather than local state, so "send me the
 * Databases page" is a link. The design put the category tree in the sidebar;
 * here it is a rail above the grid, because the sidebar in this build is the
 * panel's own navigation and nesting a second tree inside it makes both harder
 * to read.
 */
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import { catalogLeaves } from "@/data/apps";
import { useUiStore } from "@/stores/ui";

const route = useRoute();
const router = useRouter();
const ui = useUiStore();

const categories = catalogLeaves();

const activeKey = computed(() => {
  const q = route.query.category;
  const key = typeof q === "string" ? q : "";
  return categories.some((c) => c.key === key) ? key : categories[0].key;
});

const active = computed(() => categories.find((c) => c.key === activeKey.value) ?? categories[0]);

function pick(key: string) {
  void router.replace({ name: "apps-install", query: { category: key } });
}

function badgeTone(badge: string) {
  return badge === "Installed" || badge === "Free" ? "success" : "info";
}
</script>

<template>
  <div class="nx-view">
    <header class="cat__head">
      <p class="cat__kicker">Install · {{ active.label }}</p>
      <h1 class="cat__title">{{ active.label }}</h1>
      <p class="cat__sub">{{ active.sub }}</p>
    </header>

    <nav class="cat__rail nxhide" aria-label="App categories">
      <button
        v-for="c in categories"
        :key="c.key"
        type="button"
        class="cat__chip"
        :class="{ 'is-current': c.key === activeKey }"
        :aria-current="c.key === activeKey ? 'page' : undefined"
        @click="pick(c.key)"
      >
        <NxIcon :name="c.icon" size="sm" />
        {{ c.label }}
      </button>
    </nav>

    <div class="nx-grid nx-grid--3">
      <article v-for="a in active.apps" :key="a.name" class="cat__card">
        <div class="cat__card-head">
          <span class="cat__icon"><NxIcon :name="a.icon" size="lg" /></span>
          <span class="cat__name nx-row__grow">{{ a.name }}</span>
          <NxBadge :tone="badgeTone(a.badge)">{{ a.badge }}</NxBadge>
        </div>
        <p class="cat__desc">{{ a.desc }}</p>
        <div class="cat__spacer" />
        <NxButton
          class="cat__install"
          :disabled="a.badge === 'Installed'"
          @click="ui.toast('Installing ' + a.name + ' is not wired up yet.', 'info')"
        >
          {{ a.badge === "Installed" ? "Installed" : "Install" }}
        </NxButton>
      </article>
    </div>
  </div>
</template>

<style scoped>
.cat__head { padding-bottom: 20px; }
.cat__kicker {
  margin: 0;
  font-size: var(--nx-text-xs);
  letter-spacing: var(--nx-ls-caps);
  color: var(--nx-text-placeholder);
  font-weight: 600;
  text-transform: uppercase;
}
.cat__title {
  margin: 6px 0 0;
  font-size: var(--nx-text-xl);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
}
.cat__sub {
  margin: 6px 0 0;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
  text-wrap: pretty;
}

.cat__rail {
  display: flex;
  gap: 6px;
  overflow-x: auto;
  padding-bottom: 4px;
  margin-bottom: 20px;
}
.cat__chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
  padding: 7px 12px;
  border-radius: var(--nx-radius-pill);
  font-size: var(--nx-text-base);
  font-family: inherit;
  color: var(--nx-text-3);
  border: 1px solid var(--nx-border);
  background: var(--nx-surface);
  cursor: pointer;
}
.cat__chip:hover { background: var(--nx-hover); }
.cat__chip.is-current {
  background: var(--nx-primary-soft);
  border-color: var(--nx-primary-border);
  color: var(--nx-primary-text);
  font-weight: 500;
}

.cat__card {
  background: var(--nx-surface);
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-lg);
  padding: 16px;
  display: flex;
  flex-direction: column;
}
.cat__card-head { display: flex; align-items: center; gap: 12px; }
.cat__icon {
  width: 34px;
  height: 34px;
  flex: 0 0 34px;
  border-radius: var(--nx-radius-md);
  background: var(--nx-bg);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--nx-text-2);
}
.cat__name {
  font-size: var(--nx-text-base);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
}
.cat__desc {
  margin: 12px 0 0;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
  line-height: 1.5;
  text-wrap: pretty;
}
.cat__spacer { flex: 1; }
.cat__install { margin-top: 16px; width: 100%; }
</style>
