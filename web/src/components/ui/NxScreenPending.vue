<script setup lang="ts">
/**
 * A screen that has a route and a place in the navigation, but whose body has
 * not been ported from the design yet.
 *
 * Deliberately loud rather than an empty page: a blank screen is
 * indistinguishable from a screen that failed to load, and both look "done" in a
 * demo. This says which design file the markup comes from, so finishing it is a
 * lookup rather than a hunt.
 */
defineProps<{
  /** Which of the three design files carries this screen. */
  source?: string;
  /** What the finished screen is for, in one line. */
  note?: string;
}>();
</script>

<template>
  <div class="nx-pending">
    <NxIcon name="construction" size="lg" class="nx-pending__icon" />
    <div class="nx-pending__body">
      <p class="nx-pending__title">Not ported yet</p>
      <p v-if="note" class="nx-pending__note">{{ note }}</p>
      <p class="nx-pending__source">
        Design: <code>{{ source ?? "panel_ui_ref/NexPanel.dc.html" }}</code>
      </p>
    </div>
  </div>
</template>

<style scoped>
.nx-pending {
  display: flex;
  gap: 14px;
  padding: 20px;
  border: 1px dashed var(--nx-border-strong);
  border-radius: var(--nx-radius-lg);
  background: var(--nx-surface-2);
}
.nx-pending__icon { color: var(--nx-warning); margin-top: 2px; }
.nx-pending__body { min-width: 0; }
.nx-pending__title {
  margin: 0;
  font-size: var(--nx-text-base);
  font-weight: 600;
  color: var(--nx-text);
}
.nx-pending__note {
  margin: 4px 0 0;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
}
.nx-pending__source {
  margin: 8px 0 0;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-placeholder);
}
.nx-pending__source code {
  font-family: "JetBrains Mono", ui-monospace, monospace;
  font-size: var(--nx-text-xs);
}
</style>
