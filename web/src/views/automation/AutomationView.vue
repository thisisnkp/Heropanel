<script setup lang="ts">
/**
 * The automation launcher — OpenClaw and n8n.
 *
 * One component for both, keyed by the `product` route prop: the design draws
 * them identically and only the copy and the list differ.
 */
import { computed } from "vue";
import { AUTOMATION } from "@/data/system";
import { useUiStore } from "@/stores/ui";

const props = defineProps<{ product: keyof typeof AUTOMATION }>();

const ui = useUiStore();
const p = computed(() => AUTOMATION[props.product]);

const TONE = { active: "success", paused: "neutral", draft: "neutral" } as const;
</script>

<template>
  <div class="nx-view">
    <header class="auto__head">
      <div class="auto__tag" :class="'auto__tag--' + p.tone">{{ p.tag }}</div>
      <div class="nx-row__grow">
        <h1 class="auto__title">{{ p.name }}</h1>
        <p class="auto__sub">{{ p.sub }}</p>
      </div>
      <NxButton variant="primary" size="lg" @click="ui.toast(p.cta + ' is not wired up yet.', 'info')">
        {{ p.cta }}
      </NxButton>
    </header>

    <div class="nx-grid nx-grid--3 auto__block">
      <NxStat v-for="s in p.stats" :key="s.label" :label="s.label" :value="s.value" />
    </div>

    <NxCard :title="p.listTitle" flush>
      <NxTable
        :columns="[
          { key: 'name', label: 'Name', width: '1.6fr' },
          { key: 'meta', label: 'Trigger', width: '1fr' },
          { key: 'state', label: 'State', width: '0.8fr' },
          { key: 'actions', label: '', width: '90px', align: 'end' },
        ]"
        :rows="p.rows"
        :row-key="(r) => r.name"
      >
        <template #default="{ row }">
          <div class="auto__name nx-truncate">{{ row.name }}</div>
          <div class="auto__muted nx-mono nx-truncate">{{ row.meta }}</div>
          <div><NxBadge :tone="TONE[row.state]">{{ row.state }}</NxBadge></div>
          <div class="auto__actions">
            <NxButton @click="ui.toast('Open is not wired up yet.', 'info')">Open</NxButton>
          </div>
        </template>
      </NxTable>
    </NxCard>
  </div>
</template>

<style scoped>
.auto__head {
  display: flex;
  align-items: center;
  gap: 16px;
  padding-bottom: 24px;
  flex-wrap: wrap;
}
.auto__tag {
  width: 44px;
  height: 44px;
  flex: 0 0 44px;
  border-radius: var(--nx-radius-md);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: var(--nx-text-base);
  font-weight: 600;
  font-family: "JetBrains Mono", ui-monospace, monospace;
}
.auto__tag--info { background: var(--nx-info-soft); color: var(--nx-stack-php); }
.auto__tag--danger { background: var(--nx-danger-soft); color: var(--nx-danger); }
.auto__title {
  margin: 0;
  font-size: var(--nx-text-xl);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
}
.auto__sub {
  margin: 4px 0 0;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
  text-wrap: pretty;
}
.auto__block { margin-bottom: 12px; }
.auto__name { font-weight: 500; }
.auto__muted { color: var(--nx-text-muted); }
.auto__actions { display: flex; justify-content: flex-end; }
</style>
