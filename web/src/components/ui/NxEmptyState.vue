<script setup lang="ts">
/** Shown when a list has nothing in it — with the action that fills it. */
defineProps<{ icon?: string; title: string; description?: string }>();
</script>

<template>
  <div class="nx-empty">
    <NxIcon v-if="icon" :name="icon" size="xl" class="nx-empty__icon" />
    <h3 class="nx-empty__title">{{ title }}</h3>
    <p v-if="description" class="nx-empty__desc">{{ description }}</p>
    <div v-if="$slots.default" class="nx-empty__action"><slot /></div>
  </div>
</template>

<style scoped>
.nx-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  padding: 48px 24px;
}
.nx-empty__icon { color: var(--nx-text-placeholder); margin-bottom: 12px; }
.nx-empty__title {
  margin: 0;
  font-size: var(--nx-text-md);
  font-weight: 600;
  color: var(--nx-text);
}
.nx-empty__desc {
  margin: 6px 0 0;
  max-width: 44ch;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
}
.nx-empty__action { margin-top: 16px; }
</style>
