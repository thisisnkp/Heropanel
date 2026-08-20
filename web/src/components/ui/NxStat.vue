<script setup lang="ts">
/** One number with a caption — the dashboard and automation tiles. */
defineProps<{ label: string; value: string | number; hint?: string }>();
</script>

<template>
  <div class="nx-stat">
    <div class="nx-stat__value">{{ value }}</div>
    <div class="nx-stat__label">{{ label }}</div>
    <div v-if="hint" class="nx-stat__hint">{{ hint }}</div>
  </div>
</template>

<style scoped>
.nx-stat {
  background: var(--nx-surface);
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-lg);
  padding: 16px;
}
.nx-stat__value {
  font-size: var(--nx-text-2xl);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
  color: var(--nx-text);
  line-height: 1.1;
}
.nx-stat__label {
  margin-top: 6px;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
}
.nx-stat__hint {
  margin-top: 4px;
  font-size: var(--nx-text-xs);
  color: var(--nx-text-placeholder);
}
</style>
