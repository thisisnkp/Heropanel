<script setup lang="ts">
/**
 * One row inside a ContextSidebar — a link to a named route, or a button for a
 * destination the router addresses by query parameter (an app category, a
 * domain section) or for a group that expands.
 *
 * Depth is a prop rather than a CSS descendant selector because the design
 * indents by nesting *level*, not by DOM nesting: a group's children are
 * siblings in the list so that a collapsed group does not leave an empty <ul>
 * behind, and the indent still has to survive that.
 */
withDefaults(
  defineProps<{
    label: string;
    icon: string;
    /** Route name. Omit for a button — see `activate`. */
    to?: string;
    params?: Record<string, string>;
    query?: Record<string, string>;
    /** Opens in its own window — the file manager is a separate tool, not a page. */
    newTab?: boolean;
    current?: boolean;
    depth?: 0 | 1 | 2;
    /** Draws the disclosure caret. Only meaningful on a button. */
    expandable?: boolean;
    expanded?: boolean;
    /** The danger zone reads red even when it is not the current section. */
    tone?: "default" | "danger";
  }>(),
  { depth: 0, current: false, expandable: false, expanded: false, tone: "default", newTab: false },
);

/** Fired by the button form: expand the group, or go to the query-param screen. */
const emit = defineEmits<{ (e: "activate"): void }>();
</script>

<template>
  <RouterLink
    v-if="to"
    :to="{ name: to, params, query }"
    class="nx-row"
    :class="[`is-depth-${depth}`, { 'is-current': current, 'is-danger': tone === 'danger' }]"
    :aria-current="current && !newTab ? 'page' : undefined"
    :target="newTab ? '_blank' : undefined"
    :rel="newTab ? 'noopener' : undefined"
  >
    <NxIcon :name="icon" size="md" class="nx-row__icon" />
    <span class="nx-row__label">{{ label }}</span>
    <!-- Says so before it is clicked: a link that steals a new tab without
         warning reads as the app having lost the one you were in. -->
    <NxIcon v-if="newTab" name="open-in-new" size="sm" class="nx-row__ext" />
    <span v-if="$slots.default" class="nx-row__trail"><slot /></span>
  </RouterLink>

  <button
    v-else
    type="button"
    class="nx-row"
    :class="[`is-depth-${depth}`, { 'is-current': current, 'is-danger': tone === 'danger' }]"
    :aria-expanded="expandable ? expanded : undefined"
    :aria-current="current && !expandable ? 'page' : undefined"
    @click="emit('activate')"
  >
    <NxIcon :name="icon" size="md" class="nx-row__icon" />
    <span class="nx-row__label">{{ label }}</span>
    <NxIcon
      v-if="expandable"
      :name="expanded ? 'arrow-drop-down' : 'arrow-right'"
      size="md"
      class="nx-row__caret"
    />
  </button>
</template>

<style scoped>
.nx-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  text-align: left;
  border: 0;
  background: transparent;
  cursor: pointer;
  padding: 8px 12px;
  border-radius: var(--nx-radius-md);
  font-size: var(--nx-text-base);
  font-family: inherit;
  color: var(--nx-text-3);
  transition: background 130ms ease, color 130ms ease;
  animation: nxRow 160ms ease both;
}
.nx-row.is-depth-1 { padding-left: 24px; font-size: 13px; }
.nx-row.is-depth-2 { padding-left: 38px; font-size: 13px; }
.nx-row:hover { background: var(--nx-hover); }
.nx-row.is-current { background: var(--nx-hover); color: var(--nx-text); font-weight: 600; }
.nx-row__icon { color: var(--nx-text-muted); transition: color 130ms ease; }
.nx-row.is-current .nx-row__icon { color: var(--nx-primary); }
.nx-row.is-danger { color: var(--nx-danger); }
.nx-row.is-danger .nx-row__icon { color: var(--nx-danger-border); }
.nx-row.is-danger.is-current .nx-row__icon { color: var(--nx-danger); }
.nx-row__label { flex: 1; min-width: 0; }
.nx-row__caret { color: var(--nx-text-placeholder); }
.nx-row__trail { font-size: var(--nx-text-xs); color: var(--nx-text-muted); }
.nx-row__ext { color: var(--nx-text-placeholder); }
</style>
