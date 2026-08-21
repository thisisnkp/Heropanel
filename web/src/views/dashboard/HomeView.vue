<script setup lang="ts">
/**
 * Home — server health, what needs attention, and recent activity.
 *
 * The screen is assembled from fixtures in `@/data/dashboard` rather than
 * literals in the template, so wiring the real endpoints later is a change to
 * one module and not to this markup.
 */
import { computed, onMounted, onUnmounted, ref } from "vue";
import { useSitesStore } from "@/stores/sites";
import {
  ACTIVITY,
  ATTENTION,
  BANDWIDTH,
  CPU_SERIES,
  LAST_SCAN,
  PROTECTION_CHECKS,
  PROTECTION_SCORE,
  QUICK_ACTIONS,
  RAM_SERIES,
  SCAN_TILES,
  STORAGE,
  protectionGrade,
} from "@/data/dashboard";

const sites = useSitesStore();

// The design showed a skeleton for ~900ms on first paint. Keeping it means the
// loading state is a state the screen actually has, rather than something that
// only appears once a slow API is attached and nobody has laid it out.
const booting = ref(true);
let bootTimer = 0;
onMounted(() => {
  bootTimer = window.setTimeout(() => (booting.value = false), 600);
});
onUnmounted(() => window.clearTimeout(bootTimer));

const grade = computed(() => protectionGrade(PROTECTION_SCORE));
const pendingChecks = computed(() => PROTECTION_CHECKS.filter((c) => !c.ok).length);

const stats = computed(() => [
  { label: "Websites", value: String(sites.count), sub: sites.sites.filter((s) => s.status === "live").length + " live" },
  { label: "Deploys today", value: "7", sub: "1 failed, retried" },
  { label: "Disk used", value: "38 GB", sub: "of 200 GB" },
  { label: "Automations", value: "15", sub: "OpenClaw + n8n" },
]);

const bandwidthText = BANDWIDTH.used + " / " + BANDWIDTH.total;
const storageText = STORAGE.used + " / " + STORAGE.total;

// The activity feed is the one panel here with a real failure mode — it is the
// only one backed by a separate service — so it carries its own error state
// instead of taking the whole dashboard down with it.
const activityFailed = ref(false);
const errorDetailsOpen = ref(false);
</script>

<template>
  <div class="nx-view">
    <header class="home__head">
      <h1 class="home__title">Good afternoon, Aarav</h1>
      <p class="home__sub">Everything is up. Two things could use a minute of your time.</p>
    </header>

    <div v-if="booting" class="nx-grid nx-grid--4 home__block" aria-hidden="true">
      <div v-for="k in 4" :key="k" class="home__skel-card">
        <NxSkeleton height="12px" width="60%" />
        <NxSkeleton height="24px" width="45%" />
        <NxSkeleton height="12px" width="70%" />
      </div>
    </div>
    <div v-else class="nx-grid nx-grid--4 home__block">
      <NxStat v-for="s in stats" :key="s.label" :label="s.label" :value="s.value" :sub="s.sub" />
    </div>

    <div class="nx-grid nx-grid--side home__block">
      <section class="home__panel">
        <div class="home__prot-head">
          <NxIcon name="shield" size="lg" :class="'home__tone--' + grade.tone" />
          <span class="home__prot-grade nx-truncate" :class="'home__tone--' + grade.tone">{{ grade.label }}</span>
        </div>
        <div class="home__prot-score">
          <span class="home__prot-number" :class="'home__tone--' + grade.tone">{{ PROTECTION_SCORE }}</span>
          <span class="home__prot-out">/100</span>
        </div>
        <NxMeter :value="PROTECTION_SCORE" :tone="grade.tone" class="home__prot-bar" />
        <p class="home__prot-note">
          {{ pendingChecks }} of {{ PROTECTION_CHECKS.length }} checks
          {{ pendingChecks === 1 ? "needs" : "need" }} setup
        </p>

        <ul class="home__checks">
          <li v-for="c in PROTECTION_CHECKS" :key="c.label" class="home__check">
            <span class="home__check-label nx-truncate">{{ c.label }}</span>
            <NxIcon
              v-if="c.ok"
              name="check-circle"
              size="md"
              class="home__tone--success"
              :label="c.label + ' is on'"
            />
            <RouterLink v-else :to="{ name: c.to }" class="home__setup">Setup</RouterLink>
          </li>
        </ul>
      </section>

      <section class="home__panel">
        <h2 class="nx-section-title">Needs attention</h2>
        <div class="nx-stack nx-stack--sm">
          <div v-for="a in ATTENTION" :key="a.id" class="home__attn">
            <span class="home__dot" :class="'home__dot--' + a.severity" aria-hidden="true" />
            <span class="nx-row__grow">
              <span class="home__attn-label">{{ a.label }}</span>
              <span class="home__attn-sub">{{ a.sub }}</span>
            </span>
            <RouterLink :to="{ name: a.to }" class="home__attn-btn">{{ a.action }}</RouterLink>
          </div>
        </div>
      </section>
    </div>

    <div class="nx-grid nx-grid--main home__block">
      <section class="home__panel">
        <div class="nx-row home__panel-head">
          <h2 class="nx-section-title nx-row__grow">Resource usage</h2>
          <span class="home__hint">last 24 hours</span>
        </div>
        <div class="nx-stack nx-stack--lg">
          <div>
            <div class="home__series-head">
              <span class="home__swatch home__swatch--cpu" aria-hidden="true" />
              <span class="home__series-name">CPU</span>
              <span class="home__series-value nx-mono">43% now</span>
              <span class="home__hint">avg 52%</span>
            </div>
            <NxSparkBars :series="CPU_SERIES" tone="brand" height="56px" label="CPU usage over the last 24 hours" />
          </div>
          <div>
            <div class="home__series-head">
              <span class="home__swatch home__swatch--ram" aria-hidden="true" />
              <span class="home__series-name">Memory</span>
              <span class="home__series-value nx-mono">2.6 / 4 GB</span>
              <span class="home__hint">peak 71%</span>
            </div>
            <NxSparkBars :series="RAM_SERIES" tone="php" height="44px" label="Memory usage over the last 24 hours" />
          </div>
          <div class="home__divided">
            <NxMeter
              :value="BANDWIDTH.pct"
              tone="success"
              label="Bandwidth this month"
              :value-text="bandwidthText"
              :note="BANDWIDTH.note"
            />
          </div>
          <NxMeter
            :value="STORAGE.pct"
            tone="neutral"
            label="Storage"
            :value-text="storageText"
            :note="STORAGE.note"
          />
        </div>
      </section>

      <section class="home__panel home__panel--col">
        <div class="nx-row home__panel-head">
          <h2 class="nx-section-title nx-row__grow">Security &amp; scans</h2>
          <span class="home__hint">score {{ PROTECTION_SCORE }}</span>
        </div>
        <div class="nx-stack nx-stack--sm">
          <NxTile
            v-for="t in SCAN_TILES"
            :key="t.label"
            :icon="t.icon"
            :label="t.label"
            :to="t.to"
            :value="t.value"
            :tone="t.tone"
          >
            <span v-if="t.status" :class="'home__tone--' + t.tone">{{ t.status }}</span>
            <span v-else>{{ t.sub }}</span>
          </NxTile>
        </div>
        <div class="home__spacer" />
        <div class="nx-row home__scan-foot">
          <span class="home__hint nx-row__grow">{{ LAST_SCAN }}</span>
          <NxButton variant="primary">Scan now</NxButton>
        </div>
      </section>
    </div>

    <div class="nx-grid nx-grid--main">
      <section class="home__panel">
        <div class="nx-row home__panel-head">
          <h2 class="nx-section-title nx-row__grow">Recent activity</h2>
          <!-- The full log is a screen the desktop navigation otherwise never
               links to: it is a mobile tab, and on desktop it was reachable only
               by typing the URL. Five lines is a summary, not the record. -->
          <RouterLink :to="{ name: 'activity' }" class="home__all">View all</RouterLink>
        </div>

        <NxCallout v-if="activityFailed" tone="danger" title="We could not load your activity feed">
          The panel service did not answer in time. Your sites are unaffected.
          <template #actions>
            <NxButton variant="primary" @click="activityFailed = false">Retry</NxButton>
            <NxButton :aria-expanded="errorDetailsOpen" @click="errorDetailsOpen = !errorDetailsOpen">
              View details
            </NxButton>
            <pre v-if="errorDetailsOpen" class="home__err">GET /api/v1/activity — 504 upstream timeout after 10000ms (request 4f2a1c9)</pre>
          </template>
        </NxCallout>

        <ul v-else class="home__activity">
          <li v-for="(r, i) in ACTIVITY" :key="i" class="home__act">
            <NxIcon :name="r.icon" size="md" class="home__act-icon" />
            <span class="nx-row__grow home__act-text">{{ r.text }}</span>
            <span class="home__act-when">{{ r.when }}</span>
          </li>
        </ul>
      </section>

      <section class="home__panel">
        <h2 class="nx-section-title">Jump back in</h2>
        <div class="nx-grid nx-grid--2">
          <NxTile v-for="q in QUICK_ACTIONS" :key="q.label" :icon="q.icon" :label="q.label" :sub="q.sub" :to="q.to" />
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.home__head { padding-bottom: 24px; }
.home__title {
  margin: 0;
  font-size: var(--nx-text-xl);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
}
.home__sub {
  margin: 6px 0 0;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
}
.home__block { padding-bottom: 12px; }

.home__panel {
  background: var(--nx-surface);
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-lg);
  padding: 20px;
  min-width: 0;
}
.home__panel--col { display: flex; flex-direction: column; }
.home__panel-head .nx-section-title { margin-bottom: 12px; }
.home__spacer { flex: 1; }

.home__skel-card {
  display: flex;
  flex-direction: column;
  gap: 10px;
  background: var(--nx-surface);
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-lg);
  padding: 16px;
}

.home__tone--success { color: var(--nx-success); }
.home__tone--warning { color: var(--nx-warning); }
.home__tone--danger { color: var(--nx-danger); }
.home__tone--brand { color: var(--nx-primary-text); }

.home__prot-head { display: flex; align-items: center; gap: 8px; }
.home__prot-grade {
  font-size: var(--nx-text-md);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
}
.home__prot-score { display: flex; align-items: baseline; gap: 4px; padding-top: 12px; }
.home__prot-number {
  font-size: var(--nx-text-2xl);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
  line-height: 1;
}
.home__prot-out { font-size: var(--nx-text-base); color: var(--nx-text-muted); }
.home__prot-bar { margin-top: 12px; }
.home__prot-note {
  margin: 8px 0 0;
  font-size: var(--nx-text-xs);
  color: var(--nx-text-muted);
}

.home__checks {
  list-style: none;
  margin: 16px 0 0;
  padding: 6px 0 0;
  border-top: 1px solid var(--nx-hover);
}
.home__check { display: flex; align-items: center; gap: 12px; padding: 8px 0; }
.home__check-label { flex: 1; min-width: 0; font-size: var(--nx-text-base); color: var(--nx-text-2); }
.home__setup {
  flex: 0 0 auto;
  border: 1px solid var(--nx-warning-border);
  background: var(--nx-gold-soft);
  color: var(--nx-warning);
  border-radius: var(--nx-radius-sm);
  padding: 4px 12px;
  font-size: var(--nx-text-sm);
  font-weight: 500;
  white-space: nowrap;
}
.home__setup:hover { background: var(--nx-warning-soft); color: var(--nx-warning); }

.home__attn {
  display: flex;
  align-items: center;
  gap: 12px;
  border: 1px solid var(--nx-active);
  background: var(--nx-surface-2);
  border-radius: var(--nx-radius-md);
  padding: 12px 16px;
}
.home__dot { width: 7px; height: 7px; flex: 0 0 7px; border-radius: var(--nx-radius-full); }
.home__dot--critical { background: var(--nx-danger); }
.home__dot--warning { background: var(--nx-warning); }
.home__dot--info { background: var(--nx-info); }
.home__attn-label { display: block; font-size: var(--nx-text-base); font-weight: 500; }
.home__attn-sub {
  display: block;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
  padding-top: 4px;
}
.home__attn-btn {
  border: 1px solid var(--nx-border);
  background: var(--nx-surface);
  border-radius: var(--nx-radius-md);
  padding: 6px 12px;
  font-size: var(--nx-text-base);
  color: var(--nx-text-2);
  white-space: nowrap;
}
.home__attn-btn:hover { background: var(--nx-hover); color: var(--nx-text); }

.home__hint { font-size: var(--nx-text-sm); color: var(--nx-text-placeholder); }
.home__series-head { display: flex; align-items: baseline; gap: 8px; padding-bottom: 8px; }
.home__swatch { width: 7px; height: 7px; border-radius: var(--nx-radius-sm); }
.home__swatch--cpu { background: var(--nx-primary); }
.home__swatch--ram { background: var(--nx-stack-php); }
.home__series-name { flex: 1; font-size: var(--nx-text-base); font-weight: 500; }
.home__series-value { font-size: var(--nx-text-base); color: var(--nx-text-2); }
.home__divided { border-top: 1px solid var(--nx-hover); padding-top: 16px; }

.home__scan-foot { padding-top: 16px; }

.home__all { font-size: var(--nx-text-sm); color: var(--nx-primary-text); white-space: nowrap; }
.home__all:hover { text-decoration: underline; }
.home__activity { list-style: none; margin: 0; padding: 0; }
.home__act {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 0;
  border-bottom: 1px solid var(--nx-hover);
}
.home__act:last-child { border-bottom: 0; }
.home__act-icon { color: var(--nx-text-muted); }
.home__act-text { font-size: var(--nx-text-base); color: var(--nx-text-2); }
.home__act-when {
  font-size: var(--nx-text-sm);
  color: var(--nx-text-placeholder);
  white-space: nowrap;
}

.home__err {
  width: 100%;
  margin: 4px 0 0;
  background: var(--nx-dark);
  border-radius: var(--nx-radius-sm);
  padding: 8px 12px;
  font-family: "JetBrains Mono", ui-monospace, monospace;
  font-size: var(--nx-text-xs);
  line-height: 1.7;
  color: var(--nx-text-on-dark);
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
