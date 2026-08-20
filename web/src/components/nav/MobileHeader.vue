<script setup lang="ts">
/**
 * Compact mobile header. Inside a site it becomes a back button plus the site
 * domain, because on a phone the site drawer is a pushed screen rather than a
 * persistent column — so the header is the only thing telling you which site
 * you are editing.
 */
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useSitesStore } from "@/stores/sites";

const route = useRoute();
const router = useRouter();
const sites = useSitesStore();

const inSite = computed(() => String(route.name ?? "").startsWith("site"));

const title = computed(() => {
  if (inSite.value && sites.current) return sites.current.domain;
  const matched = [...route.matched].reverse().find((r) => r.meta.title);
  return (matched?.meta.title as string | undefined) ?? "NexPanel";
});
</script>

<template>
  <header class="nx-mheader">
    <button v-if="inSite" type="button" class="nx-mheader__icon" aria-label="Back" @click="router.back()">
      <NxIcon name="arrow-back" size="lg" />
    </button>
    <div v-else class="nx-mheader__mark" aria-hidden="true">N</div>

    <h1 class="nx-mheader__title">{{ title }}</h1>

    <button type="button" class="nx-mheader__icon" aria-label="Search">
      <NxIcon name="search" size="lg" />
    </button>
  </header>
</template>

<style scoped>
.nx-mheader {
  position: sticky;
  top: 0;
  z-index: 30;
  display: flex;
  align-items: center;
  gap: 12px;
  height: 56px;
  padding: 0 12px;
  background: rgba(246, 246, 244, 0.94);
  backdrop-filter: blur(8px);
  border-bottom: 1px solid var(--nx-border);
}
.nx-mheader__title {
  flex: 1;
  min-width: 0;
  margin: 0;
  font-size: var(--nx-text-md);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
  color: var(--nx-text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.nx-mheader__icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  flex: 0 0 40px;
  border: 0;
  background: transparent;
  border-radius: var(--nx-radius-md);
  color: var(--nx-text-2);
  cursor: pointer;
}
.nx-mheader__icon:hover { background: var(--nx-hover); }
.nx-mheader__mark {
  width: 28px;
  height: 28px;
  flex: 0 0 28px;
  border-radius: var(--nx-radius-md);
  background: var(--nx-violet-900);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--nx-gold-400);
  font-size: var(--nx-text-md);
  font-weight: 600;
}
</style>
