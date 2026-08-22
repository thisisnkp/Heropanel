<script setup lang="ts">
/**
 * Everything under /sites/:uid renders through here.
 *
 * Its one job is to make the route param authoritative: the shell, the drawer
 * and the header all read `sites.current`, and they mount before any child
 * screen does. Setting it here — and re-setting it when the param changes —
 * means a deep link, a back button and a site switch all arrive with the right
 * site already selected, instead of each screen doing its own lookup and the
 * chrome briefly showing the previous one.
 */
import { computed, onBeforeUnmount, watch } from "vue";
import { useSitesStore } from "@/stores/sites";

const props = defineProps<{ uid: string }>();
const sites = useSitesStore();

watch(
  () => props.uid,
  (uid) => sites.setCurrent(uid),
  { immediate: true },
);

// Leaving the site scope clears it, so the topbar breadcrumb does not keep
// claiming you are inside a site you have navigated away from.
onBeforeUnmount(() => sites.setCurrent(null));

const missing = computed(() => !sites.loading && sites.sites.length > 0 && sites.current === null);
</script>

<template>
  <div>
    <NxEmptyState
      v-if="missing"
      icon="error"
      title="No such site"
      description="It may have been deleted, or the link may be out of date."
    >
      <NxButton variant="primary" @click="$router.push({ name: 'websites' })">Back to websites</NxButton>
    </NxEmptyState>

    <RouterView v-else v-slot="{ Component }">
      <component :is="Component" />
    </RouterView>
  </div>
</template>
