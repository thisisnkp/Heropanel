<script setup lang="ts">
/**
 * Renders one site screen from its spec.
 *
 * Every section is optional and appears only when the spec supplies it, which
 * is how one component covers screens as different as "Cron jobs" (stats +
 * toggles + table + log tail) and "SSL" (stats + toggles + a side panel).
 * The layout rule the design used is kept: the toggle panel and the side panel
 * share a row when both exist, and whichever is present alone takes the full
 * width.
 */
import { computed, ref } from "vue";
import type { SiteSpec } from "@/data/siteSpec";
import { useFlagsStore } from "@/stores/flags";
import { useUiStore } from "@/stores/ui";

const props = defineProps<{ spec: SiteSpec }>();

const flags = useFlagsStore();
const ui = useUiStore();

const choice = ref(props.spec.choiceDefault ?? props.spec.choices?.[0]?.label ?? "");

const pairedPanels = computed(() => Boolean(props.spec.toggles?.length && props.spec.sideTitle));

const logLines = computed(() =>
  (props.spec.logs ?? []).map((l) => ({
    time: l.time,
    text: l.text,
    color:
      l.tone === "success"
        ? "var(--nx-success-on-dark)"
        : l.tone === "warning"
          ? "var(--nx-warning-on-dark)"
          : l.tone === "danger"
            ? "var(--nx-danger)"
            : undefined,
  })),
);

/**
 * Which badge, if any, a toggle carries. A paid add-on is labelled whatever its
 * state; "Risky" only shows while the risky option is actually on, because a
 * warning about a setting you have not enabled is noise.
 */
function badgeFor(t: { paid?: boolean; warn?: boolean; flag: Parameters<typeof flags.isOn>[0] }) {
  if (t.paid) return { text: "Paid add-on", tone: "warning" as const };
  if (t.warn && flags.isOn(t.flag)) return { text: "Risky", tone: "danger" as const };
  return null;
}

// Actions are not wired to a backend yet. Rather than render dead buttons, each
// one says so — a button that silently does nothing is indistinguishable from
// one that failed.
function notImplemented(label: string) {
  ui.toast(label + " is not wired up yet.", "info");
}
</script>

<template>
  <div class="nx-view spec">
    <div v-if="spec.stats?.length" class="nx-grid nx-grid--4 spec__block">
      <NxStat v-for="st in spec.stats" :key="st.label" :label="st.label" :value="st.value" :sub="st.sub" />
    </div>

    <NxCard v-if="spec.choices?.length" :title="spec.choiceTitle" class="spec__block">
      <div class="spec__choices" role="radiogroup" :aria-label="spec.choiceTitle">
        <button
          v-for="c in spec.choices"
          :key="c.label"
          type="button"
          role="radio"
          :aria-checked="choice === c.label"
          class="spec__choice"
          :class="{ 'is-picked': choice === c.label }"
          @click="choice = c.label"
        >
          <span class="spec__choice-label">{{ c.label }}</span>
          <span class="spec__choice-sub">{{ c.sub }}</span>
        </button>
      </div>
    </NxCard>

    <div class="nx-grid spec__block" :class="pairedPanels ? 'nx-grid--main' : ''">
      <NxCard v-if="spec.toggles?.length" :title="spec.toggleTitle">
        <ul class="spec__toggles">
          <li v-for="t in spec.toggles" :key="t.label" class="spec__toggle">
            <div class="nx-row__grow">
              <div class="spec__toggle-head">
                <span class="spec__toggle-label">{{ t.label }}</span>
                <NxBadge v-if="badgeFor(t)" :tone="badgeFor(t)!.tone">{{ badgeFor(t)!.text }}</NxBadge>
              </div>
              <p class="spec__toggle-sub">{{ t.sub }}</p>
            </div>
            <div class="spec__toggle-control">
              <span class="spec__toggle-state">{{ flags.label(t.flag) }}</span>
              <NxToggle
                :model-value="flags.isOn(t.flag)"
                :aria-label="t.label"
                @update:model-value="flags.set(t.flag, $event)"
              />
            </div>
          </li>
        </ul>
      </NxCard>

      <NxCard v-if="spec.sideTitle" :title="spec.sideTitle" class="spec__side">
        <p v-if="spec.sideNote" class="spec__side-note">{{ spec.sideNote }}</p>

        <div v-if="spec.fields?.length" class="nx-stack spec__fields">
          <div v-for="f in spec.fields" :key="f.label">
            <div class="spec__field-label">{{ f.label }}</div>
            <div class="spec__field-value nx-mono nx-truncate">{{ f.value }}</div>
          </div>
        </div>

        <div class="spec__spacer" />

        <div v-if="spec.sideActions?.length" class="spec__side-actions">
          <NxButton
            v-for="a in spec.sideActions"
            :key="a.label"
            :variant="a.primary ? 'primary' : 'default'"
            @click="notImplemented(a.label)"
          >
            {{ a.label }}
          </NxButton>
        </div>
      </NxCard>
    </div>

    <NxCard v-for="t in [spec.table1, spec.table2].filter(Boolean)" :key="t!.title" :title="t!.title" flush class="spec__block">
      <template #action>
        <NxButton @click="notImplemented(t!.action)">{{ t!.action }}</NxButton>
      </template>
      <NxTable
        :columns="[
          { key: 'a', label: t!.columns[0], width: '2fr' },
          { key: 'b', label: t!.columns[1], width: '1.1fr' },
          { key: 'c', label: t!.columns[2], width: '0.9fr' },
          { key: 'actions', label: '', width: '150px', align: 'end' },
        ]"
        :rows="t!.rows"
      >
        <template #default="{ row }">
          <div class="spec__cell-key nx-mono nx-truncate">{{ row.a }}</div>
          <div class="spec__cell nx-truncate">{{ row.b }}</div>
          <div class="spec__cell nx-truncate">{{ row.c }}</div>
          <div class="spec__row-actions">
            <NxButton v-if="row.action" @click="notImplemented(row.action)">{{ row.action }}</NxButton>
            <NxButton v-if="row.danger" variant="danger" @click="notImplemented(row.danger)">{{ row.danger }}</NxButton>
          </div>
        </template>
      </NxTable>
    </NxCard>

    <NxLogPanel v-if="logLines.length" :title="spec.logName ?? 'log'" :lines="logLines" live>
      <template #actions>
        <NxButton size="sm" class="spec__log-btn" @click="notImplemented('Download')">Download</NxButton>
      </template>
    </NxLogPanel>
  </div>
</template>

<style scoped>
.spec__block { margin-bottom: 12px; }

.spec__choices { display: flex; gap: 8px; flex-wrap: wrap; }
.spec__choice {
  text-align: left;
  min-width: 150px;
  flex: 1 1 150px;
  border: 1px solid var(--nx-border);
  background: var(--nx-surface);
  border-radius: var(--nx-radius-md);
  padding: 12px 16px;
  cursor: pointer;
  font-family: inherit;
}
.spec__choice:hover { background: var(--nx-hover); }
.spec__choice.is-picked { border-color: var(--nx-primary); background: var(--nx-primary-soft); }
.spec__choice-label {
  display: block;
  font-size: var(--nx-text-base);
  color: var(--nx-text-2);
}
.spec__choice.is-picked .spec__choice-label { font-weight: 600; color: var(--nx-primary-text); }
.spec__choice-sub {
  display: block;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
  padding-top: 4px;
  line-height: 1.45;
}

.spec__toggles { list-style: none; margin: 0; padding: 0; }
.spec__toggle {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 0;
  border-bottom: 1px solid var(--nx-hover);
}
.spec__toggle:last-child { border-bottom: 0; }
.spec__toggle-head { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.spec__toggle-label { font-size: var(--nx-text-base); font-weight: 500; }
.spec__toggle-sub {
  margin: 4px 0 0;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
  text-wrap: pretty;
}
.spec__toggle-control { display: flex; align-items: center; gap: 8px; flex: 0 0 auto; }
.spec__toggle-state {
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
  width: 22px;
  text-align: right;
}

.spec__side { display: flex; flex-direction: column; }
.spec__side :deep(.nx-card__body) { display: flex; flex-direction: column; flex: 1; }
.spec__side-note {
  margin: 0;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
  line-height: 1.55;
  text-wrap: pretty;
}
.spec__fields { padding-top: 16px; }
.spec__field-label {
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
  padding-bottom: 6px;
}
.spec__field-value {
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-md);
  padding: 8px 12px;
  font-size: var(--nx-text-base);
  background: var(--nx-surface-2);
  color: var(--nx-text-2);
}
.spec__spacer { flex: 1; }
.spec__side-actions { display: flex; gap: 8px; flex-wrap: wrap; padding-top: 16px; }

.spec__cell-key { font-weight: 500; }
.spec__cell { color: var(--nx-text-muted); }
.spec__row-actions { display: flex; gap: 8px; justify-content: flex-end; }

.spec__log-btn {
  border-color: var(--nx-dark-border-2);
  background: transparent;
  color: var(--nx-text-on-dark);
}
.spec__log-btn:hover { background: var(--nx-dark-border); }
</style>
