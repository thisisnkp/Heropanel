<script setup lang="ts">
/**
 * The desktop chrome.
 *
 * Which left column shows is decided by the route, not by a store flag: inside a
 * site the design swaps the wide sidebar for the icon rail plus the site drawer,
 * so a deep link straight into a site's cron jobs arrives with the right chrome
 * already rendered instead of flashing the wrong one.
 */
import { computed } from "vue";
import { useRoute } from "vue-router";
import AppSidebar from "@/components/nav/AppSidebar.vue";
import AppRail from "@/components/nav/AppRail.vue";
import AppTopbar from "@/components/nav/AppTopbar.vue";
import SiteDrawer from "@/components/nav/SiteDrawer.vue";
import ToastStack from "@/components/ui/ToastStack.vue";

const route = useRoute();
const inSite = computed(() => String(route.name ?? "").startsWith("site"));
const fullBleed = computed(() => route.matched.some((r) => r.meta.fullBleed));
</script>

<template>
  <div class="nx-shell">
    <AppRail v-if="inSite" />
    <SiteDrawer v-if="inSite" />
    <AppSidebar v-else />

    <main class="nx-shell__main nxscroll">
      <AppTopbar />
      <div id="nx-main" :class="['nx-shell__content', { 'is-full-bleed': fullBleed }]">
        <RouterView v-slot="{ Component }">
          <component :is="Component" class="nx-view" />
        </RouterView>
      </div>
    </main>

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
.nx-shell__content.is-full-bleed { padding: 0; max-width: none; }
</style>

<style>
/* Not scoped: the view is a child component, so the animation has to reach it. */
.nx-view { animation: nxView 260ms cubic-bezier(0.16, 1, 0.3, 1) both; }
</style>
