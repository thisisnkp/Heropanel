<script setup lang="ts">
/** Databases — the MySQL databases attached to this site. */
import { computed } from "vue";
import { databases } from "@/data/siteDetail";
import { useSitesStore } from "@/stores/sites";
import { useUiStore } from "@/stores/ui";

const sites = useSitesStore();
const ui = useUiStore();

const site = computed(() => sites.current);
const rows = computed(() => (site.value ? databases(site.value) : []));
</script>

<template>
  <div v-if="site">
    <SiteHeader kicker="Data" title="Databases" sub="MySQL databases attached to this site." />

    <div class="nx-view">
      <NxCard title="MySQL databases" flush>
        <template #action>
          <NxButton @click="$router.push({ name: 'site-phpmyadmin' })">Open phpMyAdmin</NxButton>
        </template>

        <NxTable
          :columns="[
            { key: 'name', label: 'Database', width: '1.4fr' },
            { key: 'user', label: 'User', width: '1fr' },
            { key: 'size', label: 'Size', width: '0.7fr' },
            { key: 'actions', label: '', width: '90px', align: 'end' },
          ]"
          :rows="rows"
          :row-key="(d) => d.name"
        >
          <template #default="{ row }">
            <div class="db__name nx-mono nx-truncate">{{ row.name }}</div>
            <div class="db__muted nx-mono">{{ row.user }}</div>
            <div class="db__muted">{{ row.size }}</div>
            <div class="db__actions">
              <NxButton @click="ui.toast('Export is not wired up yet.', 'info')">Export</NxButton>
            </div>
          </template>
        </NxTable>
      </NxCard>
    </div>
  </div>
  <NxSkeleton v-else height="200px" />
</template>

<style scoped>
.db__name { font-weight: 500; }
.db__muted { color: var(--nx-text-muted); }
.db__actions { display: flex; justify-content: flex-end; }
</style>
