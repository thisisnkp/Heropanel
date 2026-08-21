<script setup lang="ts">
/** API — tokens and webhooks for driving the panel from your own scripts. */
import { API_EXAMPLE, API_STATS, API_TOKENS, WEBHOOKS } from "@/data/system";
import { useUiStore } from "@/stores/ui";

const ui = useUiStore();

const TONE = { Active: "success", Idle: "warning" } as const;

async function copy(value: string) {
  // Tokens are masked in this list. Copying gives you the masked string, which
  // is the truth — the plaintext is shown once, at creation, and never again.
  try {
    await navigator.clipboard.writeText(value);
    ui.toast("Copied the masked token. The full value is only shown when it is created.", "info");
  } catch {
    ui.toast("Could not access the clipboard.", "danger");
  }
}
</script>

<template>
  <div class="nx-view">
    <NxPageHeader title="API" subtitle="Tokens and webhooks for driving the panel from your own scripts.">
      <template #actions>
        <NxButton size="lg" @click="ui.toast('Docs are not wired up yet.', 'info')">Read the docs</NxButton>
        <NxButton variant="primary" size="lg" @click="ui.toast('Create token is not wired up yet.', 'info')">
          Create token
        </NxButton>
      </template>
    </NxPageHeader>

    <div class="nx-grid nx-grid--4 api__block">
      <NxStat v-for="s in API_STATS" :key="s.label" :label="s.label" :value="s.value" :sub="s.sub" />
    </div>

    <NxCard flush class="api__block">
      <NxTable
        :columns="[
          { key: 'name', label: 'Token', width: '1.6fr' },
          { key: 'key', label: 'Key', width: '1.2fr' },
          { key: 'scope', label: 'Scope', width: '1fr' },
          { key: 'actions', label: '', width: '150px', align: 'end' },
        ]"
        :rows="API_TOKENS"
        :row-key="(t) => t.name"
      >
        <template #default="{ row }">
          <div class="api__name-cell">
            <div class="api__name nx-truncate">{{ row.name }}</div>
            <div class="api__state" :class="'api__state--' + TONE[row.state]">
              {{ row.state }} · used {{ row.used }}
            </div>
          </div>
          <div class="api__muted nx-mono nx-truncate">{{ row.key }}</div>
          <div class="api__muted nx-truncate">{{ row.scope }}</div>
          <div class="api__actions">
            <NxButton @click="copy(row.key)">Copy</NxButton>
            <NxButton variant="danger" @click="ui.toast('Revoke is not wired up yet.', 'info')">Revoke</NxButton>
          </div>
        </template>
      </NxTable>
    </NxCard>

    <div class="nx-grid api__split">
      <NxCard title="Webhooks">
        <div class="nx-stack nx-stack--sm">
          <div v-for="w in WEBHOOKS" :key="w.url" class="api__hook">
            <div class="api__hook-head">
              <span class="nx-mono nx-truncate nx-row__grow">{{ w.url }}</span>
              <NxBadge tone="success">{{ w.state }}</NxBadge>
            </div>
            <p class="api__hook-events">{{ w.events }}</p>
          </div>
        </div>
        <NxButton class="api__add" @click="ui.toast('Add webhook is not wired up yet.', 'info')">Add webhook</NxButton>
      </NxCard>

      <NxLogPanel title="example request" :lines="API_EXAMPLE.map((text) => ({ text }))" />
    </div>
  </div>
</template>

<style scoped>
.api__block { margin-bottom: 12px; }
.api__split { grid-template-columns: minmax(0, 1.3fr) minmax(0, 1fr); }
@media (max-width: 1100px) {
  .api__split { grid-template-columns: minmax(0, 1fr); }
}
.api__name-cell { min-width: 0; }
.api__name { font-weight: 500; }
.api__state { font-size: var(--nx-text-xs); white-space: nowrap; }
.api__state--success { color: var(--nx-success); }
.api__state--warning { color: var(--nx-warning); }
.api__muted { color: var(--nx-text-muted); }
.api__actions { display: flex; gap: 8px; justify-content: flex-end; }

.api__hook {
  border: 1px solid var(--nx-active);
  background: var(--nx-surface-2);
  border-radius: var(--nx-radius-md);
  padding: 12px;
}
.api__hook-head { display: flex; align-items: center; gap: 12px; font-size: var(--nx-text-base); }
.api__hook-events {
  margin: 6px 0 0;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
}
.api__add { margin-top: 12px; }
</style>
