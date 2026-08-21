<script setup lang="ts">
/**
 * Git deployments — the connected repository and the full deploy history.
 *
 * A site with no repo connected does not get an empty history table; it gets
 * the one action that changes that, pointing at the setup screen. An empty
 * table here would read as "no deploys yet" when the truth is "nothing is
 * connected".
 */
import { computed, ref } from "vue";
import { DEPLOYS } from "@/data/siteDetail";
import { useJobsStore } from "@/stores/jobs";
import { useSitesStore } from "@/stores/sites";

const sites = useSitesStore();
const jobs = useJobsStore();

const site = computed(() => sites.current);
const connected = computed(() => site.value?.deploy === "GitHub");
const buildCommand = computed(() => (site.value?.stackKey === "node" ? "npm ci && npm run build" : "no build step"));

const autoDeploy = ref(true);
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
    <SiteHeader
      kicker="Deployment"
      title="Git deployments"
      sub="Connected repository, auto-deploy and full history."
    />

    <div class="nx-view">
      <NxEmptyState
        v-if="!connected"
        icon="cloud-sync"
        title="No repository connected"
        description="Connect a GitHub repository and every push to your branch goes live automatically."
      >
        <NxButton variant="primary" @click="$router.push({ name: 'site-git-setup' })">Connect a repository</NxButton>
      </NxEmptyState>

      <template v-else>
        <NxCard class="git__head">
          <div class="git__repo-row">
            <div class="nx-row__grow">
              <div class="git__repo">{{ site.repo }}</div>
              <div class="git__meta nx-mono">branch {{ site.branch }} · {{ buildCommand }}</div>
            </div>
            <NxToggle v-model="autoDeploy" label="Auto-deploy on push" />
            <NxButton variant="primary" :loading="redeploying" @click="redeploy">
              {{ redeploying ? "Deploying…" : "Redeploy" }}
            </NxButton>
          </div>
        </NxCard>

        <NxCard title="Deployment history" flush>
          <NxTable
            :columns="[
              { key: 'dot', label: '', width: '8px' },
              { key: 'msg', label: 'Commit', width: '2fr' },
              { key: 'sha', label: 'SHA', width: '1fr' },
              { key: 'when', label: 'When', width: '0.7fr' },
              { key: 'state', label: 'State', width: '78px', align: 'end' },
            ]"
            :rows="DEPLOYS"
            :row-key="(d) => d.sha"
          >
            <template #default="{ row }">
              <span class="git__dot" :class="row.state === 'Failed' ? 'is-failed' : 'is-ok'" aria-hidden="true" />
              <div class="nx-truncate">{{ row.message }}</div>
              <div class="git__muted nx-mono">{{ row.sha }}</div>
              <div class="git__muted">{{ row.when }}</div>
              <div class="git__state" :class="row.state === 'Failed' ? 'is-failed' : ''">{{ row.state }}</div>
            </template>
          </NxTable>
        </NxCard>
      </template>
    </div>
  </div>
  <NxSkeleton v-else height="200px" />
</template>

<style scoped>
.git__head { margin-bottom: 12px; }
.git__repo-row { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; }
.git__repo { font-size: var(--nx-text-base); font-weight: 600; }
.git__meta {
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
  padding-top: 4px;
}
.git__dot {
  width: 8px;
  height: 8px;
  border-radius: var(--nx-radius-full);
  display: block;
}
.git__dot.is-ok { background: var(--nx-success); }
.git__dot.is-failed { background: var(--nx-danger); }
.git__muted { color: var(--nx-text-muted); }
.git__state { text-align: right; color: var(--nx-text-muted); }
.git__state.is-failed { color: var(--nx-danger); }
</style>
