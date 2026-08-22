<script setup lang="ts">
/**
 * The frame every pre-session screen shares: login, first-run bootstrap, and the
 * "no datastore" explanation.
 *
 * It is a component rather than three copies of the same markup because these
 * screens are the only place a stranger sees the panel, and a login form that
 * sits two pixels off from the bootstrap form it just replaced reads as a
 * different site. There is no chrome here at all — no rail, no sidebar, nothing
 * to navigate to — so the shell is the whole layout.
 */
defineProps<{
  title: string;
  subtitle?: string;
}>();
</script>

<template>
  <main id="nx-main" class="auth">
    <section class="auth__card">
      <header class="auth__head">
        <p class="auth__brand">NexPanel</p>
        <h1 class="auth__title">{{ title }}</h1>
        <p v-if="subtitle" class="auth__sub">{{ subtitle }}</p>
      </header>

      <slot />

      <footer v-if="$slots.footer" class="auth__foot"><slot name="footer" /></footer>
    </section>
  </main>
</template>

<style scoped>
.auth {
  min-height: 100dvh;
  display: grid;
  place-items: center;
  padding: 24px;
  background: var(--nx-bg);
}
.auth__card {
  width: 100%;
  max-width: 400px;
  display: flex;
  flex-direction: column;
  gap: 20px;
  padding: 28px;
  background: var(--nx-surface);
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-lg);
}
.auth__head { display: flex; flex-direction: column; gap: 6px; }
.auth__brand {
  margin: 0;
  font-size: var(--nx-text-sm);
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--nx-primary);
}
.auth__title { margin: 0; font-size: var(--nx-text-xl); font-weight: 600; color: var(--nx-text); }
.auth__sub { margin: 0; font-size: var(--nx-text-base); color: var(--nx-text-muted); }
.auth__foot {
  padding-top: 16px;
  border-top: 1px solid var(--nx-border);
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
}
</style>
