<script setup lang="ts">
/**
 * The Security context sidebar.
 *
 * Security is nine screens, which is more than a tab strip can carry without
 * either wrapping or scrolling sideways — and the design gives it a sidebar for
 * that reason. Moving it out of the tab strip also frees the top of the page for
 * the section's own heading, so the page still has one <h1> that names where you
 * are.
 *
 * The chips are computed from the flag store, not fixed: see securityIssues().
 */
import { computed } from "vue";
import { useRoute } from "vue-router";
import ContextSidebar from "./ContextSidebar.vue";
import ContextNavRow from "./ContextNavRow.vue";
import { SECURITY_SECTIONS, profileNote, securityIssues } from "@/data/securitySpec";
import { useFlagsStore } from "@/stores/flags";
import { useSecurityStore } from "@/stores/security";

const route = useRoute();
const flags = useFlagsStore();
const security = useSecurityStore();

const issues = computed(() => securityIssues(flags));
const critical = computed(() => issues.value.filter((i) => i.severity === "critical"));
const warnings = computed(() => issues.value.filter((i) => i.severity === "warning"));

/** The chip's tooltip lists what it is counting, so the number is checkable. */
function names(list: readonly { label: string }[]) {
  return list.map((i) => i.label).join(", ");
}

function routeName(key: string) {
  return "security-" + key;
}
</script>

<template>
  <ContextSidebar
    nav-label="Security sections"
    back-to="home"
    back-label="Back to panel"
    title="Security"
    footer-caption="PROFILE"
    :footer-text="profileNote(security.profile)"
  >
    <template #chips>
      <span v-if="critical.length" class="sec__chip is-critical" :title="names(critical)">
        {{ critical.length }} critical
      </span>
      <span v-if="warnings.length" class="sec__chip is-warning" :title="names(warnings)">
        {{ warnings.length }} {{ warnings.length === 1 ? "warning" : "warnings" }}
      </span>
      <span v-if="!issues.length" class="sec__chip is-clear">All clear</span>
    </template>

    <ContextNavRow
      v-for="section in SECURITY_SECTIONS"
      :key="section.key"
      :label="section.label"
      :icon="section.icon"
      :to="routeName(section.key)"
      :current="route.name === routeName(section.key)"
    />
  </ContextSidebar>
</template>

<style scoped>
.sec__chip {
  font-size: var(--nx-text-xs);
  padding: 4px 8px;
  border-radius: var(--nx-radius-sm);
  font-weight: 500;
  white-space: nowrap;
}
.sec__chip.is-critical { background: var(--nx-danger-soft); color: var(--nx-danger); }
.sec__chip.is-warning { background: var(--nx-gold-soft); color: var(--nx-warning); }
.sec__chip.is-clear { background: var(--nx-success-soft); color: var(--nx-success); }
</style>
