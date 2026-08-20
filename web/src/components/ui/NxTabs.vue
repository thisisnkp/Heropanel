<script setup lang="ts">
/**
 * The chip tab strip the design uses for in-page sections (Apps, Security).
 *
 * Tabs are router links, not local state, so a section is addressable — the
 * whole reason Apps and Security have child routes rather than a `section` ref.
 * Rendered as a tablist for assistive tech, with the links as the tabs.
 */
export interface NxTab {
  readonly to: string;
  readonly label: string;
  readonly icon?: string;
}

defineProps<{ tabs: readonly NxTab[] }>();
</script>

<template>
  <nav class="nx-tabs nxhide" role="tablist">
    <RouterLink
      v-for="tab in tabs"
      :key="tab.to"
      :to="{ name: tab.to }"
      class="nx-tabs__tab"
      role="tab"
      active-class="is-current"
    >
      <NxIcon v-if="tab.icon" :name="tab.icon" size="sm" />
      <span>{{ tab.label }}</span>
    </RouterLink>
  </nav>
</template>

<style scoped>
.nx-tabs {
  display: flex;
  gap: 6px;
  overflow-x: auto;
  padding-bottom: 4px;
  margin-bottom: 20px;
  border-bottom: 1px solid var(--nx-border);
}
.nx-tabs__tab {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
  padding: 7px 12px;
  border-radius: var(--nx-radius-pill);
  font-size: var(--nx-text-base);
  color: var(--nx-text-3);
  border: 1px solid transparent;
}
.nx-tabs__tab:hover { background: var(--nx-hover); color: var(--nx-text-2); }
.nx-tabs__tab.is-current {
  background: var(--nx-primary-soft);
  border-color: var(--nx-primary-border);
  color: var(--nx-primary-text);
  font-weight: 500;
}
</style>
