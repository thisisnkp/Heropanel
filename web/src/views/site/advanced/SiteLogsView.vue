<script setup lang="ts">
/** Logs — live tail of the application and error log for this site. */
import { computed } from "vue";
import { SITE_LOG_LINES, logName } from "@/data/siteDetail";
import { useSitesStore } from "@/stores/sites";
import { useUiStore } from "@/stores/ui";

const sites = useSitesStore();
const ui = useUiStore();

const site = computed(() => sites.current);
</script>

<template>
  <div v-if="site">
    <SiteHeader kicker="Diagnostics" title="Logs" sub="Live tail of the application and error log." />

    <div class="nx-view">
      <NxLogPanel :title="logName(site)" :lines="SITE_LOG_LINES" live>
        <template #actions>
          <NxButton size="sm" class="logs__btn" @click="ui.toast('Download is not wired up yet.', 'info')">
            Download
          </NxButton>
        </template>
      </NxLogPanel>
    </div>
  </div>
  <NxSkeleton v-else height="200px" />
</template>

<style scoped>
.logs__btn {
  border-color: var(--nx-dark-border-2);
  background: transparent;
  color: var(--nx-text-on-dark);
}
.logs__btn:hover { background: var(--nx-dark-border); }
</style>
