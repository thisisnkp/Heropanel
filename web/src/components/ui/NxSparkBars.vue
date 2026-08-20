<script setup lang="ts">
/**
 * The 24-bar CPU/memory histogram on the dashboard.
 *
 * Bars above `warnAbove` switch to the warning tone, which is what makes the
 * chart readable at a glance without a legend — the design did this inline per
 * bar; here the threshold is stated once and applied by the component.
 */
import { computed } from "vue";

const props = withDefaults(
  defineProps<{
    /** Percentages, 0–100. */
    series: readonly number[];
    height?: string;
    tone?: "brand" | "php";
    warnAbove?: number;
    label?: string;
  }>(),
  { height: "56px", tone: "brand", warnAbove: 70 },
);

const bars = computed(() =>
  props.series.map((v) => ({
    height: `${Math.min(100, Math.max(0, v))}%`,
    warn: v > props.warnAbove,
  })),
);
</script>

<template>
  <div class="nx-spark" :style="{ height }" role="img" :aria-label="label">
    <div
      v-for="(b, i) in bars"
      :key="i"
      :class="['nx-spark__bar', `nx-spark__bar--${tone}`, { 'is-warn': b.warn }]"
      :style="{ height: b.height }"
    />
  </div>
</template>

<style scoped>
.nx-spark {
  display: flex;
  align-items: flex-end;
  gap: 4px;
}
.nx-spark__bar {
  flex: 1;
  min-width: 0;
  border-radius: 2px 2px 0 0;
  opacity: 0.85;
}
.nx-spark__bar--brand { background: var(--nx-primary); }
.nx-spark__bar--php { background: var(--nx-stack-php); }
.nx-spark__bar.is-warn { background: var(--nx-warning); }
</style>
