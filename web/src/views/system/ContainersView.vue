<script setup lang="ts">
/** Containers — the Docker engine, its containers and its images. */
import { CONTAINERS, DOCKER_STATS, IMAGES } from "@/data/system";
import { useUiStore } from "@/stores/ui";

const ui = useUiStore();
</script>

<template>
  <div class="nx-view">
    <NxPageHeader title="Containers" subtitle="Docker containers and images running beside your websites.">
      <template #actions>
        <NxButton size="lg" @click="ui.toast('Pull image is not wired up yet.', 'info')">Pull image</NxButton>
        <NxButton variant="primary" size="lg" @click="ui.toast('Run container is not wired up yet.', 'info')">
          Run container
        </NxButton>
      </template>
    </NxPageHeader>

    <div class="nx-grid nx-grid--4 dk__block">
      <NxStat v-for="s in DOCKER_STATS" :key="s.label" :label="s.label" :value="s.value" :sub="s.sub" />
    </div>

    <NxCard title="Containers" flush class="dk__block">
      <NxTable
        :columns="[
          { key: 'name', label: 'Name', width: '1.3fr' },
          { key: 'image', label: 'Image', width: '1.3fr' },
          { key: 'ports', label: 'Ports', width: '1fr' },
          { key: 'usage', label: 'CPU / memory', width: '0.9fr' },
          { key: 'actions', label: '', width: '150px', align: 'end' },
        ]"
        :rows="CONTAINERS"
        :row-key="(c) => c.name"
      >
        <template #default="{ row }">
          <div class="dk__name nx-mono nx-truncate">{{ row.name }}</div>
          <div class="dk__muted nx-mono nx-truncate">{{ row.image }}</div>
          <div class="dk__muted nx-mono nx-truncate">{{ row.ports }}</div>
          <div class="dk__muted nx-mono">{{ row.cpu }} · {{ row.mem }}</div>
          <div class="dk__actions">
            <NxBadge :tone="row.state === 'Running' ? 'success' : 'neutral'">{{ row.state }}</NxBadge>
            <NxButton @click="ui.toast((row.state === 'Running' ? 'Stop' : 'Start') + ' is not wired up yet.', 'info')">
              {{ row.state === "Running" ? "Stop" : "Start" }}
            </NxButton>
          </div>
        </template>
      </NxTable>
    </NxCard>

    <NxCard title="Images" flush>
      <template #action>
        <NxButton @click="ui.toast('Prune is not wired up yet.', 'info')">Prune unused</NxButton>
      </template>
      <NxTable
        :columns="[
          { key: 'name', label: 'Image', width: '1.6fr' },
          { key: 'size', label: 'Size', width: '0.8fr' },
          { key: 'used', label: 'Used by', width: '1fr' },
          { key: 'age', label: 'Pulled', width: '1fr' },
        ]"
        :rows="IMAGES"
        :row-key="(i) => i.name"
      >
        <template #default="{ row }">
          <div class="dk__name nx-mono nx-truncate">{{ row.name }}</div>
          <div class="dk__muted nx-mono">{{ row.size }}</div>
          <div class="dk__muted nx-truncate" :class="{ 'dk__unused': row.used === 'unused' }">{{ row.used }}</div>
          <div class="dk__muted">{{ row.age }}</div>
        </template>
      </NxTable>
    </NxCard>
  </div>
</template>

<style scoped>
.dk__block { margin-bottom: 12px; }
.dk__name { font-weight: 500; }
.dk__muted { color: var(--nx-text-muted); }
.dk__unused { color: var(--nx-warning); }
.dk__actions { display: flex; gap: 8px; align-items: center; justify-content: flex-end; }
</style>
