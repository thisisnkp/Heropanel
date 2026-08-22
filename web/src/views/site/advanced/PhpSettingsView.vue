<script setup lang="ts">
/**
 * PHP settings — version and extensions on one tab, php.ini on the other.
 *
 * The two tabs are local state rather than routes: unlike Apps and Security,
 * the design does not treat these as separately addressable screens, and
 * "which half of the PHP page were you on" is not something worth putting in a
 * URL someone might share.
 */
import { computed, ref } from "vue";
import {
  PHP_EXT_AVAILABLE,
  PHP_EXT_ENABLED,
  PHP_INI_FLAGS,
  PHP_INI_RAW,
  PHP_VERSIONS,
  phpIniRows,
} from "@/data/siteDetail";
import { useSitesStore } from "@/stores/sites";
import { useUiStore } from "@/stores/ui";

const sites = useSitesStore();
const ui = useUiStore();

const site = computed(() => sites.current);
const tab = ref<"version" | "ini">("version");

const version = ref("8.4");
const versionMenuOpen = ref(false);

// Extensions the site does not have yet, ticked but not applied. Kept separate
// from the enabled list so "what is loaded" and "what you are about to load"
// stay distinguishable right up to the moment Apply is pressed.
const pendingExt = ref<Record<string, boolean>>({});
const iniFlags = ref<Record<string, boolean>>(
  Object.fromEntries(PHP_INI_FLAGS.map((f) => [f.key, f.on])),
);

const iniRows = computed(() => (site.value ? phpIniRows(site.value) : []));
const iniValues = ref<Record<string, string>>({});

function iniValue(label: string, fallback: string) {
  return iniValues.value[label] ?? fallback;
}

function pickVersion(v: string) {
  version.value = v;
  versionMenuOpen.value = false;
}

function notImplemented(label: string) {
  ui.toast(label + " is not wired up yet.", "info");
}
</script>

<template>
  <div v-if="site">
    <SiteHeader kicker="Runtime" title="PHP settings" sub="Version, limits and extensions for this site." />

    <div class="nx-view">
      <div class="php__tabs" role="tablist" aria-label="PHP settings sections">
        <button
          type="button"
          role="tab"
          class="php__tab"
          :class="{ 'is-current': tab === 'version' }"
          :aria-selected="tab === 'version'"
          @click="tab = 'version'"
        >
          Select PHP version
        </button>
        <button
          type="button"
          role="tab"
          class="php__tab"
          :class="{ 'is-current': tab === 'ini' }"
          :aria-selected="tab === 'ini'"
          @click="tab = 'ini'"
        >
          PHP INI editor
        </button>
      </div>

      <template v-if="tab === 'version'">
        <NxCard class="php__block">
          <div class="php__version-row">
            <NxMenu v-model:open="versionMenuOpen" align="start" width="100%">
              <template #trigger="{ toggle }">
                <div class="php__version-field">
                  <div class="php__label">PHP version</div>
                  <button type="button" class="php__version-btn nx-mono" :aria-expanded="versionMenuOpen" @click="toggle">
                    <span class="nx-row__grow">PHP {{ version }}</span>
                    <NxIcon :name="versionMenuOpen ? 'expand-less' : 'expand-more'" size="md" />
                  </button>
                </div>
              </template>

              <button
                v-for="v in PHP_VERSIONS"
                :key="v.version"
                type="button"
                class="php__version-option"
                :class="{ 'is-current': v.version === version }"
                role="menuitem"
                @click="pickVersion(v.version)"
              >
                <span class="nx-row__grow nx-mono">PHP {{ v.version }}</span>
                <span class="php__version-note">{{ v.note }}</span>
                <NxIcon v-if="v.version === version" name="check" size="sm" class="php__tick" />
              </button>
            </NxMenu>

            <NxButton variant="primary" size="lg" @click="notImplemented('Apply version')">Apply version</NxButton>
          </div>
          <p class="php__note">
            Switching restarts PHP-FPM for this site only — about two seconds, no file changes.
          </p>
        </NxCard>

        <NxCard title="Enabled extensions" class="php__block">
          <template #action><span class="php__hint">{{ PHP_EXT_ENABLED.length }} loaded</span></template>
          <div class="php__ext-grid">
            <div v-for="x in PHP_EXT_ENABLED" :key="x" class="php__ext is-on">
              <span class="php__box is-on" aria-hidden="true"><NxIcon name="check" size="sm" /></span>
              <span class="php__ext-name nx-mono nx-truncate">{{ x }}</span>
            </div>
          </div>
        </NxCard>

        <NxCard title="Select to enable">
          <template #action><span class="php__hint">tick, then apply</span></template>
          <div class="php__ext-grid">
            <label v-for="x in PHP_EXT_AVAILABLE" :key="x" class="php__ext">
              <input v-model="pendingExt[x]" type="checkbox" class="php__checkbox" />
              <span class="php__box" :class="{ 'is-on': pendingExt[x] }" aria-hidden="true">
                <NxIcon v-if="pendingExt[x]" name="check" size="sm" />
              </span>
              <span class="php__ext-name nx-mono nx-truncate">{{ x }}</span>
            </label>
          </div>
          <div class="php__foot">
            <span class="php__hint nx-row__grow">Enabling an extension reloads PHP-FPM.</span>
            <NxButton @click="pendingExt = {}">Reset</NxButton>
            <NxButton variant="primary" @click="notImplemented('Apply changes')">Apply changes</NxButton>
          </div>
        </NxCard>
      </template>

      <template v-else>
        <NxCard title="On / off directives" class="php__block">
          <template #action><span class="php__hint">PHP {{ version }} · php.ini</span></template>
          <div class="php__flag-grid">
            <label v-for="f in PHP_INI_FLAGS" :key="f.key" class="php__ext">
              <input v-model="iniFlags[f.key]" type="checkbox" class="php__checkbox" />
              <span class="php__box" :class="{ 'is-on': iniFlags[f.key] }" aria-hidden="true">
                <NxIcon v-if="iniFlags[f.key]" name="check" size="sm" />
              </span>
              <span class="php__flag-name nx-mono">{{ f.key }}</span>
            </label>
          </div>
        </NxCard>

        <NxCard title="Value directives" class="php__block">
          <div class="php__values">
            <NxField v-for="r in iniRows" :key="r.label" :label="r.label">
              <template #default="{ id }">
                <NxInput
                  :id="id"
                  mono
                  :model-value="iniValue(r.label, r.value)"
                  @update:model-value="iniValues[r.label] = $event"
                />
              </template>
            </NxField>
          </div>
          <div class="php__foot">
            <span class="php__hint nx-row__grow">Changes apply on save and reload PHP-FPM.</span>
            <NxButton @click="iniValues = {}">Reset to defaults</NxButton>
            <NxButton variant="primary" @click="notImplemented('Save')">Save</NxButton>
          </div>
        </NxCard>

        <NxLogPanel
          title="php.ini · raw editor"
          :lines="PHP_INI_RAW.map((text) => ({ text }))"
        >
          <template #actions>
            <NxButton size="sm" class="php__dark-btn" @click="notImplemented('Validate')">Validate</NxButton>
            <NxButton size="sm" class="php__dark-btn" @click="notImplemented('Download')">Download</NxButton>
          </template>
        </NxLogPanel>
      </template>
    </div>
  </div>
  <NxSkeleton v-else height="200px" />
</template>

<style scoped>
.php__block { margin-bottom: 12px; }
.php__tabs {
  display: flex;
  gap: 4px;
  border-bottom: 1px solid var(--nx-border);
  margin-bottom: 20px;
}
.php__tab {
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
.php__tab:hover { color: var(--nx-text); }
.php__tab.is-current { color: var(--nx-text); font-weight: 600; border-bottom-color: var(--nx-primary); }

.php__version-row { display: flex; align-items: flex-end; gap: 16px; flex-wrap: wrap; }
.php__version-field { flex: 0 1 260px; min-width: 200px; }
.php__label { font-size: var(--nx-text-sm); color: var(--nx-text-muted); padding-bottom: 6px; }
.php__version-btn {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  text-align: left;
  border: 1px solid var(--nx-border);
  background: var(--nx-surface);
  border-radius: var(--nx-radius-md);
  padding: 12px;
  font-size: var(--nx-text-base);
  cursor: pointer;
  color: var(--nx-text);
}
.php__version-btn:hover { border-color: var(--nx-border-strong); }
.php__version-option {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  text-align: left;
  border: 0;
  background: transparent;
  cursor: pointer;
  padding: 8px 12px;
  border-radius: var(--nx-radius-md);
  font-family: inherit;
  font-size: var(--nx-text-base);
  color: var(--nx-text);
}
.php__version-option:hover { background: var(--nx-hover); }
.php__version-option.is-current { background: var(--nx-hover); font-weight: 600; }
.php__version-note { font-size: var(--nx-text-xs); color: var(--nx-text-muted); }
.php__tick { color: var(--nx-primary); }
.php__note {
  margin: 12px 0 0;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
  text-wrap: pretty;
}
.php__hint { font-size: var(--nx-text-sm); color: var(--nx-text-muted); }

.php__ext-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(126px, 1fr));
  gap: 8px;
}
.php__flag-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
}
@media (max-width: 900px) {
  .php__flag-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
.php__ext {
  display: flex;
  align-items: center;
  gap: 8px;
  text-align: left;
  border: 1px solid var(--nx-active);
  background: var(--nx-surface);
  border-radius: var(--nx-radius-md);
  padding: 8px 12px;
  cursor: pointer;
  min-width: 0;
}
.php__ext:hover { background: var(--nx-hover); }
.php__ext.is-on { background: var(--nx-surface-2); cursor: default; }
.php__checkbox { position: absolute; opacity: 0; width: 0; height: 0; }
.php__box {
  width: 15px;
  height: 15px;
  flex: 0 0 15px;
  border: 1.5px solid var(--nx-border-strong);
  border-radius: var(--nx-radius-sm);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--nx-primary-on);
}
.php__box.is-on { background: var(--nx-primary); border-color: var(--nx-primary); }
.php__checkbox:focus-visible + .php__box { outline: 2px solid var(--nx-focus-ring); outline-offset: 2px; }
.php__ext-name { flex: 1; min-width: 0; font-size: var(--nx-text-base); color: var(--nx-text); }
.php__flag-name {
  flex: 1;
  min-width: 0;
  font-size: var(--nx-text-sm);
  line-height: 1.35;
  color: var(--nx-text);
  overflow-wrap: anywhere;
}

.php__values {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 12px;
  max-width: 520px;
}
.php__foot { display: flex; align-items: center; gap: 12px; padding-top: 16px; flex-wrap: wrap; }

.php__dark-btn {
  border-color: var(--nx-dark-border-2);
  background: transparent;
  color: var(--nx-text-on-dark);
}
.php__dark-btn:hover { background: var(--nx-dark-border); }
</style>
