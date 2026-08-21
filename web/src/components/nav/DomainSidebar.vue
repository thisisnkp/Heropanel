<script setup lang="ts">
/**
 * The DNS zone context sidebar, shown once a domain is being edited.
 *
 * A zone editor with no persistent answer to "which zone is this" is how a
 * record ends up on the wrong domain — the most expensive mistake this screen
 * can make, and one that is invisible until mail stops arriving. The switcher
 * keeps the answer on screen and makes changing it deliberate.
 *
 * The nameserver card is here rather than in the page body because it is the
 * one fact you need while typing records at the registrar, not something you
 * scroll back to find.
 */
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import ContextSidebar from "./ContextSidebar.vue";
import ContextNavRow from "./ContextNavRow.vue";
import NxSwitcher from "@/components/ui/NxSwitcher.vue";
import { DNS_DOMAINS, DOMAIN_SECTIONS, NAMESERVERS, type DomainSection } from "@/data/dns";

const route = useRoute();
const router = useRouter();

const domain = computed(() => {
  const q = route.query.domain;
  const d = typeof q === "string" ? q : "";
  return (DNS_DOMAINS as readonly string[]).includes(d) ? d : "";
});

const section = computed<DomainSection>(() => (route.query.section === "dns" ? "dns" : "overview"));

const items = computed(() => DNS_DOMAINS.map((d) => ({ key: d, label: d })));

function switchTo(next: string | number) {
  // The section carries over: someone comparing two zones' records does not want
  // to be dropped back on an overview after every switch.
  void router.push({ name: "dns", query: { domain: String(next), section: section.value } });
}

function goSection(key: DomainSection) {
  void router.push({ name: "dns", query: { domain: domain.value, section: key } });
}
</script>

<template>
  <ContextSidebar nav-label="Domain sections" back-to="domains" back-label="All domains">
    <template #top>
      <NxSwitcher
        :items="items"
        :current="domain"
        mono
        placeholder="Search domains"
        empty-text="No domain matches."
        label="Switch domain"
        @pick="switchTo"
      >
        <template #trigger>
          <span class="dom__name nx-mono">{{ domain }}</span>
          <span class="dom__state">Active · SSL valid</span>
        </template>
      </NxSwitcher>
    </template>

    <ContextNavRow
      v-for="s in DOMAIN_SECTIONS"
      :key="s.key"
      :label="s.label"
      :icon="s.icon"
      :current="section === s.key"
      @activate="goSection(s.key)"
    />

    <template #footer>
      <div class="dom__caption">NAMESERVERS</div>
      <p class="dom__ns nx-mono">
        <span v-for="ns in NAMESERVERS" :key="ns">{{ ns }}<br /></span>
      </p>
    </template>
  </ContextSidebar>
</template>

<style scoped>
.dom__name {
  display: block;
  font-size: var(--nx-text-base);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.dom__state { display: block; font-size: var(--nx-text-xs); color: var(--nx-success); padding-top: 4px; }
.dom__caption {
  font-size: var(--nx-text-xs);
  color: var(--nx-text-muted);
  letter-spacing: var(--nx-ls-caps);
}
.dom__ns { margin: 0; font-size: var(--nx-text-sm); color: var(--nx-text-2); padding-top: 6px; }
</style>
