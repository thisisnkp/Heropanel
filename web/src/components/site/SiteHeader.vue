<script setup lang="ts">
/**
 * The header every site screen opens with: which section you are in, the
 * domain as a live link, and the two actions that apply everywhere inside a
 * site.
 *
 * The domain is an <a target="_blank"> to the real site rather than a heading
 * with a click handler — people expect to be able to middle-click or copy it,
 * and the design drew it with an open-in-new affordance for exactly that.
 */
import { computed, ref } from "vue";
import { useSitesStore } from "@/stores/sites";
import { useUiStore } from "@/stores/ui";

defineProps<{ kicker: string; title: string; sub: string }>();

const sites = useSitesStore();
const ui = useUiStore();

const site = computed(() => sites.current);
const url = computed(() => (site.value ? "https://" + site.value.domain : "#"));
const isWordPress = computed(() => site.value?.stackKey === "wp");

// The design flips this button to a confirmation for a couple of seconds rather
// than firing a toast, so the feedback lands next to the thing you clicked.
const cleared = ref(false);
let clearTimer = 0;

function clearCache() {
  cleared.value = true;
  ui.toast("Cache cleared for " + (site.value?.domain ?? "this site") + ".", "success");
  window.clearTimeout(clearTimer);
  clearTimer = window.setTimeout(() => (cleared.value = false), 2200);
}
</script>

<template>
  <header class="shead">
    <div class="nx-row__grow">
      <p class="shead__kicker">{{ kicker }}</p>
      <a :href="url" target="_blank" rel="noopener" class="shead__link" title="Open in a new tab">
        <h1 class="shead__domain nx-truncate">{{ site?.domain ?? "…" }}</h1>
        <NxIcon name="open-in-new" size="md" class="shead__ext" />
      </a>
      <p class="shead__sub">{{ title }} · {{ sub }}</p>
    </div>

    <a
      v-if="isWordPress"
      :href="url + '/wp-admin'"
      target="_blank"
      rel="noopener"
      class="shead__wp"
    >
      <NxIcon name="extension" size="md" />
      Open wp-admin
    </a>

    <NxButton size="lg" @click="clearCache">
      <NxIcon :name="cleared ? 'check' : 'cleaning-services'" size="md" />
      {{ cleared ? "Cache cleared" : "Clear cache" }}
    </NxButton>
  </header>
</template>

<style scoped>
.shead {
  display: flex;
  align-items: flex-end;
  gap: 16px;
  padding-bottom: 24px;
  flex-wrap: wrap;
}
.shead__kicker {
  margin: 0;
  font-size: var(--nx-text-xs);
  letter-spacing: var(--nx-ls-caps);
  color: var(--nx-text-placeholder);
  font-weight: 600;
  text-transform: uppercase;
}
.shead__link {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding-top: 6px;
  max-width: 100%;
  color: var(--nx-text);
}
.shead__link:hover { color: var(--nx-primary); }
.shead__domain {
  margin: 0;
  font-size: var(--nx-text-xl);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
  min-width: 0;
  color: inherit;
}
.shead__ext { color: inherit; opacity: 0.55; }
.shead__sub {
  margin: 6px 0 0;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
  text-wrap: pretty;
}
.shead__wp {
  display: flex;
  align-items: center;
  gap: 8px;
  border: 1px solid var(--nx-primary-border);
  background: var(--nx-info-soft);
  border-radius: var(--nx-radius-md);
  padding: 12px 16px;
  font-size: var(--nx-text-base);
  font-weight: 500;
  color: var(--nx-primary);
  white-space: nowrap;
}
.shead__wp:hover { background: var(--nx-primary-soft); color: var(--nx-primary); }
</style>
