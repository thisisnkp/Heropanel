<script setup lang="ts">
/**
 * The 64px icon rail, shown beside the site drawer where the full sidebar would
 * not fit. Same destinations as the sidebar, shortened labels.
 */
import { useRoute } from "vue-router";
import { RAIL_ENTRIES } from "@/config/navigation";
import { useAccountStore } from "@/stores/account";

const route = useRoute();
const account = useAccountStore();
</script>

<template>
  <nav class="nx-rail nxhide" aria-label="Sections">
    <!-- The brand mark doubles as the way home. In the design it is decoration;
         at 64px wide it is also the only affordance a user reaches for when they
         want out of a scoped context, so it is a link. -->
    <RouterLink :to="{ name: 'home' }" class="nx-rail__mark" aria-label="NexPanel home">N</RouterLink>

    <RouterLink
      v-for="entry in RAIL_ENTRIES"
      :key="entry.label"
      :to="{ name: entry.to }"
      class="nx-rail__item"
      :class="{ 'is-current': route.name === entry.to }"
      :aria-current="route.name === entry.to ? 'page' : undefined"
    >
      <NxIcon :name="entry.icon" size="lg" />
      <span class="nx-rail__label">{{ entry.label }}</span>
    </RouterLink>

    <span class="nx-rail__spacer" />

    <RouterLink :to="{ name: 'billing' }" class="nx-rail__avatar" :aria-label="account.name">
      {{ account.initials }}
    </RouterLink>
  </nav>
</template>

<style scoped>
.nx-rail {
  width: 64px;
  flex: 0 0 64px;
  background: var(--nx-surface-2);
  border-right: 1px solid var(--nx-border);
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 16px 0 12px;
  overflow-y: auto;
  overflow-x: hidden;
  min-height: 0;
  animation: nxSidebar 220ms cubic-bezier(0.16, 1, 0.3, 1) both;
}
.nx-rail__mark {
  width: 26px;
  height: 26px;
  flex: 0 0 26px;
  border-radius: var(--nx-radius-md);
  background: var(--nx-violet-900);
  color: var(--nx-gold-400);
  font-size: var(--nx-text-base);
  font-weight: 600;
  line-height: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 16px;
}
.nx-rail__spacer { flex: 1; min-height: 12px; }
.nx-rail__avatar {
  width: 26px;
  height: 26px;
  flex: 0 0 26px;
  border-radius: var(--nx-radius-full);
  background: var(--nx-primary-soft);
  color: var(--nx-primary-text);
  font-size: var(--nx-text-xs);
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
}
.nx-rail__item {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  width: 56px;
  padding: 8px 2px;
  border-radius: var(--nx-radius-md);
  color: var(--nx-text-muted);
  text-align: center;
}
.nx-rail__item:hover { background: var(--nx-hover); color: var(--nx-text-2); }
.nx-rail__item.is-current { color: var(--nx-primary); background: var(--nx-primary-soft); }
.nx-rail__label {
  font-size: 10px;
  line-height: 1.2;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
