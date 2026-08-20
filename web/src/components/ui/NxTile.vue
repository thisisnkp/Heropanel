<script setup lang="ts">
/**
 * The clickable icon + label + sub tile: "Jump back in", the security scan
 * shortcuts, and the app cards.
 *
 * Renders as a RouterLink when given `to`, otherwise a button — so a tile that
 * navigates is a real link (middle-clickable, has a URL on hover) and a tile
 * that performs an action is not pretending to be one.
 */
defineProps<{
  icon?: string;
  label: string;
  sub?: string;
  /** Route name. Omit for an action tile and listen for @click. */
  to?: string;
  /** Right-aligned figure, as the security scan tiles use. */
  value?: string;
  tone?: "brand" | "success" | "warning" | "danger";
}>();
</script>

<template>
  <component
    :is="to ? 'RouterLink' : 'button'"
    :to="to ? { name: to } : undefined"
    :type="to ? undefined : 'button'"
    class="nx-tile"
  >
    <span v-if="icon" :class="['nx-tile__icon', tone ? `nx-tile__icon--${tone}` : '']">
      <NxIcon :name="icon" size="md" />
    </span>
    <span class="nx-tile__body">
      <span class="nx-tile__label">{{ label }}</span>
      <span v-if="$slots.default" class="nx-tile__sub"><slot /></span>
      <span v-else-if="sub" class="nx-tile__sub">{{ sub }}</span>
    </span>
    <span v-if="value" :class="['nx-tile__value', tone ? `nx-tile__value--${tone}` : '']">{{ value }}</span>
  </component>
</template>

<style scoped>
.nx-tile {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  text-align: left;
  border: 1px solid var(--nx-active);
  background: var(--nx-surface-2);
  border-radius: var(--nx-radius-md);
  padding: 12px;
  cursor: pointer;
  font-family: inherit;
  color: inherit;
  transition: background 120ms ease, border-color 120ms ease;
}
.nx-tile:hover { background: var(--nx-hover); border-color: var(--nx-border-strong); }
.nx-tile__icon {
  width: 32px;
  height: 32px;
  flex: 0 0 32px;
  border-radius: var(--nx-radius-md);
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--nx-hover);
  color: var(--nx-primary);
}
.nx-tile__icon--brand { background: var(--nx-primary-soft); color: var(--nx-primary-text); }
.nx-tile__icon--success { background: var(--nx-success-soft); color: var(--nx-success); }
.nx-tile__icon--warning { background: var(--nx-gold-soft); color: var(--nx-warning); }
.nx-tile__icon--danger { background: var(--nx-danger-soft); color: var(--nx-danger); }
.nx-tile__body { flex: 1; min-width: 0; }
.nx-tile__label {
  display: block;
  font-size: var(--nx-text-base);
  font-weight: 500;
  color: var(--nx-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.nx-tile__sub {
  display: block;
  font-size: var(--nx-text-xs);
  color: var(--nx-text-muted);
  padding-top: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.nx-tile__value {
  flex: 0 0 auto;
  font-size: var(--nx-text-md);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
  white-space: nowrap;
  color: var(--nx-text);
}
.nx-tile__value--success { color: var(--nx-success); }
.nx-tile__value--warning { color: var(--nx-warning); }
.nx-tile__value--danger { color: var(--nx-danger); }
.nx-tile__value--brand { color: var(--nx-primary-text); }
</style>
