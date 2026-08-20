<script setup lang="ts">
/** Paid app licenses — keys, seats and renewals. */
import { ref } from "vue";
import { LICENSES } from "@/data/apps";
import { useUiStore } from "@/stores/ui";

const ui = useUiStore();
const key = ref("");

const TONE = { Active: "success", Expiring: "warning", "Not licensed": "neutral" } as const;

async function copyKey(value: string) {
  // Keys are masked in this list, so what lands on the clipboard is the masked
  // string. Say so rather than implying the real key was copied.
  try {
    await navigator.clipboard.writeText(value);
    ui.toast("Copied the masked key. Reveal it from the vendor portal for the full value.", "info");
  } catch {
    ui.toast("Could not access the clipboard.", "danger");
  }
}
</script>

<template>
  <div class="nx-view">
    <header class="lic__head">
      <p class="lic__kicker">Apps</p>
      <h1 class="lic__title">Paid app licenses</h1>
      <p class="lic__sub">Keys, seats and renewals for the paid add-ons you use.</p>
    </header>

    <NxCard flush class="lic__block">
      <NxTable
        :columns="[
          { key: 'name', label: 'App', width: '1.3fr' },
          { key: 'key', label: 'License key', width: '1.3fr' },
          { key: 'seats', label: 'Covers', width: '0.8fr' },
          { key: 'state', label: 'Status', width: '1fr' },
          { key: 'actions', label: '', width: '176px', align: 'end' },
        ]"
        :rows="LICENSES"
        :row-key="(l) => l.name"
      >
        <template #default="{ row }">
          <div class="lic__name">{{ row.name }}</div>
          <div class="lic__muted nx-mono nx-truncate">{{ row.key }}</div>
          <div class="lic__muted">{{ row.seats }}</div>
          <div>
            <NxBadge :tone="TONE[row.state]">{{ row.state }}</NxBadge>
            <div class="lic__renew">{{ row.renew }}</div>
          </div>
          <div class="lic__actions">
            <NxButton :disabled="row.key === '—'" @click="copyKey(row.key)">Copy key</NxButton>
            <NxButton variant="primary" @click="ui.toast('Renew is not wired up yet.', 'info')">Renew</NxButton>
          </div>
        </template>
      </NxTable>
    </NxCard>

    <NxCard title="Have a key already?" subtitle="Keys move between servers freely.">
      <div class="lic__activate">
        <NxField label="License key" class="nx-row__grow">
          <template #default="{ id }">
            <NxInput :id="id" v-model="key" mono placeholder="XXXX-XXXX-XXXX-XXXX" autocomplete="off" />
          </template>
        </NxField>
        <NxButton
          variant="primary"
          size="lg"
          :disabled="key.trim().length < 8"
          @click="ui.toast('Activation is not wired up yet.', 'info')"
        >
          Activate license
        </NxButton>
      </div>
    </NxCard>
  </div>
</template>

<style scoped>
.lic__head { padding-bottom: 24px; }
.lic__kicker {
  margin: 0;
  font-size: var(--nx-text-xs);
  letter-spacing: var(--nx-ls-caps);
  color: var(--nx-text-placeholder);
  font-weight: 600;
  text-transform: uppercase;
}
.lic__title {
  margin: 6px 0 0;
  font-size: var(--nx-text-xl);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
}
.lic__sub {
  margin: 6px 0 0;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
}
.lic__block { margin-bottom: 12px; }
.lic__name { font-weight: 500; }
.lic__muted { color: var(--nx-text-muted); }
.lic__renew { font-size: var(--nx-text-sm); color: var(--nx-text-muted); padding-top: 4px; }
.lic__actions { display: flex; gap: 8px; justify-content: flex-end; }
.lic__activate { display: flex; align-items: flex-end; gap: 12px; flex-wrap: wrap; }
</style>
