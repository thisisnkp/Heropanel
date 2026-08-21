<script setup lang="ts">
/**
 * Temporary access — time-boxed grants for support or a contractor.
 *
 * The "what guests never get" panel is deliberately part of the screen rather
 * than help text: the whole point of this feature is that you can let someone in
 * without worrying, and that only works if the boundaries are visible at the
 * moment you are deciding.
 */
import { ref } from "vue";
import { GRANTS, TEMP_STATS } from "@/data/system";
import { useUiStore } from "@/stores/ui";

const ui = useUiStore();

const email = ref("");
const duration = ref("8h");
const scope = ref("site");

const TONE = { Active: "success", Pending: "warning", Expired: "neutral" } as const;

const DURATIONS = [
  { value: "1h", label: "1 hour" },
  { value: "8h", label: "8 hours" },
  { value: "24h", label: "24 hours" },
  { value: "7d", label: "7 days" },
];

const SCOPES = [
  { value: "site", label: "One website" },
  { value: "files", label: "Files + logs" },
  { value: "panel", label: "Full panel, no billing" },
];

const NEVER = [
  { icon: "block", tone: "danger", text: "License & billing" },
  { icon: "block", tone: "danger", text: "Deleting websites or backups" },
  { icon: "block", tone: "danger", text: "API tokens and SSH keys" },
  { icon: "visibility", tone: "success", text: "Every guest action is written to the audit trail" },
] as const;
</script>

<template>
  <div class="nx-view">
    <NxPageHeader
      title="Temporary access"
      subtitle="Let support or a developer in for a few hours — access expires on its own, no password sharing."
    >
      <template #actions>
        <NxButton variant="primary" size="lg" @click="ui.toast('Grant access is not wired up yet.', 'info')">
          Grant access
        </NxButton>
      </template>
    </NxPageHeader>

    <div class="nx-grid nx-grid--4 tmp__block">
      <NxStat v-for="s in TEMP_STATS" :key="s.label" :label="s.label" :value="s.value" :sub="s.sub" />
    </div>

    <NxCard flush class="tmp__block">
      <NxTable
        :columns="[
          { key: 'who', label: 'Who', width: '1.6fr' },
          { key: 'scope', label: 'Scope', width: '1.3fr' },
          { key: 'expires', label: 'Expires', width: '1fr' },
          { key: 'actions', label: '', width: '96px', align: 'end' },
        ]"
        :rows="GRANTS"
        :row-key="(g) => g.who"
      >
        <template #default="{ row }">
          <div class="tmp__who">
            <NxIcon name="schedule-send" size="md" class="tmp__who-icon" />
            <span class="nx-truncate">
              <span class="tmp__who-mail nx-mono nx-truncate">{{ row.who }}</span>
              <NxBadge :tone="TONE[row.state]">{{ row.state }}</NxBadge>
            </span>
          </div>
          <div class="tmp__muted nx-truncate">{{ row.scope }}</div>
          <div class="tmp__muted">{{ row.expires }}</div>
          <div class="tmp__actions">
            <NxButton @click="ui.toast(row.action + ' is not wired up yet.', 'info')">{{ row.action }}</NxButton>
          </div>
        </template>
      </NxTable>
    </NxCard>

    <div class="nx-grid nx-grid--2">
      <NxCard title="New grant" subtitle="We email a one-time link. Nothing is shared until they accept.">
        <div class="nx-stack">
          <NxField label="Email" required>
            <template #default="{ id }">
              <NxInput :id="id" v-model="email" type="email" mono placeholder="dev@example.com" autocomplete="off" />
            </template>
          </NxField>
          <div class="nx-grid nx-grid--2">
            <NxField label="Duration">
              <template #default="{ id }">
                <NxSelect :id="id" v-model="duration" :options="DURATIONS" />
              </template>
            </NxField>
            <NxField label="Scope">
              <template #default="{ id }">
                <NxSelect :id="id" v-model="scope" :options="SCOPES" />
              </template>
            </NxField>
          </div>
        </div>
        <NxButton
          variant="primary"
          size="lg"
          class="tmp__send"
          :disabled="!email.includes('@')"
          @click="ui.toast('Sending invites is not wired up yet.', 'info')"
        >
          Send invite
        </NxButton>
      </NxCard>

      <NxCard title="What guests never get" subtitle="Some areas stay yours regardless of scope.">
        <ul class="tmp__never">
          <li v-for="n in NEVER" :key="n.text" class="tmp__never-row">
            <NxIcon :name="n.icon" size="md" :class="'tmp__tone--' + n.tone" />
            <span class="nx-row__grow">{{ n.text }}</span>
          </li>
        </ul>
      </NxCard>
    </div>
  </div>
</template>

<style scoped>
.tmp__block { margin-bottom: 12px; }
.tmp__who { display: flex; align-items: center; gap: 12px; min-width: 0; }
.tmp__who-icon { color: var(--nx-text-muted); }
.tmp__who-mail { display: block; font-size: var(--nx-text-base); }
.tmp__muted { color: var(--nx-text-muted); }
.tmp__actions { display: flex; justify-content: flex-end; }
.tmp__send { margin-top: 16px; }

.tmp__never { list-style: none; margin: 0; padding: 0; }
.tmp__never-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 0;
  border-bottom: 1px solid var(--nx-hover);
  font-size: var(--nx-text-base);
}
.tmp__never-row:last-child { border-bottom: 0; }
.tmp__tone--danger { color: var(--nx-danger); }
.tmp__tone--success { color: var(--nx-success); }
</style>
