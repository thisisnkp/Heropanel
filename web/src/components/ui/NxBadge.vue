<script setup lang="ts">
/**
 * Status pill. `tone` covers the semantic states; `bg`/`fg` exist for the stack
 * badges, whose colours come from the stack table rather than from a status.
 */
withDefaults(
  defineProps<{
    tone?: "neutral" | "success" | "warning" | "danger" | "info" | "brand";
    bg?: string;
    fg?: string;
    /** Leading dot, as the design uses for live/building site rows. */
    dot?: boolean;
  }>(),
  { tone: "neutral", dot: false },
);
</script>

<template>
  <span
    :class="['nx-badge', `nx-badge--${tone}`]"
    :style="bg || fg ? { background: bg, color: fg } : undefined"
  >
    <span v-if="dot" class="nx-badge__dot" aria-hidden="true" />
    <slot />
  </span>
</template>

<style scoped>
.nx-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border-radius: var(--nx-radius-pill);
  padding: 3px 10px;
  font-size: var(--nx-text-xs);
  font-weight: 600;
  letter-spacing: var(--nx-ls-normal);
  white-space: nowrap;
}
.nx-badge__dot {
  width: 6px;
  height: 6px;
  border-radius: var(--nx-radius-full);
  background: currentColor;
}
.nx-badge--neutral { background: var(--nx-hover); color: var(--nx-text-muted); }
.nx-badge--success { background: var(--nx-success-soft); color: var(--nx-success); }
.nx-badge--warning { background: var(--nx-warning-soft); color: var(--nx-warning); }
.nx-badge--danger { background: var(--nx-danger-soft); color: var(--nx-danger); }
.nx-badge--info { background: var(--nx-info-soft); color: var(--nx-info); }
.nx-badge--brand { background: var(--nx-primary-soft); color: var(--nx-primary-text); }
</style>
