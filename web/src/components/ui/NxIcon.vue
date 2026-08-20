<script setup lang="ts">
/**
 * Renders a registry icon by name, at a token size.
 *
 * Unknown names render nothing rather than a broken glyph or a fallback box —
 * a missing icon should be invisible in production and obvious in review, not a
 * grey square that looks deliberate.
 */
import { computed } from "vue";
import { ICONS } from "./icons";

const props = withDefaults(
  defineProps<{
    name: string;
    size?: "sm" | "md" | "lg" | "xl";
    /** Decorative by default; pass a label when the icon is the only content. */
    label?: string;
  }>(),
  { size: "md" },
);

const component = computed(() => ICONS[props.name]);
</script>

<template>
  <component
    :is="component"
    v-if="component"
    :class="['nx-icon', `nx-icon--${size}`]"
    :aria-hidden="label ? undefined : true"
    :aria-label="label"
    :role="label ? 'img' : undefined"
  />
</template>

<style scoped>
.nx-icon { flex: 0 0 auto; display: block; }
.nx-icon--sm { width: var(--nx-icon-sm); height: var(--nx-icon-sm); }
.nx-icon--md { width: var(--nx-icon-md); height: var(--nx-icon-md); }
.nx-icon--lg { width: var(--nx-icon-lg); height: var(--nx-icon-lg); }
.nx-icon--xl { width: var(--nx-icon-xl); height: var(--nx-icon-xl); }
</style>
