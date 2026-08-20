<script setup lang="ts">
/**
 * The mobile chrome: a compact header, the view, and a bottom tab bar.
 *
 * The tab bar carries four destinations plus "More" rather than trying to fit
 * fourteen. Everything the rail offers is still reachable — through the More
 * sheet — but the four here are the ones the design puts a thumb on.
 */
import { computed } from "vue";
import { useRoute } from "vue-router";
import MobileTabBar from "@/components/nav/MobileTabBar.vue";
import MobileNavSheet from "@/components/nav/MobileNavSheet.vue";
import MobileHeader from "@/components/nav/MobileHeader.vue";
import ToastStack from "@/components/ui/ToastStack.vue";

const route = useRoute();
const fullBleed = computed(() => route.matched.some((r) => r.meta.fullBleed));
</script>

<template>
  <div class="nx-mobile">
    <MobileHeader />

    <main id="nx-main" :class="['nx-mobile__content', { 'is-full-bleed': fullBleed }]">
      <RouterView v-slot="{ Component }">
        <component :is="Component" class="nx-view" />
      </RouterView>
    </main>

    <MobileTabBar />
    <MobileNavSheet />
    <ToastStack />
  </div>
</template>

<style scoped>
.nx-mobile {
  display: flex;
  flex-direction: column;
  min-height: 100dvh;
  background: var(--nx-bg);
}
.nx-mobile__content {
  flex: 1;
  min-height: 0;
  /* Bottom padding clears the tab bar plus the home indicator on iOS. */
  padding: 16px 16px calc(76px + env(safe-area-inset-bottom));
}
.nx-mobile__content.is-full-bleed { padding: 0 0 calc(64px + env(safe-area-inset-bottom)); }
</style>
