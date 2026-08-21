<script setup lang="ts">
/**
 * Site overview — speed, health and security at a glance.
 *
 * The stat row is `auto-fit` rather than a fixed column count because how many
 * cards there are depends on the site: a WordPress site gains a pending-updates
 * card, a Git-connected site gains a last-deploy card, and a plain static site
 * has neither.
 */
import { computed, ref } from "vue";
import { SITE_SECURITY, quickActions } from "@/data/siteDetail";
import { useJobsStore } from "@/stores/jobs";
import { useSitesStore } from "@/stores/sites";

const sites = useSitesStore();
const jobs = useJobsStore();

const site = computed(() => sites.current);
const actions = computed(() => (site.value ? quickActions(site.value) : []));
const isWordPress = computed(() => site.value?.stackKey === "wp");
const isGit = computed(() => site.value?.stackKey !== "wp" && site.value?.deploy === "GitHub");

const redeploying = ref(false);

/**
 * A deploy outlives this screen, so it is reported in the job tray rather than
 * as a toast. A toast is gone in four seconds and takes the only record of the
 * deploy with it; the tray keeps the progress visible while you go and look at
 * the logs, which is the next thing anyone does.
 */
function redeploy() {
  if (!site.value) return;
  redeploying.value = true;
  jobs.start("Deploy " + (site.value.branch ?? "main") + "@4f2a1c9", site.value.domain, "Fetching repository");
  window.setTimeout(() => (redeploying.value = false), 1800);
}
</script>

<template>
  <div v-if="site">
    <SiteHeader kicker="Site" title="Overview" sub="Speed, health and security at a glance." />

    <div class="nx-view">
      <div class="ov__stats">
        <RouterLink :to="{ name: 'site-pagespeed' }" class="ov__card ov__card--link">
          <div class="ov__card-label">PageSpeed</div>
          <div class="ov__card-figure">
            <span class="ov__score">92</span>
            <span class="ov__card-sub">mobile · LCP 1.4 s</span>
          </div>
          <NxMeter :value="92" tone="success" height="5px" class="ov__card-bar" />
        </RouterLink>

        <div class="ov__card">
          <div class="ov__card-label">Status</div>
          <div class="ov__status">
            <span class="ov__dot" :class="'ov__dot--' + site.status" aria-hidden="true" />
            <span class="ov__status-text">{{ site.status === "live" ? "Live" : "Building" }}</span>
          </div>
          <div class="ov__card-sub">99.98% uptime, last 30 days</div>
        </div>

        <div class="ov__card">
          <div class="ov__card-label">Domain &amp; SSL</div>
          <div class="ov__card-value nx-mono nx-truncate">{{ site.domain }}</div>
          <div class="ov__card-sub ov__ok">SSL active · auto-renews</div>
        </div>

        <div v-if="isWordPress" class="ov__card">
          <div class="ov__card-label">Pending updates</div>
          <div class="ov__card-figure">
            <span class="ov__score ov__score--warn">2</span>
            <span class="ov__card-sub">1 plugin, 1 theme</span>
          </div>
          <NxButton class="ov__card-btn" @click="$router.push({ name: 'site-wp-plugins' })">Review updates</NxButton>
        </div>

        <div v-if="isGit" class="ov__card">
          <div class="ov__card-label">Last deploy</div>
          <div class="ov__card-value nx-truncate">{{ site.lastDeploy }}</div>
          <NxButton class="ov__card-btn" :loading="redeploying" @click="redeploy">
            {{ redeploying ? "Deploying…" : "Redeploy" }}
          </NxButton>
        </div>
      </div>

      <div class="nx-grid ov__split">
        <NxCard title="Quick actions">
          <template #action>
            <span class="ov__hint">most used for this site</span>
          </template>
          <div class="nx-stack nx-stack--sm">
            <RouterLink
              v-for="q in actions"
              :key="q.to"
              :to="{ name: q.to, params: { id: String(site.id) } }"
              class="ov__quick"
              :target="q.newTab ? '_blank' : undefined"
              :rel="q.newTab ? 'noopener' : undefined"
            >
              <span class="ov__quick-icon" :class="'ov__quick-icon--' + q.tone">
                <NxIcon :name="q.icon" size="sm" />
              </span>
              <span class="nx-row__grow">
                <span class="ov__quick-label nx-truncate">{{ q.label }}</span>
                <span class="ov__quick-sub nx-mono nx-truncate">{{ q.sub }}</span>
              </span>
              <NxIcon :name="q.newTab ? 'open-in-new' : 'chevron-right'" size="sm" class="ov__chevron" />
            </RouterLink>
          </div>
        </NxCard>

        <NxCard title="Security" class="ov__sec">
          <template #action>
            <span class="ov__verdict">
              <NxIcon name="shield" size="sm" />
              {{ SITE_SECURITY.verdict }}
            </span>
          </template>

          <div class="ov__sec-score">
            <span class="ov__sec-number">{{ SITE_SECURITY.score }}</span>
            <span class="ov__card-sub">/100</span>
          </div>
          <NxMeter :value="SITE_SECURITY.score" tone="success" height="6px" class="ov__sec-bar" />

          <ul class="ov__checks">
            <li v-for="c in SITE_SECURITY.checks" :key="c.label">
              <RouterLink :to="{ name: c.to }" class="ov__check">
                <NxIcon
                  :name="c.ok ? 'check-circle' : 'error'"
                  size="md"
                  :class="c.ok ? 'ov__ok' : 'ov__bad'"
                />
                <span class="nx-row__grow">
                  <span class="ov__check-label nx-truncate">{{ c.label }}</span>
                  <span class="ov__check-sub nx-truncate">{{ c.sub }}</span>
                </span>
                <NxIcon name="chevron-right" size="sm" class="ov__chevron" />
              </RouterLink>
            </li>
          </ul>

          <div class="ov__spacer" />
          <div class="ov__sec-actions">
            <NxButton variant="primary" @click="$router.push({ name: 'site-malware' })">Run a scan</NxButton>
            <NxButton @click="$router.push({ name: 'site-ssl' })">SSL details</NxButton>
          </div>
        </NxCard>
      </div>
    </div>
  </div>
  <NxSkeleton v-else height="200px" />
</template>

<style scoped>
.ov__stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
  gap: 12px;
  padding-bottom: 12px;
}
.ov__card {
  background: var(--nx-surface);
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-lg);
  padding: 16px;
  min-width: 0;
}
.ov__card--link { display: block; color: inherit; transition: border-color 140ms ease; }
.ov__card--link:hover { border-color: var(--nx-border-strong); color: inherit; }
.ov__card-label { font-size: var(--nx-text-sm); color: var(--nx-text-muted); }
.ov__card-figure { display: flex; align-items: baseline; gap: 6px; padding-top: 8px; }
.ov__card-value {
  font-size: var(--nx-text-md);
  font-weight: 500;
  padding-top: 8px;
  color: var(--nx-text);
}
.ov__card-sub { font-size: var(--nx-text-sm); color: var(--nx-text-muted); padding-top: 4px; }
.ov__card-figure .ov__card-sub { padding-top: 0; }
.ov__card-bar { margin-top: 8px; }
.ov__card-btn { margin-top: 8px; }
.ov__score {
  font-size: var(--nx-text-lg);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
  color: var(--nx-success);
}
.ov__score--warn { color: var(--nx-warning); }
.ov__ok { color: var(--nx-success); }
.ov__bad { color: var(--nx-danger); }

.ov__status { display: flex; align-items: center; gap: 8px; padding-top: 8px; }
.ov__dot { width: 9px; height: 9px; border-radius: var(--nx-radius-full); }
.ov__dot--live { background: var(--nx-success); }
.ov__dot--building { background: var(--nx-warning); }
.ov__dot--suspended { background: var(--nx-text-placeholder); }
.ov__status-text {
  font-size: var(--nx-text-lg);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
}

.ov__split { grid-template-columns: minmax(0, 1.25fr) minmax(0, 1fr); }
@media (max-width: 1100px) {
  .ov__split { grid-template-columns: minmax(0, 1fr); }
}

.ov__hint { font-size: var(--nx-text-sm); color: var(--nx-text-placeholder); }
.ov__verdict {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: var(--nx-text-sm);
  color: var(--nx-success);
  font-weight: 500;
  white-space: nowrap;
}

.ov__quick {
  display: flex;
  align-items: center;
  gap: 12px;
  border: 1px solid var(--nx-active);
  background: var(--nx-surface);
  border-radius: var(--nx-radius-md);
  padding: 8px 12px;
  color: inherit;
  transition: background 140ms ease, border-color 140ms ease;
}
.ov__quick:hover { background: var(--nx-surface-2); border-color: var(--nx-border-strong); color: inherit; }
.ov__quick-icon {
  width: 26px;
  height: 26px;
  flex: 0 0 26px;
  border-radius: var(--nx-radius-md);
  display: flex;
  align-items: center;
  justify-content: center;
}
.ov__quick-icon--brand { background: var(--nx-info-soft); color: var(--nx-primary); }
.ov__quick-icon--success { background: var(--nx-stack-node-soft); color: var(--nx-stack-node); }
.ov__quick-icon--warning { background: var(--nx-warning-soft); color: var(--nx-warning); }
.ov__quick-icon--danger { background: var(--nx-danger-soft); color: var(--nx-danger); }
.ov__quick-label {
  display: block;
  font-size: var(--nx-text-base);
  font-weight: 500;
  color: var(--nx-text);
}
.ov__quick-sub {
  display: block;
  font-size: var(--nx-text-xs);
  color: var(--nx-text-placeholder);
}
.ov__chevron { color: var(--nx-border-strong); }

.ov__sec :deep(.nx-card__body) { display: flex; flex-direction: column; height: 100%; }
.ov__sec-score { display: flex; align-items: baseline; gap: 6px; }
.ov__sec-number {
  font-size: var(--nx-text-2xl);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
  line-height: 1;
  color: var(--nx-success);
}
.ov__sec-bar { margin: 12px 0 4px; }
.ov__checks { list-style: none; margin: 6px 0 0; padding: 0; }
.ov__check {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 0;
  border-bottom: 1px solid var(--nx-hover);
  color: inherit;
}
.ov__check:hover .ov__check-label { color: var(--nx-text); }
.ov__check-label { display: block; font-size: var(--nx-text-base); color: var(--nx-text-2); }
.ov__check-sub { display: block; font-size: var(--nx-text-xs); color: var(--nx-text-placeholder); }
.ov__spacer { flex: 1; }
.ov__sec-actions { display: flex; gap: 8px; padding-top: 16px; }
</style>
