<script setup lang="ts">
/** Mail — mailboxes, forwarding and deliverability for your domains. */
import { FORWARDERS, MAILBOXES, MAIL_DNS, MAIL_STATS } from "@/data/mail";
import { useUiStore } from "@/stores/ui";

const ui = useUiStore();

const RECORD_TONE = { Valid: "success", Weak: "warning", Missing: "danger" } as const;

function todo(label: string) {
  ui.toast(label + " is not wired up yet.", "info");
}
</script>

<template>
  <div class="nx-view">
    <NxPageHeader title="Mail" subtitle="Mailboxes, forwarding and deliverability for your domains.">
      <template #actions>
        <NxButton variant="primary" size="lg" @click="todo('Create mailbox')">Create mailbox</NxButton>
      </template>
    </NxPageHeader>

    <div class="nx-grid nx-grid--4 mail__block">
      <NxStat v-for="s in MAIL_STATS" :key="s.label" :label="s.label" :value="s.value" :sub="s.sub" />
    </div>

    <NxCard title="Mailboxes" flush class="mail__block">
      <NxTable
        :columns="[
          { key: 'addr', label: 'Address', width: '1.8fr' },
          { key: 'used', label: 'Storage', width: '1.4fr' },
          { key: 'actions', label: '', width: '260px', align: 'end' },
        ]"
        :rows="MAILBOXES"
        :row-key="(m) => m.address"
      >
        <template #default="{ row }">
          <div class="mail__addr">
            <NxIcon name="alternate-email" size="md" class="mail__addr-icon" />
            <span class="nx-mono nx-truncate">{{ row.address }}</span>
          </div>
          <NxMeter :value="row.pct" height="5px" :label="row.used" />
          <div class="mail__actions">
            <NxButton @click="todo('Webmail')">Webmail</NxButton>
            <NxButton @click="todo('Password reset')">Password</NxButton>
            <NxButton variant="danger" @click="todo('Delete mailbox')">Delete</NxButton>
          </div>
        </template>
      </NxTable>
    </NxCard>

    <div class="nx-grid nx-grid--2">
      <NxCard title="Forwarders">
        <div class="nx-stack nx-stack--sm">
          <div v-for="f in FORWARDERS" :key="f.from" class="mail__fw">
            <span class="nx-mono nx-truncate mail__fw-from">{{ f.from }}</span>
            <NxIcon name="arrow-forward" size="sm" class="mail__fw-arrow" />
            <span class="nx-mono nx-truncate mail__fw-to">{{ f.to }}</span>
            <NxButton size="sm" variant="danger" @click="todo('Remove forwarder')">Remove</NxButton>
          </div>
        </div>
        <NxButton class="mail__add" @click="todo('Add forwarder')">Add forwarder</NxButton>
      </NxCard>

      <NxCard title="Deliverability" subtitle="Records we manage for you. Fix DMARC to stop spoofing.">
        <ul class="mail__dns">
          <li v-for="d in MAIL_DNS" :key="d.label" class="mail__dns-row">
            <span class="mail__dns-label nx-mono">{{ d.label }}</span>
            <span class="mail__dns-value nx-mono nx-truncate">{{ d.value }}</span>
            <NxBadge :tone="RECORD_TONE[d.state]">{{ d.state }}</NxBadge>
          </li>
        </ul>
        <NxButton class="mail__add" @click="todo('Fix DMARC record')">Fix DMARC record</NxButton>
      </NxCard>
    </div>
  </div>
</template>

<style scoped>
.mail__block { margin-bottom: 12px; }
.mail__addr { display: flex; align-items: center; gap: 12px; min-width: 0; font-size: var(--nx-text-base); }
.mail__addr-icon { color: var(--nx-text-muted); }
.mail__actions { display: flex; gap: 8px; justify-content: flex-end; }

.mail__fw {
  display: flex;
  align-items: center;
  gap: 12px;
  border: 1px solid var(--nx-active);
  background: var(--nx-surface-2);
  border-radius: var(--nx-radius-md);
  padding: 12px;
}
.mail__fw-from { flex: 1; min-width: 0; font-size: var(--nx-text-base); }
.mail__fw-to { flex: 1; min-width: 0; font-size: var(--nx-text-base); color: var(--nx-text-muted); }
.mail__fw-arrow { color: var(--nx-text-placeholder); }
.mail__add { margin-top: 12px; }

.mail__dns { list-style: none; margin: 0; padding: 0; }
.mail__dns-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 0;
  border-bottom: 1px solid var(--nx-hover);
}
.mail__dns-row:last-child { border-bottom: 0; }
.mail__dns-label { font-size: var(--nx-text-sm); font-weight: 600; width: 52px; flex: 0 0 52px; }
.mail__dns-value { flex: 1; min-width: 0; font-size: var(--nx-text-sm); color: var(--nx-text-muted); }
</style>
