<script setup lang="ts">
/**
 * The surface every panel sits on. `title`/`action` are the header row the
 * design repeats on ~30 screens; omit both and you get a bare surface.
 */
withDefaults(
  defineProps<{
    title?: string;
    /** Muted line under the title. */
    subtitle?: string;
    /** Removes the body padding, for tables that draw their own rows edge to edge. */
    flush?: boolean;
  }>(),
  { flush: false },
);
</script>

<template>
  <section class="nx-card">
    <header v-if="title || $slots.action" class="nx-card__head">
      <div class="nx-card__titles">
        <h2 v-if="title" class="nx-card__title">{{ title }}</h2>
        <p v-if="subtitle" class="nx-card__sub">{{ subtitle }}</p>
      </div>
      <div v-if="$slots.action" class="nx-card__action"><slot name="action" /></div>
    </header>
    <div :class="['nx-card__body', { 'is-flush': flush }]"><slot /></div>
  </section>
</template>

<style scoped>
.nx-card {
  background: var(--nx-surface);
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-lg);
  overflow: hidden;
}
.nx-card__head {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--nx-active);
  background: var(--nx-surface-2);
}
.nx-card__titles { flex: 1; min-width: 0; }
.nx-card__title {
  margin: 0;
  font-size: var(--nx-text-base);
  font-weight: 600;
  color: var(--nx-text);
}
.nx-card__sub {
  margin: 2px 0 0;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
}
.nx-card__action { flex: 0 0 auto; display: flex; gap: 8px; }
.nx-card__body { padding: 16px; }
.nx-card__body.is-flush { padding: 0; }
</style>
