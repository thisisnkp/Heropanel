<script setup lang="ts">
/** Compose — multi-container stacks defined by a compose file. */
import { COMPOSE_STACKS, COMPOSE_YAML } from "@/data/system";
import { useUiStore } from "@/stores/ui";

const ui = useUiStore();
</script>

<template>
  <div class="nx-view">
    <NxPageHeader title="Compose" subtitle="Multi-container stacks, brought up and down as one unit.">
      <template #actions>
        <NxButton variant="primary" size="lg" @click="ui.toast('New stack is not wired up yet.', 'info')">
          New stack
        </NxButton>
      </template>
    </NxPageHeader>

    <NxCard title="Stacks" flush class="cmp__block">
      <NxTable
        :columns="[
          { key: 'name', label: 'Stack', width: '1.2fr' },
          { key: 'path', label: 'Path', width: '1.8fr' },
          { key: 'services', label: 'Services', width: '0.8fr' },
          { key: 'actions', label: '', width: '150px', align: 'end' },
        ]"
        :rows="COMPOSE_STACKS"
        :row-key="(s) => s.name"
      >
        <template #default="{ row }">
          <div class="cmp__name nx-mono nx-truncate">{{ row.name }}</div>
          <div class="cmp__muted nx-mono nx-truncate">{{ row.path }}</div>
          <div class="cmp__muted">{{ row.services }}</div>
          <div class="cmp__actions">
            <NxBadge :tone="row.state === 'Running' ? 'success' : 'neutral'">{{ row.state }}</NxBadge>
            <NxButton @click="ui.toast(row.action + ' is not wired up yet.', 'info')">{{ row.action }}</NxButton>
          </div>
        </template>
      </NxTable>
    </NxCard>

    <NxLogPanel title="novaretail-stack · compose.yaml" :lines="COMPOSE_YAML.map((text) => ({ text }))">
      <template #actions>
        <NxButton size="sm" class="cmp__dark-btn" @click="ui.toast('Validate is not wired up yet.', 'info')">
          Validate
        </NxButton>
        <NxButton size="sm" class="cmp__dark-btn" @click="ui.toast('Edit is not wired up yet.', 'info')">Edit</NxButton>
      </template>
    </NxLogPanel>
  </div>
</template>

<style scoped>
.cmp__block { margin-bottom: 12px; }
.cmp__name { font-weight: 500; }
.cmp__muted { color: var(--nx-text-muted); }
.cmp__actions { display: flex; gap: 8px; align-items: center; justify-content: flex-end; }
.cmp__dark-btn {
  border-color: var(--nx-dark-border-2);
  background: transparent;
  color: var(--nx-text-on-dark);
}
.cmp__dark-btn:hover { background: var(--nx-dark-border); }
</style>
