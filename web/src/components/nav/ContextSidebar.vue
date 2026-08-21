<script setup lang="ts">
/**
 * The 236px scoped sidebar, shared by every context that replaces the global
 * navigation: a website, a DNS zone, Security, Apps.
 *
 * The design draws these four the same way on purpose — a way out at the top, a
 * heading or switcher naming what you are inside, the sections of that thing,
 * and a footer card explaining why the list looks the way it does. Writing them
 * as four components meant four chances for the paddings to drift; writing the
 * chrome once means a context is a title, a list and a note.
 */
defineProps<{
  /** Where the escape hatch goes, and what it says. */
  backTo: string;
  backLabel: string;
  /** Heading shown when the context has no switcher. */
  title?: string;
  /** Named `navLabel`, not `ariaLabel`: the latter collides with the plain
   *  aria-label attribute at every call site and silently becomes an attr. */
  navLabel: string;
  footerCaption?: string;
  footerText?: string;
}>();
</script>

<template>
  <aside class="nx-ctx nxhide" :aria-label="navLabel">
    <RouterLink :to="{ name: backTo }" class="nx-ctx__back">
      <span class="nx-ctx__arrow" aria-hidden="true">&larr;</span>
      <span>{{ backLabel }}</span>
    </RouterLink>

    <!-- A switcher when the context is one of many, a heading when it is not. -->
    <slot name="top">
      <div v-if="title" class="nx-ctx__title">{{ title }}</div>
    </slot>

    <div v-if="$slots.chips" class="nx-ctx__chips">
      <slot name="chips" />
    </div>

    <div class="nx-ctx__nav">
      <slot />
    </div>

    <div class="nx-ctx__spacer" />

    <div v-if="footerCaption || $slots.footer" class="nx-ctx__footer">
      <slot name="footer">
        <div class="nx-ctx__footer-caption">{{ footerCaption }}</div>
        <p class="nx-ctx__footer-text">{{ footerText }}</p>
      </slot>
    </div>
  </aside>
</template>

<style scoped>
.nx-ctx {
  width: 236px;
  flex: 0 0 236px;
  background: var(--nx-surface);
  border-right: 1px solid var(--nx-border);
  display: flex;
  flex-direction: column;
  padding: 20px 16px 16px;
  overflow-y: auto;
  min-height: 0;
  animation: nxSidebar 220ms cubic-bezier(0.16, 1, 0.3, 1) both;
}
.nx-ctx__back {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 8px 16px;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
  white-space: nowrap;
}
.nx-ctx__back:hover { color: var(--nx-text); }
.nx-ctx__arrow { font-family: "JetBrains Mono", ui-monospace, monospace; }
.nx-ctx__title {
  padding: 0 8px 4px;
  font-size: var(--nx-text-md);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
  color: var(--nx-text);
}
.nx-ctx__chips { display: flex; align-items: center; gap: 6px; padding: 0 8px 16px; flex-wrap: wrap; }
.nx-ctx__nav { display: flex; flex-direction: column; gap: 4px; }
.nx-ctx__spacer { flex: 1; min-height: 16px; }
.nx-ctx__footer {
  border: 1px solid var(--nx-active);
  border-radius: var(--nx-radius-md);
  padding: 12px;
  background: var(--nx-surface-2);
}
.nx-ctx__footer-caption {
  font-size: var(--nx-text-xs);
  color: var(--nx-text-muted);
  letter-spacing: var(--nx-ls-caps);
}
.nx-ctx__footer-text {
  margin: 0;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
  line-height: 1.5;
  padding-top: 6px;
  text-wrap: pretty;
}
</style>
