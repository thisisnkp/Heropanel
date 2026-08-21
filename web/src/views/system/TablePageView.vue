<script setup lang="ts">
/**
 * The three list screens the design draws identically: Domains, Backups and
 * Panel settings.
 *
 * One component keyed by `pageKey` rather than three files that differ only in
 * their fixture — the same arrangement the site and security screens use.
 */
import { computed } from "vue";
import { TABLE_PAGES } from "@/data/system";
import { useUiStore } from "@/stores/ui";

const props = defineProps<{ pageKey: keyof typeof TABLE_PAGES }>();

const ui = useUiStore();
const page = computed(() => TABLE_PAGES[props.pageKey]);
</script>

<template>
  <div class="nx-view">
    <NxPageHeader :title="page.title" :subtitle="page.sub">
      <template #actions>
        <NxButton size="lg" @click="ui.toast(page.action + ' is not wired up yet.', 'info')">
          {{ page.action }}
        </NxButton>
      </template>
    </NxPageHeader>

    <NxCard flush>
      <NxTable
        :columns="[
          { key: 'a', label: page.columns[0], width: '1.8fr' },
          { key: 'b', label: page.columns[1], width: '1.2fr' },
          { key: 'c', label: page.columns[2], width: '1fr' },
          { key: 'actions', label: '', width: '84px', align: 'end' },
        ]"
        :rows="page.rows"
        :row-key="(r) => r.a"
      >
        <template #default="{ row }">
          <div class="tp__key nx-truncate" :class="{ 'nx-mono': page.mono }">{{ row.a }}</div>
          <div class="tp__muted nx-truncate">{{ row.b }}</div>
          <div class="tp__muted nx-truncate">{{ row.c }}</div>
          <div class="tp__actions">
            <NxButton @click="ui.toast('Manage is not wired up yet.', 'info')">Manage</NxButton>
          </div>
        </template>
      </NxTable>
    </NxCard>
  </div>
</template>

<style scoped>
.tp__key { font-weight: 500; }
.tp__muted { color: var(--nx-text-muted); }
.tp__actions { display: flex; justify-content: flex-end; }
</style>
