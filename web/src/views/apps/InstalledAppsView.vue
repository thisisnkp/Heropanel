<script setup lang="ts">
/** Installed apps — everything running alongside the websites. */
import { INSTALLED_APPS, APP_STATS } from "@/data/apps";
import { useUiStore } from "@/stores/ui";

const ui = useUiStore();

const TONE = { Running: "success", "Update ready": "warning", Stopped: "neutral" } as const;
</script>

<template>
  <div class="nx-view">
    <header class="apps__head">
      <p class="apps__kicker">Apps</p>
      <h1 class="apps__title">Installed apps</h1>
      <p class="apps__sub">
        Everything running alongside your websites. Open, update or remove in one place.
      </p>
    </header>

    <div class="nx-grid nx-grid--4 apps__block">
      <NxStat v-for="s in APP_STATS" :key="s.label" :label="s.label" :value="s.value" :sub="s.sub" />
    </div>

    <NxCard flush>
      <ul class="apps__list">
        <li v-for="a in INSTALLED_APPS" :key="a.name" class="apps__row">
          <span class="apps__icon"><NxIcon :name="a.icon" size="lg" /></span>
          <div class="nx-row__grow">
            <div class="apps__name-row">
              <span class="apps__name">{{ a.name }}</span>
              <span class="apps__ver nx-mono">v{{ a.version }}</span>
              <NxBadge :tone="TONE[a.state]">{{ a.state }}</NxBadge>
              <span v-if="a.licensed" class="apps__licensed">Licensed</span>
            </div>
            <p class="apps__row-sub">{{ a.sub }}</p>
          </div>
          <div class="apps__actions">
            <NxButton v-if="a.to" @click="$router.push({ name: a.to })">Open</NxButton>
            <NxButton v-else @click="ui.toast('Open is not wired up yet.', 'info')">Open</NxButton>
            <NxButton @click="ui.toast('Settings are not wired up yet.', 'info')">Settings</NxButton>
            <NxButton variant="danger" @click="ui.toast('Uninstall is not wired up yet.', 'info')">Uninstall</NxButton>
          </div>
        </li>
      </ul>
    </NxCard>
  </div>
</template>

<style scoped>
.apps__head { padding-bottom: 24px; }
.apps__kicker {
  margin: 0;
  font-size: var(--nx-text-xs);
  letter-spacing: var(--nx-ls-caps);
  color: var(--nx-text-placeholder);
  font-weight: 600;
  text-transform: uppercase;
}
.apps__title {
  margin: 6px 0 0;
  font-size: var(--nx-text-xl);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
}
.apps__sub {
  margin: 6px 0 0;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
  text-wrap: pretty;
}
.apps__block { margin-bottom: 12px; }

.apps__list { list-style: none; margin: 0; padding: 0; }
.apps__row {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px;
  border-bottom: 1px solid var(--nx-hover);
  flex-wrap: wrap;
}
.apps__row:last-child { border-bottom: 0; }
.apps__icon {
  width: 38px;
  height: 38px;
  flex: 0 0 38px;
  border-radius: var(--nx-radius-md);
  background: var(--nx-bg);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--nx-text-2);
}
.apps__name-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.apps__name {
  font-size: var(--nx-text-md);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
}
.apps__ver { font-size: var(--nx-text-sm); color: var(--nx-text-muted); }
.apps__licensed { font-size: var(--nx-text-xs); color: var(--nx-stack-php); }
.apps__row-sub {
  margin: 4px 0 0;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
}
.apps__actions { display: flex; gap: 8px; flex-wrap: wrap; }
</style>
