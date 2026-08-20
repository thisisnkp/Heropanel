<script setup lang="ts">
/**
 * A bordered message block: the failed-request alert, "this is destructive"
 * warnings, and the informational notes the design puts above forms.
 *
 * `role="alert"` only when the tone is danger — announcing every informational
 * note interrupts a screen reader user mid-sentence for something that is not
 * urgent.
 */
withDefaults(
  defineProps<{
    tone?: "info" | "success" | "warning" | "danger";
    title?: string;
    icon?: string;
  }>(),
  { tone: "info" },
);

const ICON_FOR = { info: "info", success: "check-circle", warning: "error", danger: "error" } as const;
</script>

<template>
  <div :class="['nx-callout', `nx-callout--${tone}`]" :role="tone === 'danger' ? 'alert' : undefined">
    <NxIcon :name="icon ?? ICON_FOR[tone]" size="md" class="nx-callout__icon" />
    <div class="nx-callout__body">
      <p v-if="title" class="nx-callout__title">{{ title }}</p>
      <div v-if="$slots.default" class="nx-callout__text"><slot /></div>
      <div v-if="$slots.actions" class="nx-callout__actions"><slot name="actions" /></div>
    </div>
  </div>
</template>

<style scoped>
.nx-callout {
  display: flex;
  gap: 12px;
  border-radius: var(--nx-radius-md);
  border: 1px solid;
  padding: 16px;
}
.nx-callout__icon { margin-top: 1px; }
.nx-callout__body { flex: 1; min-width: 0; }
.nx-callout__title {
  margin: 0;
  font-size: var(--nx-text-base);
  font-weight: 500;
}
.nx-callout__text {
  font-size: var(--nx-text-sm);
  color: var(--nx-text-2);
  padding-top: 8px;
  line-height: 1.5;
}
.nx-callout__actions {
  display: flex;
  gap: 8px;
  align-items: center;
  padding-top: 12px;
  flex-wrap: wrap;
}
.nx-callout--info { background: var(--nx-info-soft); border-color: var(--nx-primary-border); }
.nx-callout--info .nx-callout__icon, .nx-callout--info .nx-callout__title { color: var(--nx-info); }
.nx-callout--success { background: var(--nx-success-soft); border-color: var(--nx-success); }
.nx-callout--success .nx-callout__icon, .nx-callout--success .nx-callout__title { color: var(--nx-success); }
.nx-callout--warning { background: var(--nx-warning-soft); border-color: var(--nx-warning-border); }
.nx-callout--warning .nx-callout__icon, .nx-callout--warning .nx-callout__title { color: var(--nx-warning); }
.nx-callout--danger { background: var(--nx-danger-soft); border-color: var(--nx-danger-border); }
.nx-callout--danger .nx-callout__icon, .nx-callout--danger .nx-callout__title { color: var(--nx-danger); }
</style>
