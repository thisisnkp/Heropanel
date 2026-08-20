<script setup lang="ts">
/**
 * The twenty-two site screens that share the design's generic layout.
 *
 * One component parameterised by `specKey` rather than twenty-two files that
 * differ only in a string: the router names the screen, `siteSpec.ts` supplies
 * its content, and the layout lives in exactly one place. A screen that stops
 * fitting the shape gets its own component — that is a deliberate decision, not
 * something that happens by accident when someone edits a copy.
 */
import { computed } from "vue";
import { buildSiteSpec, type SpecKey } from "@/data/siteSpec";
import { useSitesStore } from "@/stores/sites";

const props = defineProps<{ specKey: SpecKey }>();

const sites = useSitesStore();
const spec = computed(() => (sites.current ? buildSiteSpec(props.specKey, sites.current) : null));
</script>

<template>
  <div v-if="spec">
    <SiteHeader :kicker="spec.kicker" :title="spec.title" :sub="spec.sub" />
    <SiteSpecScreen :spec="spec" />
  </div>
  <NxSkeleton v-else height="200px" />
</template>
