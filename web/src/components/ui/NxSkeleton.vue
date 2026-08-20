<script setup lang="ts">
/** Loading placeholder using the design's shimmer. */
withDefaults(defineProps<{ width?: string; height?: string; rounded?: boolean }>(), {
  width: "100%",
  height: "14px",
  rounded: false,
});
</script>

<template>
  <span
    class="nx-skel"
    :style="{ width, height, display: 'block', borderRadius: rounded ? 'var(--nx-radius-pill)' : undefined }"
    aria-hidden="true"
  />
</template>
