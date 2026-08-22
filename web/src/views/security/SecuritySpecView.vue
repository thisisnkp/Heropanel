<script setup lang="ts">
/**
 * The nine server-security screens.
 *
 * One component parameterised by `securityKey`, the same arrangement the site
 * screens use. Three of the nine have a choice that changes what the screen
 * shows — WAF level, security profile, log source — so the choice is bound to
 * the store rather than left local, and the spec is rebuilt when it changes.
 */
import { computed } from "vue";
import { buildSecuritySpec, type LogSource, type SecurityKey } from "@/data/securitySpec";
import { useFlagsStore } from "@/stores/flags";
import { useSecurityStore } from "@/stores/security";

const props = defineProps<{ securityKey: SecurityKey }>();

const flags = useFlagsStore();
const security = useSecurityStore();

const spec = computed(() =>
  buildSecuritySpec(props.securityKey, {
    flags,
    wafLevel: security.wafLevel,
    profile: security.profile,
    logSource: security.logSource,
    scanning: security.scanning,
  }),
);

/**
 * Which store field this screen's choice writes to. Screens not listed keep the
 * selection local to the component.
 */
const choice = computed({
  get() {
    if (props.securityKey === "waf") return security.wafLevel;
    if (props.securityKey === "settings") return security.profile;
    if (props.securityKey === "logs") return security.logSource;
    return "";
  },
  set(v: string) {
    if (props.securityKey === "waf") security.wafLevel = v;
    else if (props.securityKey === "settings") security.profile = v;
    else if (props.securityKey === "logs") security.logSource = v as LogSource;
  },
});

const bindsChoice = computed(() =>
  props.securityKey === "waf" || props.securityKey === "settings" || props.securityKey === "logs",
);
</script>

<template>
  <div>
    <header class="secv__head">
      <p class="secv__kicker">{{ spec.kicker }}</p>
      <h1 class="secv__title">{{ spec.title }}</h1>
      <p class="secv__sub">{{ spec.sub }}</p>
    </header>

    <!-- The one live piece on these screens: which scanners this host can
         actually run. Everything else here is still a fixture, and "is maldet
         installed" is the one question a fixture cannot answer without lying. -->
    <MalwareEngines v-if="securityKey === 'malware'" />

    <SpecScreen v-if="bindsChoice" v-model:choice="choice" :spec="spec" />
    <SpecScreen v-else :spec="spec" />
  </div>
</template>

<style scoped>
.secv__head { padding-bottom: 24px; }
.secv__kicker {
  margin: 0;
  font-size: var(--nx-text-xs);
  letter-spacing: var(--nx-ls-caps);
  color: var(--nx-text-placeholder);
  font-weight: 600;
  text-transform: uppercase;
}
.secv__title {
  margin: 6px 0 0;
  font-size: var(--nx-text-xl);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
}
.secv__sub {
  margin: 6px 0 0;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
  text-wrap: pretty;
}
</style>
