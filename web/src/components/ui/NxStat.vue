<script setup lang="ts">
/**
 * One figure with a caption — the dashboard, websites and section stat rows.
 *
 * Ordered label → value → sub to match the design, which leads with what the
 * number *is*. Reading a column of bare numbers and only then learning their
 * labels is the reason that order was chosen.
 */
defineProps<{ label: string; value: string | number; sub?: string }>();
</script>

<template>
  <div class="nx-stat">
    <div class="nx-stat__label">{{ label }}</div>
    <div class="nx-stat__value">{{ value }}</div>
    <div v-if="sub" class="nx-stat__sub">{{ sub }}</div>
  </div>
</template>

<style scoped>
.nx-stat {
  background: var(--nx-surface);
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-lg);
  padding: 16px;
  min-width: 0;
}
.nx-stat__label {
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
  letter-spacing: var(--nx-ls-caps);
}
.nx-stat__value {
  font-size: var(--nx-text-xl);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
  padding-top: 6px;
  color: var(--nx-text);
}
.nx-stat__sub {
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
  padding-top: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
