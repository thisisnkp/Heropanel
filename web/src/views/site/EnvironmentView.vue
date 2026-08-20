<script setup lang="ts">
/**
 * Environment variables — secrets and config, injected at start.
 *
 * Values arrive already masked from the server, and there is deliberately no
 * "reveal" here: the panel never receives the plaintext, so a reveal control
 * could only ever un-mask a string of bullets. Editing sends a new value; it
 * does not read the old one back.
 */
import { computed } from "vue";
import { ENV_VARS } from "@/data/siteDetail";
import { useSitesStore } from "@/stores/sites";
import { useUiStore } from "@/stores/ui";

const sites = useSitesStore();
const ui = useUiStore();

const site = computed(() => sites.current);
</script>

<template>
  <div v-if="site">
    <SiteHeader kicker="Runtime" title="Environment variables" sub="Secrets and config, injected at start." />

    <div class="nx-view">
      <NxCard title="Environment variables" flush>
        <template #action>
          <NxButton variant="primary" @click="ui.toast('Add variable is not wired up yet.', 'info')">
            Add variable
          </NxButton>
        </template>

        <NxTable
          :columns="[
            { key: 'k', label: 'Name', width: '1fr' },
            { key: 'v', label: 'Value', width: '1.4fr' },
            { key: 'actions', label: '', width: '90px', align: 'end' },
          ]"
          :rows="ENV_VARS"
          :row-key="(e) => e.label"
        >
          <template #default="{ row }">
            <div class="env__key nx-mono nx-truncate">{{ row.label }}</div>
            <div class="env__value nx-mono nx-truncate">{{ row.value }}</div>
            <div class="env__actions">
              <NxButton @click="ui.toast('Edit is not wired up yet.', 'info')">Edit</NxButton>
            </div>
          </template>
        </NxTable>

        <p class="env__foot">Changes apply on the next restart.</p>
      </NxCard>
    </div>
  </div>
  <NxSkeleton v-else height="200px" />
</template>

<style scoped>
.env__key { font-weight: 500; }
.env__value { color: var(--nx-text-muted); }
.env__actions { display: flex; gap: 8px; justify-content: flex-end; }
.env__foot {
  margin: 0;
  padding: 12px 16px;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
}
</style>
