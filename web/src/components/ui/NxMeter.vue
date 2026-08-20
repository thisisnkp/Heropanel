<script setup lang="ts">
/**
 * A labelled progress bar — resource usage, quota, disk, scan progress.
 *
 * A real <progress> element underneath would bring the semantics for free, but
 * it cannot be styled to the design's 7px track consistently across engines, so
 * the ARIA meter roles are supplied explicitly instead.
 */
withDefaults(
  defineProps<{
    /** 0–100. Values outside the range are clamped rather than overflowing the track. */
    value: number;
    label?: string;
    /** Right-aligned figure on the label row, e.g. "38 GB / 200 GB". */
    valueText?: string;
    /** Muted line under the bar. */
    note?: string;
    tone?: "brand" | "success" | "warning" | "danger" | "neutral";
    height?: string;
  }>(),
  { tone: "brand", height: "7px" },
);
</script>

<template>
  <div class="nx-meter">
    <div v-if="label || valueText" class="nx-meter__head">
      <span class="nx-meter__label">{{ label }}</span>
      <span v-if="valueText" class="nx-meter__value">{{ valueText }}</span>
    </div>
    <div
      class="nx-meter__track"
      :style="{ height }"
      role="meter"
      :aria-valuenow="Math.round(value)"
      aria-valuemin="0"
      aria-valuemax="100"
      :aria-label="label"
    >
      <div
        :class="['nx-meter__fill', `nx-meter__fill--${tone}`]"
        :style="{ width: `${Math.min(100, Math.max(0, value))}%` }"
      />
    </div>
    <div v-if="note" class="nx-meter__note">{{ note }}</div>
  </div>
</template>

<style scoped>
.nx-meter__head {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding-bottom: 8px;
}
.nx-meter__label {
  flex: 1;
  min-width: 0;
  font-size: var(--nx-text-base);
  font-weight: 500;
  color: var(--nx-text);
}
.nx-meter__value {
  font-size: var(--nx-text-base);
  color: var(--nx-text-2);
  font-family: "JetBrains Mono", ui-monospace, monospace;
  white-space: nowrap;
}
.nx-meter__track {
  border-radius: var(--nx-radius-sm);
  background: var(--nx-hover);
  overflow: hidden;
}
.nx-meter__fill {
  height: 100%;
  border-radius: var(--nx-radius-sm);
  transition: width 260ms cubic-bezier(0.16, 1, 0.3, 1);
}
.nx-meter__fill--brand { background: var(--nx-primary); }
.nx-meter__fill--success { background: var(--nx-success); }
.nx-meter__fill--warning { background: var(--nx-warning); }
.nx-meter__fill--danger { background: var(--nx-danger); }
.nx-meter__fill--neutral { background: var(--nx-text); }
.nx-meter__note {
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
  padding-top: 6px;
}
</style>
