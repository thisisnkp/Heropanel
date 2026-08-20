<script setup lang="ts">
/** The title block every screen opens with. */
defineProps<{ title: string; subtitle?: string }>();
</script>

<template>
  <header class="nx-page-head">
    <div class="nx-page-head__titles">
      <h1 class="nx-page-head__title">{{ title }}</h1>
      <p v-if="subtitle" class="nx-page-head__sub">{{ subtitle }}</p>
    </div>
    <div v-if="$slots.actions" class="nx-page-head__actions"><slot name="actions" /></div>
  </header>
</template>

<style scoped>
.nx-page-head {
  display: flex;
  align-items: flex-start;
  gap: 16px;
  margin-bottom: 20px;
}
.nx-page-head__titles { flex: 1; min-width: 0; }
.nx-page-head__title {
  margin: 0;
  font-size: var(--nx-text-xl);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
  color: var(--nx-text);
}
.nx-page-head__sub {
  margin: 4px 0 0;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
}
.nx-page-head__actions { display: flex; gap: 8px; flex: 0 0 auto; }
</style>
