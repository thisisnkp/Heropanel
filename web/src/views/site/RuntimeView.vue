<script setup lang="ts">
/**
 * Runtime — the interpreter configuration and process control.
 *
 * PHP and WordPress sites are redirected to the PHP settings screen instead:
 * for them the "runtime" is php.ini and extensions, which is a bigger screen
 * with its own tabs. Showing both would give a PHP site two half-answers to
 * the same question.
 */
import { computed, ref } from "vue";
import { processNote, runtimeFields, runtimeTitle } from "@/data/siteDetail";
import { useSitesStore } from "@/stores/sites";
import { useUiStore } from "@/stores/ui";

const sites = useSitesStore();
const ui = useUiStore();

const site = computed(() => sites.current);
const isPhp = computed(() => site.value?.stackKey === "php" || site.value?.stackKey === "wp");
const fields = computed(() => (site.value ? runtimeFields(site.value) : []));

const restarting = ref(false);
function restart() {
  restarting.value = true;
  window.setTimeout(() => {
    restarting.value = false;
    ui.toast("Restarted " + (site.value?.domain ?? "the app") + ".", "success");
  }, 1500);
}
</script>

<template>
  <div v-if="site">
    <SiteHeader
      kicker="Runtime"
      :title="isPhp ? 'PHP settings' : runtimeTitle(site)"
      sub="Version, start command and process control."
    />

    <div class="nx-view">
      <NxCallout v-if="isPhp" tone="info" title="PHP is configured on its own screen">
        Version, limits, extensions and php.ini for this site live under PHP settings.
        <template #actions>
          <NxButton variant="primary" @click="$router.push({ name: 'site-php' })">Open PHP settings</NxButton>
        </template>
      </NxCallout>

      <div v-else class="nx-grid nx-grid--2">
        <NxCard :title="runtimeTitle(site)">
          <div class="nx-stack">
            <div v-for="f in fields" :key="f.label">
              <div class="rt__label">{{ f.label }}</div>
              <div class="rt__value nx-mono nx-truncate">{{ f.value }}</div>
            </div>
          </div>
        </NxCard>

        <NxCard title="Process control" class="rt__side">
          <p class="rt__note">{{ processNote(site) }}</p>
          <div class="rt__spacer" />
          <div class="rt__actions">
            <NxButton variant="primary" :loading="restarting" @click="restart">
              {{ restarting ? "Restarting…" : "Restart app" }}
            </NxButton>
            <NxButton @click="ui.toast('Stop is not wired up yet.', 'info')">Stop</NxButton>
          </div>
        </NxCard>
      </div>
    </div>
  </div>
  <NxSkeleton v-else height="200px" />
</template>

<style scoped>
.rt__label { font-size: var(--nx-text-sm); color: var(--nx-text-muted); padding-bottom: 6px; }
.rt__value {
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-md);
  padding: 8px 12px;
  font-size: var(--nx-text-base);
  background: var(--nx-surface-2);
  color: var(--nx-text-2);
}
.rt__side :deep(.nx-card__body) { display: flex; flex-direction: column; height: 100%; }
.rt__note {
  margin: 0;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
  text-wrap: pretty;
  line-height: 1.55;
}
.rt__spacer { flex: 1; }
.rt__actions { display: flex; gap: 8px; padding-top: 20px; }
</style>
