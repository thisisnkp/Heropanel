<script setup lang="ts">
/** The dark log surface: site logs, deploy output, scan results. */
export interface NxLogLine {
  /** Timestamp column, kept narrow and fixed so the messages align. */
  readonly time?: string;
  readonly text: string;
  /** A token colour; defaults to the panel's body colour. */
  readonly color?: string;
}

withDefaults(
  defineProps<{
    title: string;
    lines: readonly NxLogLine[];
    /** Green when the source is live-tailing. */
    live?: boolean;
  }>(),
  { live: false },
);
</script>

<template>
  <div class="nx-log">
    <header class="nx-log__head">
      <span :class="['nx-log__dot', { 'is-live': live }]" aria-hidden="true" />
      <div class="nx-log__title">{{ title }}</div>
      <slot name="actions" />
    </header>
    <div class="nx-log__body nxscroll">
      <div v-for="(l, i) in lines" :key="i" class="nx-log__line">
        <span v-if="l.time" class="nx-log__time">{{ l.time }}</span>
        <span :style="{ color: l.color }">{{ l.text }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.nx-log {
  background: var(--nx-text);
  border-radius: var(--nx-radius-lg);
  overflow: hidden;
}
.nx-log__head {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--nx-dark-border);
}
.nx-log__dot {
  width: 7px;
  height: 7px;
  border-radius: var(--nx-radius-full);
  background: var(--nx-text-muted);
}
.nx-log__dot.is-live { background: var(--nx-success-on-dark); }
.nx-log__title {
  flex: 1;
  font-size: var(--nx-text-base);
  color: var(--nx-text-on-dark);
  font-family: "JetBrains Mono", ui-monospace, monospace;
}
.nx-log__body {
  padding: 16px;
  max-height: 420px;
  overflow: auto;
  font-family: "JetBrains Mono", ui-monospace, monospace;
  font-size: var(--nx-text-base);
  line-height: 1.85;
  color: var(--nx-text-on-dark);
}
.nx-log__line { display: flex; gap: 12px; }
.nx-log__time { color: var(--nx-text-muted); flex: 0 0 62px; }
</style>
