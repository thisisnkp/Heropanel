<script setup lang="ts">
/**
 * DNS & nameservers — the zone editor.
 *
 * The picked domain is a query parameter, so a zone is a link. With no domain
 * picked the screen is a picker rather than an empty editor: "which zone am I
 * editing" is the one question that must be answered before anything on this
 * screen means anything, and a record form with no zone behind it is a trap.
 */
import { computed, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  DNS_DOMAINS,
  EXPORT_FORMATS,
  NAMESERVERS,
  RECORD_TYPES,
  ZONE_REDIRECTS,
  ZONE_STATS,
  ZONE_SUBDOMAINS,
  ZONE_HEALTH,
  ZONE_TARGETS,
  exportPreview,
  recordFields,
  zoneRecords,
  type DomainSection,
  type RecordType,
} from "@/data/dns";
import { useUiStore } from "@/stores/ui";

const route = useRoute();
const router = useRouter();
const ui = useUiStore();

const domain = computed(() => {
  const q = route.query.domain;
  const d = typeof q === "string" ? q : "";
  return (DNS_DOMAINS as readonly string[]).includes(d) ? d : "";
});

function pickDomain(d: string) {
  // Landing on the overview, not the record editor: picking a domain answers
  // "which one", and the next question is usually "is it healthy", not "let me
  // edit an A record".
  void router.replace({ name: "dns", query: { domain: d, section: "overview" } });
}

function clearDomain() {
  void router.replace({ name: "dns" });
}

/**
 * Which half of the zone you are looking at, from the URL.
 *
 * The design splits a domain into an overview and the DNS/nameserver editor, and
 * the sidebar lists them as two sections. Holding that in the query string
 * rather than in local state means the sidebar and the page cannot disagree, and
 * "send me this zone's records" stays a link.
 */
const section = computed<DomainSection>(() => (route.query.section === "dns" ? "dns" : "overview"));

type Tab = "records" | "nameservers" | "redirects" | "subdomains" | "ssl";
const TABS: readonly { key: Tab; label: string }[] = [
  { key: "records", label: "DNS records" },
  { key: "nameservers", label: "Nameservers" },
  { key: "redirects", label: "Redirects" },
  { key: "subdomains", label: "Subdomains" },
  { key: "ssl", label: "SSL" },
];
const tab = ref<Tab>("records");

// ---- record editor ---------------------------------------------------------

const recordType = ref<RecordType>("A");
const fields = computed(() => recordFields(recordType.value, domain.value));
const gridTemplate = computed(() => fields.value.map((f) => f.width).join(" "));
const draft = ref<Record<string, string>>({});

const records = computed(() => zoneRecords(domain.value));
const selected = ref<string[]>([]);

const allSelected = computed(() => selected.value.length === records.value.length && records.value.length > 0);

function toggleAll() {
  selected.value = allSelected.value ? [] : records.value.map((r) => r.id);
}

function toggle(id: string) {
  selected.value = selected.value.includes(id)
    ? selected.value.filter((x) => x !== id)
    : [...selected.value, id];
}

function deleteSelected() {
  if (!selected.value.length) return;
  ui.toast(selected.value.length + " record(s) would be deleted — not wired up yet.", "info");
  selected.value = [];
}

// ---- import / export -------------------------------------------------------

const importOpen = ref(false);
const exportOpen = ref(false);
const pasted = ref("");
const exportFormat = ref<string>(EXPORT_FORMATS[0]);
</script>

<template>
  <div class="nx-view">
    <!-- No zone chosen: pick one. -->
    <template v-if="!domain">
      <NxPageHeader title="DNS &amp; nameservers" subtitle="Pick a domain to edit its zone." />
      <NxCard title="Your domains" flush>
        <ul class="dns__pick-list">
          <li v-for="d in DNS_DOMAINS" :key="d">
            <button type="button" class="dns__pick" @click="pickDomain(d)">
              <NxIcon name="dns" size="md" class="dns__pick-icon" />
              <span class="nx-row__grow nx-mono nx-truncate">{{ d }}</span>
              <NxIcon name="chevron-right" size="sm" class="dns__chevron" />
            </button>
          </li>
        </ul>
      </NxCard>
    </template>

    <!-- Overview: what this domain points at, and whether it is healthy. -->
    <template v-else-if="section === 'overview'">
      <header class="dns__head">
        <div class="nx-row__grow">
          <p class="dns__kicker">Domain</p>
          <h1 class="dns__title nx-mono nx-truncate">{{ domain }}</h1>
          <p class="dns__lead">Everything this domain points at, and the records behind it.</p>
        </div>
        <NxButton size="lg" @click="clearDomain">
          <NxIcon name="swap-horiz" size="sm" />
          Change domain
        </NxButton>
      </header>

      <div class="nx-grid nx-grid--4 dns__block">
        <NxStat v-for="s in ZONE_STATS" :key="s.label" :label="s.label" :value="s.value" :sub="s.sub" />
      </div>

      <div class="dns__split">
        <NxCard title="Where it points">
          <ul class="dns__ns">
            <li v-for="t in ZONE_TARGETS" :key="t.label" class="dns__ns-row">
              <NxIcon :name="t.icon" size="md" class="dns__pick-icon" />
              <span class="nx-row__grow">{{ t.label }}</span>
              <span class="dns__muted nx-mono nx-truncate">{{ t.value }}</span>
            </li>
          </ul>
        </NxCard>

        <NxCard title="Health">
          <p class="dns__health">{{ ZONE_HEALTH }}</p>
          <NxButton @click="ui.toast('DNS check is not wired up yet.', 'info')">Run a DNS check</NxButton>
        </NxCard>
      </div>
    </template>

    <!-- DNS / nameservers: the editor. -->
    <template v-else>
      <header class="dns__head">
        <div class="nx-row__grow">
          <p class="dns__kicker">DNS / nameservers</p>
          <h1 class="dns__title nx-mono nx-truncate">{{ domain }}</h1>
        </div>
        <NxButton size="lg" @click="clearDomain">
          <NxIcon name="swap-horiz" size="sm" />
          Change domain
        </NxButton>
      </header>

      <div class="dns__tabs" role="tablist" aria-label="DNS and nameserver sections">
        <button
          v-for="t in TABS"
          :key="t.key"
          type="button"
          role="tab"
          class="dns__tab"
          :class="{ 'is-current': tab === t.key }"
          :aria-selected="tab === t.key"
          @click="tab = t.key"
        >
          {{ t.label }}
        </button>
      </div>

      <!-- DNS records -->
      <template v-if="tab === 'records'">
        <NxCard title="Add a record" class="dns__block">
          <template #action>
            <NxButton @click="importOpen = true">Import</NxButton>
            <NxButton @click="exportOpen = true">Export</NxButton>
          </template>

          <fieldset class="dns__fieldset">
            <legend class="dns__legend">Type</legend>
            <div class="dns__types">
              <button
                v-for="t in RECORD_TYPES"
                :key="t"
                type="button"
                class="dns__type nx-mono"
                :class="{ 'is-picked': recordType === t }"
                :aria-pressed="recordType === t"
                @click="recordType = t; draft = {}"
              >
                {{ t }}
              </button>
            </div>
          </fieldset>

          <div class="dns__form">
            <div class="dns__form-grid" :style="{ gridTemplateColumns: gridTemplate }">
              <NxField v-for="f in fields" :key="f.label" :label="f.label">
                <template #default="{ id }">
                  <NxInput
                    :id="id"
                    mono
                    :placeholder="f.placeholder"
                    :model-value="draft[f.label] ?? ''"
                    autocomplete="off"
                    @update:model-value="draft[f.label] = $event"
                  />
                </template>
              </NxField>
            </div>
            <NxButton variant="primary" size="lg" @click="ui.toast('Adding records is not wired up yet.', 'info')">
              Add
            </NxButton>
          </div>
        </NxCard>

        <NxCard flush>
          <div class="dns__table" role="table" aria-label="DNS records">
            <div role="row" class="dns__row dns__row--head">
              <span class="dns__cell-check">
                <input
                  type="checkbox"
                  class="dns__check"
                  :checked="allSelected"
                  aria-label="Select all records"
                  @change="toggleAll"
                />
              </span>
              <span role="columnheader">Name</span>
              <span role="columnheader">Type / TTL</span>
              <span role="columnheader">Value</span>
              <span role="columnheader" class="dns__cell-actions" />
            </div>

            <div
              v-for="r in records"
              :key="r.id"
              role="row"
              class="dns__row"
              :class="{ 'is-picked': selected.includes(r.id) }"
            >
              <span class="dns__cell-check">
                <input
                  type="checkbox"
                  class="dns__check"
                  :checked="selected.includes(r.id)"
                  :aria-label="r.name + ' ' + r.type"
                  @change="toggle(r.id)"
                />
              </span>
              <span role="cell" class="dns__name nx-mono nx-truncate">{{ r.name }}</span>
              <span role="cell" class="dns__muted nx-mono nx-truncate">{{ r.type }} · {{ r.ttl }}</span>
              <span role="cell" class="dns__muted nx-mono nx-truncate">{{ r.value }}</span>
              <span role="cell" class="dns__cell-actions">
                <NxButton @click="ui.toast('Edit is not wired up yet.', 'info')">Edit</NxButton>
                <NxButton variant="danger" @click="ui.toast('Delete is not wired up yet.', 'info')">Delete</NxButton>
              </span>
            </div>
          </div>

          <div class="dns__foot">
            <NxButton variant="danger" :disabled="!selected.length" @click="deleteSelected">
              {{ selected.length ? "Delete selected (" + selected.length + ")" : "Delete selected" }}
            </NxButton>
            <span class="nx-row__grow" />
            <NxButton @click="ui.toast('Reset is not wired up yet.', 'info')">Reset to defaults</NxButton>
          </div>
        </NxCard>
      </template>

      <!-- Nameservers -->
      <NxCard v-else-if="tab === 'nameservers'" title="Nameservers" subtitle="Point your registrar at these two.">
        <ul class="dns__ns">
          <li v-for="n in NAMESERVERS" :key="n" class="dns__ns-row">
            <NxIcon name="dns" size="md" class="dns__pick-icon" />
            <span class="nx-row__grow nx-mono">{{ n }}</span>
            <NxBadge tone="success">Responding</NxBadge>
          </li>
        </ul>
        <NxCallout tone="info" class="dns__ns-note">
          Nameserver changes take up to 24 hours to propagate. Your site keeps serving from the old records until they
          do.
        </NxCallout>
      </NxCard>

      <!-- Redirects -->
      <NxCard v-else-if="tab === 'redirects'" title="Redirect rules" flush>
        <template #action>
          <NxButton @click="ui.toast('Add rule is not wired up yet.', 'info')">Add rule</NxButton>
        </template>
        <NxTable
          :columns="[
            { key: 'from', label: 'From', width: '1.4fr' },
            { key: 'to', label: 'To', width: '1.6fr' },
            { key: 'type', label: 'Type', width: '0.6fr' },
          ]"
          :rows="ZONE_REDIRECTS"
          :row-key="(r) => r.from"
        >
          <template #default="{ row }">
            <div class="dns__name nx-mono nx-truncate">{{ row.from }}</div>
            <div class="dns__muted nx-mono nx-truncate">{{ row.to }}</div>
            <div class="dns__muted nx-mono">{{ row.type }}</div>
          </template>
        </NxTable>
      </NxCard>

      <!-- Subdomains -->
      <NxCard v-else-if="tab === 'subdomains'" title="Subdomains" flush>
        <template #action>
          <NxButton @click="ui.toast('Create is not wired up yet.', 'info')">Create</NxButton>
        </template>
        <NxTable
          :columns="[
            { key: 'name', label: 'Subdomain', width: '1.4fr' },
            { key: 'root', label: 'Document root', width: '1.6fr' },
            { key: 'ssl', label: 'SSL', width: '0.6fr' },
          ]"
          :rows="ZONE_SUBDOMAINS"
          :row-key="(s) => s.name"
        >
          <template #default="{ row }">
            <div class="dns__name nx-mono nx-truncate">{{ row.name }}.{{ domain }}</div>
            <div class="dns__muted nx-mono nx-truncate">{{ row.root }}</div>
            <div><NxBadge tone="success">{{ row.ssl }}</NxBadge></div>
          </template>
        </NxTable>
      </NxCard>

      <!-- SSL -->
      <NxCard v-else title="SSL certificate" subtitle="Let's Encrypt, renewed automatically.">
        <div class="nx-stack">
          <div class="dns__kv">
            <span class="dns__kv-label">Issuer</span>
            <span class="nx-mono">Let's Encrypt R11</span>
          </div>
          <div class="dns__kv">
            <span class="dns__kv-label">Covers</span>
            <span class="nx-mono nx-truncate">{{ domain }}, www.{{ domain }}</span>
          </div>
          <div class="dns__kv">
            <span class="dns__kv-label">Expires</span>
            <span class="nx-mono">in 74 days · auto-renews at 30</span>
          </div>
        </div>
        <NxButton class="dns__ns-note" @click="ui.toast('Reissue is not wired up yet.', 'info')">Reissue now</NxButton>
      </NxCard>
    </template>

    <NxModal
      v-model:open="importOpen"
      title="Import DNS records"
      description="Paste a zone file or upload one — nothing changes until you confirm."
      width="560px"
    >
      <div class="dns__drop">
        <NxIcon name="upload-file" size="xl" class="dns__drop-icon" />
        <p class="dns__drop-title">Drop a .zone, .txt, .json or .csv file</p>
        <p class="dns__drop-sub">Up to 1 MB</p>
      </div>
      <NxField label="Or paste records">
        <template #default="{ id }">
          <textarea
            :id="id"
            v-model="pasted"
            class="dns__textarea nx-mono"
            placeholder="@	300	IN	A	203.0.113.24"
          />
        </template>
      </NxField>
      <NxCallout tone="warning">Existing records with the same name and type will be replaced.</NxCallout>

      <template #footer>
        <NxButton @click="importOpen = false">Cancel</NxButton>
        <NxButton variant="primary" @click="importOpen = false; ui.toast('Import is not wired up yet.', 'info')">
          Preview import
        </NxButton>
      </template>
    </NxModal>

    <NxModal
      v-model:open="exportOpen"
      title="Export DNS records"
      :description="domain + ' · ' + records.length + ' records'"
      width="620px"
    >
      <fieldset class="dns__fieldset">
        <legend class="dns__legend">Format</legend>
        <div class="dns__types">
          <button
            v-for="f in EXPORT_FORMATS"
            :key="f"
            type="button"
            class="dns__format"
            :class="{ 'is-picked': exportFormat === f }"
            :aria-pressed="exportFormat === f"
            @click="exportFormat = f"
          >
            {{ f }}
          </button>
        </div>
      </fieldset>

      <NxLogPanel
        :title="'preview · ' + exportFormat"
        :lines="exportPreview(domain).map((text) => ({ text }))"
        class="dns__preview"
      />

      <template #footer>
        <NxButton @click="exportOpen = false">Cancel</NxButton>
        <NxButton variant="primary" @click="exportOpen = false; ui.toast('Download is not wired up yet.', 'info')">
          Download
        </NxButton>
      </template>
    </NxModal>
  </div>
</template>

<style scoped>
.dns__lead { margin: 6px 0 0; font-size: var(--nx-text-base); color: var(--nx-text-muted); }
.dns__split {
  display: grid;
  grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr);
  gap: 12px;
}
@media (max-width: 900px) {
  .dns__split { grid-template-columns: minmax(0, 1fr); }
}
.dns__health {
  margin: 0 0 16px;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
  line-height: 1.55;
  text-wrap: pretty;
}
.dns__block { margin-bottom: 12px; }

.dns__pick-list { list-style: none; margin: 0; padding: 0; }
.dns__pick {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  text-align: left;
  border: 0;
  background: transparent;
  cursor: pointer;
  padding: 14px 16px;
  border-bottom: 1px solid var(--nx-hover);
  font-family: inherit;
  font-size: var(--nx-text-base);
  color: var(--nx-text);
}
.dns__pick:hover { background: var(--nx-surface-2); }
.dns__pick-icon { color: var(--nx-text-muted); }
.dns__chevron { color: var(--nx-border-strong); }

.dns__head { display: flex; align-items: flex-end; gap: 16px; padding-bottom: 24px; flex-wrap: wrap; }
.dns__kicker {
  margin: 0;
  font-size: var(--nx-text-xs);
  letter-spacing: var(--nx-ls-caps);
  color: var(--nx-text-placeholder);
  font-weight: 600;
  text-transform: uppercase;
}
.dns__title {
  margin: 6px 0 0;
  font-size: var(--nx-text-xl);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
}

.dns__tabs {
  display: flex;
  gap: 4px;
  border-bottom: 1px solid var(--nx-border);
  margin-bottom: 20px;
  overflow-x: auto;
}
.dns__tab {
  border: 0;
  background: transparent;
  cursor: pointer;
  padding: 12px 16px;
  font-size: var(--nx-text-base);
  font-family: inherit;
  color: var(--nx-text-muted);
  border-bottom: 2px solid transparent;
  margin-bottom: -1px;
  white-space: nowrap;
  transition: color 140ms ease, border-color 140ms ease;
}
.dns__tab:hover { color: var(--nx-text); }
.dns__tab.is-current { color: var(--nx-text); font-weight: 600; border-bottom-color: var(--nx-primary); }

.dns__fieldset { border: 0; margin: 0; padding: 0; min-width: 0; }
.dns__legend {
  padding: 0 0 8px;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
}
.dns__types { display: flex; gap: 8px; flex-wrap: wrap; padding-bottom: 16px; }
.dns__type,
.dns__format {
  border: 1px solid var(--nx-border);
  background: var(--nx-surface);
  color: var(--nx-text-3);
  border-radius: var(--nx-radius-md);
  padding: 8px 16px;
  font-size: var(--nx-text-base);
  font-family: inherit;
  cursor: pointer;
}
.dns__type { font-family: "JetBrains Mono", ui-monospace, monospace; }
.dns__format { flex: 1 1 140px; text-align: left; }
.dns__type:hover,
.dns__format:hover { background: var(--nx-hover); }
.dns__type.is-picked,
.dns__format.is-picked {
  border-color: var(--nx-primary);
  background: var(--nx-info-soft);
  color: var(--nx-primary);
  font-weight: 600;
}

.dns__form { display: flex; align-items: flex-end; gap: 12px; flex-wrap: wrap; }
.dns__form-grid { flex: 1; display: grid; gap: 12px; min-width: 0; }
@media (max-width: 760px) {
  .dns__form-grid { grid-template-columns: minmax(0, 1fr) !important; }
}

.dns__table { width: 100%; }
.dns__row {
  display: grid;
  grid-template-columns: 22px minmax(0, 1.1fr) minmax(104px, 0.7fr) minmax(0, 1.9fr) auto;
  gap: 16px;
  align-items: center;
  padding: 12px 16px;
  border-bottom: 1px solid var(--nx-hover);
}
.dns__row--head {
  background: var(--nx-surface-2);
  border-bottom: 1px solid var(--nx-active);
  font-size: var(--nx-text-xs);
  letter-spacing: var(--nx-ls-caps);
  color: var(--nx-text-placeholder);
  font-weight: 600;
  text-transform: uppercase;
}
.dns__row.is-picked { background: var(--nx-primary-soft); }
@media (max-width: 900px) {
  .dns__row { grid-template-columns: 22px minmax(0, 1fr) auto; }
  .dns__row > :nth-child(3) { display: none; }
}
.dns__cell-check { display: flex; }
.dns__check { width: 16px; height: 16px; cursor: pointer; accent-color: var(--nx-primary); }
.dns__name { font-size: var(--nx-text-base); font-weight: 500; }
.dns__muted { font-size: var(--nx-text-base); color: var(--nx-text-muted); }
.dns__cell-actions { display: flex; gap: 8px; justify-content: flex-end; }

.dns__foot {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  background: var(--nx-surface-2);
  border-top: 1px solid var(--nx-active);
}

.dns__ns { list-style: none; margin: 0; padding: 0; }
.dns__ns-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 0;
  border-bottom: 1px solid var(--nx-hover);
  font-size: var(--nx-text-base);
}
.dns__ns-row:last-child { border-bottom: 0; }
.dns__ns-note { margin-top: 16px; }

.dns__kv { display: flex; align-items: baseline; gap: 16px; font-size: var(--nx-text-base); }
.dns__kv-label { flex: 0 0 84px; color: var(--nx-text-muted); }

.dns__drop {
  border: 1px dashed var(--nx-border-strong);
  border-radius: var(--nx-radius-md);
  padding: 24px;
  text-align: center;
  margin-bottom: 16px;
}
.dns__drop-icon { color: var(--nx-text-placeholder); margin: 0 auto; }
.dns__drop-title {
  margin: 8px 0 0;
  font-size: var(--nx-text-base);
  font-weight: 500;
}
.dns__drop-sub {
  margin: 4px 0 0;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
}
.dns__textarea {
  width: 100%;
  height: 120px;
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-md);
  padding: 12px;
  font-size: var(--nx-text-base);
  outline: 0;
  resize: vertical;
  background: var(--nx-surface);
  color: var(--nx-text);
}
.dns__textarea:focus { border-color: var(--nx-primary); box-shadow: 0 0 0 3px var(--nx-primary-soft); }
.dns__preview { margin-top: 16px; }
</style>
