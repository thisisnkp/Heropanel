<script setup lang="ts">
/** Plugins and updates — core, plugins, theme and cache for this install. */
import { computed } from "vue";
import { WP_ITEMS } from "@/data/siteDetail";
import { useSitesStore } from "@/stores/sites";
import { useUiStore } from "@/stores/ui";

const sites = useSitesStore();
const ui = useUiStore();

const site = computed(() => sites.current);
const pending = computed(() => WP_ITEMS.filter((w) => w.state === "update").length);

function openAdmin() {
  if (!site.value) return;
  window.open("https://" + site.value.domain + "/wp-admin", "_blank", "noopener");
}
</script>

<template>
  <div v-if="site">
    <SiteHeader
      kicker="WordPress manager"
      title="Plugins and updates"
      sub="Core, plugins, theme and cache for this install."
    />

    <div class="nx-view nx-grid nx-grid--2">
      <NxCard title="WordPress 6.7.1">
        <p class="wp__ok">Up to date · auto-updates on</p>
        <div class="wp__actions">
          <NxButton variant="primary" @click="openAdmin">Open wp-admin</NxButton>
          <NxButton @click="ui.toast('Cache cleared for ' + site.domain + '.', 'success')">Clear cache</NxButton>
        </div>
      </NxCard>

      <NxCard title="Plugins &amp; theme">
        <template #action>
          <span class="wp__hint">{{ pending }} update{{ pending === 1 ? "" : "s" }} pending</span>
        </template>

        <ul class="wp__list">
          <li v-for="w in WP_ITEMS" :key="w.name" class="wp__item">
            <span class="nx-row__grow nx-truncate">{{ w.name }}</span>
            <span class="wp__ver nx-mono">{{ w.version }}</span>
            <NxBadge :tone="w.state === 'update' ? 'warning' : 'success'">{{ w.state }}</NxBadge>
          </li>
        </ul>
      </NxCard>
    </div>
  </div>
  <NxSkeleton v-else height="200px" />
</template>

<style scoped>
.wp__ok { margin: 0; font-size: var(--nx-text-base); color: var(--nx-success); }
.wp__actions { display: flex; gap: 8px; padding-top: 16px; flex-wrap: wrap; }
.wp__hint { font-size: var(--nx-text-sm); color: var(--nx-text-muted); }
.wp__list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 8px; }
.wp__item { display: flex; align-items: center; gap: 12px; font-size: var(--nx-text-base); }
.wp__ver { font-size: var(--nx-text-sm); color: var(--nx-text-muted); }
</style>
