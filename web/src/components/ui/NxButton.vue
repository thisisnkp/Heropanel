<script setup lang="ts">
/**
 * The one button. Variants match the four the design actually uses; a fifth
 * would mean the design grew one, not that a screen needed a tweak.
 *
 * Hover/active are CSS classes rather than the design's inline `style-hover`
 * attribute, because inline styles cannot express a pseudo-class and the
 * prototype's attribute was a renderer feature, not real CSS.
 */
withDefaults(
  defineProps<{
    variant?: "default" | "primary" | "danger" | "ghost";
    size?: "sm" | "md" | "lg";
    disabled?: boolean;
    /** Renders a spinner and blocks clicks without changing the button's width. */
    loading?: boolean;
    type?: "button" | "submit";
  }>(),
  { variant: "default", size: "md", disabled: false, loading: false, type: "button" },
);
</script>

<template>
  <button
    :type="type"
    :class="['nx-btn', `nx-btn--${variant}`, `nx-btn--${size}`, { 'is-loading': loading }]"
    :disabled="disabled || loading"
    :aria-busy="loading || undefined"
  >
    <span v-if="loading" class="nx-btn__spinner" aria-hidden="true" />
    <slot />
  </button>
</template>

<style scoped>
.nx-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  font-family: inherit;
  font-size: var(--nx-text-base);
  font-weight: 500;
  border-radius: var(--nx-radius-md);
  cursor: pointer;
  white-space: nowrap;
  transition: background 120ms ease, border-color 120ms ease, color 120ms ease;
}
.nx-btn:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

.nx-btn--sm { padding: 4px 10px; font-size: var(--nx-text-sm); }
.nx-btn--md { padding: 6px 12px; }
.nx-btn--lg { padding: 12px 16px; }

.nx-btn--default {
  border: 1px solid var(--nx-border);
  background: var(--nx-surface);
  color: var(--nx-text-2);
}
.nx-btn--default:hover:not(:disabled) { background: var(--nx-hover); }
.nx-btn--default:active:not(:disabled) { background: var(--nx-active); }

.nx-btn--primary {
  border: 0;
  background: var(--nx-primary);
  color: var(--nx-primary-on);
}
.nx-btn--primary:hover:not(:disabled) { background: var(--nx-primary-hover); }
.nx-btn--primary:active:not(:disabled) { background: var(--nx-primary-active); }

.nx-btn--danger {
  border: 1px solid var(--nx-border);
  background: var(--nx-surface);
  color: var(--nx-danger);
}
.nx-btn--danger:hover:not(:disabled) {
  background: var(--nx-danger-soft);
  border-color: var(--nx-danger-border);
}

.nx-btn--ghost {
  border: 0;
  background: transparent;
  color: var(--nx-text-2);
}
.nx-btn--ghost:hover:not(:disabled) { background: var(--nx-hover); }

.nx-btn__spinner {
  width: 12px;
  height: 12px;
  border: 2px solid currentColor;
  border-right-color: transparent;
  border-radius: var(--nx-radius-full);
  animation: nxSpin 600ms linear infinite;
}
@keyframes nxSpin { to { transform: rotate(360deg); } }
</style>
