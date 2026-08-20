<script setup lang="ts">
/** Four destinations plus the More sheet, pinned to the bottom. */
import { useRoute } from "vue-router";
import { MOBILE_TABS } from "@/config/navigation";
import { useUiStore } from "@/stores/ui";

const route = useRoute();
const ui = useUiStore();
</script>

<template>
  <nav class="nx-tabbar" aria-label="Primary">
    <RouterLink
      v-for="tab in MOBILE_TABS"
      :key="tab.label"
      :to="{ name: tab.to }"
      class="nx-tabbar__tab"
      :class="{ 'is-current': route.name === tab.to }"
      :aria-current="route.name === tab.to ? 'page' : undefined"
    >
      <NxIcon :name="tab.icon" size="lg" />
      <span class="nx-tabbar__label">{{ tab.label }}</span>
    </RouterLink>

    <button
      type="button"
      class="nx-tabbar__tab"
      :class="{ 'is-current': ui.mobileNavOpen }"
      :aria-expanded="ui.mobileNavOpen"
      @click="ui.mobileNavOpen = !ui.mobileNavOpen"
    >
      <NxIcon name="more-horiz" size="lg" />
      <span class="nx-tabbar__label">More</span>
    </button>
  </nav>
</template>

<style scoped>
.nx-tabbar {
  position: fixed;
  left: 0;
  right: 0;
  bottom: 0;
  z-index: 40;
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  background: rgba(255, 255, 255, 0.94);
  backdrop-filter: blur(10px);
  border-top: 1px solid var(--nx-border);
  padding-bottom: env(safe-area-inset-bottom);
}
.nx-tabbar__tab {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 3px;
  padding: 9px 2px 7px;
  border: 0;
  background: transparent;
  font-family: inherit;
  color: var(--nx-text-muted);
  cursor: pointer;
  /* Comfortably above the 44px minimum touch target, label included. */
  min-height: 52px;
}
.nx-tabbar__tab.is-current { color: var(--nx-primary); }
.nx-tabbar__label { font-size: 10px; line-height: 1.2; }
</style>
