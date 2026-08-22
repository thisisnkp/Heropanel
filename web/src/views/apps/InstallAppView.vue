<script setup lang="ts">
/**
 * Install new app — the catalogue, browsed by category.
 *
 * The category is a query parameter rather than local state, so "send me the
 * Databases page" is a link. The tree that picks it lives in the Apps context
 * sidebar, which replaces the global navigation while you are in this section —
 * so the categories stay visible as you scroll the grid, and this screen is only
 * the grid.
 */
import { computed } from "vue";
import { useRoute } from "vue-router";
import { catalogLeaves, type AppTag } from "@/data/apps";
import { useUiStore } from "@/stores/ui";

const route = useRoute();
const ui = useUiStore();

const categories = catalogLeaves();

const activeKey = computed(() => {
  const q = route.query.category;
  const key = typeof q === "string" ? q : "";
  return categories.some((c) => c.key === key) ? key : categories[0].key;
});

const active = computed(() => categories.find((c) => c.key === activeKey.value) ?? categories[0]);

function badgeTone(badge: string) {
  return badge === "Installed" || badge === "Free" ? "success" : "info";
}

// The support chip. `eol` is the only one that gets a warning colour: the others
// are information, but an unpatched interpreter facing the internet is the one
// thing on this screen someone should not be able to pick by accident.
const TAG_TEXT: Record<AppTag, string> = {
  current: "newest",
  active: "actively supported",
  lts: "long-term support",
  "security-only": "security fixes only",
  eol: "end of life",
};

</script>

<template>
  <div class="nx-view">
    <header class="cat__head">
      <p class="cat__kicker">Install · {{ active.label }}</p>
      <h1 class="cat__title">{{ active.label }}</h1>
      <p class="cat__sub">{{ active.sub }}</p>
      <p class="cat__meta">
        A catalogue of what this panel's stack is made of — installing from here is not wired up
        yet, and nothing on this screen reads the host.
      </p>
    </header>

    <NxCallout v-if="active.note" :tone="active.noteTone ?? 'info'" class="cat__note">
      {{ active.note }}
    </NxCallout>

    <div class="nx-grid nx-grid--3">
      <article v-for="a in active.apps" :key="a.name" class="cat__card">
        <div class="cat__card-head">
          <span class="cat__icon"><NxIcon :name="a.icon" size="lg" /></span>
          <span class="cat__name nx-row__grow">{{ a.name }}</span>
          <NxBadge :tone="badgeTone(a.badge)">{{ a.badge }}</NxBadge>
        </div>
        <p v-if="a.tag" class="cat__tag" :class="`cat__tag--${a.tag}`">{{ TAG_TEXT[a.tag] }}</p>
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
.cat__meta {
  margin: 8px 0 0;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-placeholder);
  text-wrap: pretty;
}
.cat__note { margin-bottom: 20px; }

.cat__tag {
  margin: 10px 0 0;
  font-size: var(--nx-text-xs);
  font-weight: 600;
  letter-spacing: var(--nx-ls-caps);
  text-transform: uppercase;
  color: var(--nx-text-placeholder);
}
/* End of life is the one that changes a decision, so it is the one that is
   coloured. Colouring all four would make none of them mean anything. */
.cat__tag--eol { color: var(--nx-warning); }

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
