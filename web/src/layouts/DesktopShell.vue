<script setup lang="ts">
/**
 * The desktop chrome.
 *
 * Which left column shows is decided by the route, not by a store flag: a deep
 * link straight into a site's cron jobs arrives with the right chrome already
 * rendered instead of flashing the wrong one.
 *
 * There are five columns in the design, not two. A website, a DNS zone, Security
 * and Apps each replace the global sidebar with their own — because each of them
 * is a place you stay inside for several screens, and a global menu offers no
 * way to move between those screens. The icon rail comes along in every one of
 * those contexts so the rest of the panel is still one click away.
 */
import { computed } from "vue";
import { useRoute } from "vue-router";
import AppSidebar from "@/components/nav/AppSidebar.vue";
import AppRail from "@/components/nav/AppRail.vue";
import AppTopbar from "@/components/nav/AppTopbar.vue";
import SiteDrawer from "@/components/nav/SiteDrawer.vue";
import SecuritySidebar from "@/components/nav/SecuritySidebar.vue";
import AppsSidebar from "@/components/nav/AppsSidebar.vue";
import DomainSidebar from "@/components/nav/DomainSidebar.vue";
import AiPanel from "@/components/ai/AiPanel.vue";
import JobTray from "@/components/ui/JobTray.vue";
import SearchPalette from "@/components/ui/SearchPalette.vue";
import ToastStack from "@/components/ui/ToastStack.vue";
import { DNS_DOMAINS } from "@/data/dns";

const route = useRoute();

type Context = "site" | "security" | "apps" | "dns" | null;

const context = computed<Context>(() => {
  const name = String(route.name ?? "");
  if (name.startsWith("site")) return "site";
  if (name.startsWith("security")) return "security";
  if (name.startsWith("apps")) return "apps";
  // The zone editor only becomes a context once a zone is chosen; before that
  // the screen is a picker, and a sidebar naming a domain would be lying.
  const d = route.query.domain;
  if (name === "dns" && typeof d === "string" && (DNS_DOMAINS as readonly string[]).includes(d)) return "dns";
  return null;
});

</script>

<template>
  <div class="nx-shell">
    <AppRail v-if="context" />
    <SiteDrawer v-if="context === 'site'" />
    <SecuritySidebar v-else-if="context === 'security'" />
    <AppsSidebar v-else-if="context === 'apps'" />
    <DomainSidebar v-else-if="context === 'dns'" />
    <AppSidebar v-else />

    <main class="nx-shell__main nxscroll">
      <AppTopbar />
      <div id="nx-main" class="nx-shell__content">
        <RouterView v-slot="{ Component }">
          <component :is="Component" class="nx-view" />
        </RouterView>
      </div>
    </main>

    <SearchPalette />
    <AiPanel />
    <JobTray />
    <ToastStack />
  </div>
</template>

<style scoped>
.nx-shell {
  display: flex;
  height: 100vh;
  min-height: 0;
  background: var(--nx-bg);
}
.nx-shell__main { flex: 1; min-width: 0; overflow-y: auto; }
.nx-shell__content { padding: 32px 32px 72px; max-width: 1180px; }
</style>

<style>
/* Not scoped: the view is a child component, so the animation has to reach it. */
.nx-view { animation: nxView 260ms cubic-bezier(0.16, 1, 0.3, 1) both; }
</style>
