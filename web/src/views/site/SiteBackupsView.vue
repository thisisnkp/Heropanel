<script setup lang="ts">
/**
 * Backups — restore this site to any of the last 14 days.
 *
 * Restore asks for confirmation naming the snapshot: it overwrites the live
 * site, and the rows differ only by a date, which is exactly the kind of list
 * where the wrong click looks identical to the right one.
 */
import { computed, ref } from "vue";
import { SITE_BACKUPS } from "@/data/siteDetail";
import { useSitesStore } from "@/stores/sites";
import { useUiStore } from "@/stores/ui";

const sites = useSitesStore();
const ui = useUiStore();

const site = computed(() => sites.current);
const pending = ref<(typeof SITE_BACKUPS)[number] | null>(null);

function confirmRestore() {
  if (!pending.value) return;
  ui.toast("Restoring " + pending.value.when + "…", "info");
  pending.value = null;
}
</script>

<template>
  <div v-if="site">
    <SiteHeader kicker="Data" title="Backups" sub="Restore this site to any of the last 14 days." />

    <div class="nx-view">
      <NxCard title="Daily backups · kept 14 days" flush>
        <template #action>
          <NxButton @click="ui.toast('Backing up ' + site.domain + '…', 'info')">Back up now</NxButton>
        </template>

        <NxTable
          :columns="[
            { key: 'when', label: 'Snapshot', width: '1.2fr' },
            { key: 'kind', label: 'Contents', width: '1fr' },
            { key: 'size', label: 'Size', width: '0.8fr' },
            { key: 'actions', label: '', width: '100px', align: 'end' },
          ]"
          :rows="SITE_BACKUPS"
          :row-key="(b) => b.when"
        >
          <template #default="{ row }">
            <div class="bk__when">{{ row.when }}</div>
            <div class="bk__muted">{{ row.kind }}</div>
            <div class="bk__muted nx-mono">{{ row.size }}</div>
            <div class="bk__actions"><NxButton @click="pending = row">Restore</NxButton></div>
          </template>
        </NxTable>
      </NxCard>
    </div>

    <NxModal
      :open="pending !== null"
      title="Restore this backup?"
      :description="
        'Files and databases for ' + site.domain + ' are replaced with the snapshot from ' + (pending?.when ?? '') + '.'
      "
      @update:open="(v) => { if (!v) pending = null; }"
    >
      <NxCallout tone="warning" title="Anything changed since then is lost">
        A fresh backup of the current state is taken first, so this is reversible for 14 days.
      </NxCallout>

      <template #footer>
        <NxButton @click="pending = null">Cancel</NxButton>
        <NxButton variant="primary" @click="confirmRestore">Restore backup</NxButton>
      </template>
    </NxModal>
  </div>
  <NxSkeleton v-else height="200px" />
</template>

<style scoped>
.bk__when { font-size: var(--nx-text-base); }
.bk__muted { color: var(--nx-text-muted); }
.bk__actions { display: flex; justify-content: flex-end; }
</style>
