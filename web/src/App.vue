<script setup lang="ts">
// The shell is chosen by viewport, not by route: every route renders inside
// whichever chrome fits the screen. Two shells rather than one responsive one
// because the design treats them as different products — the desktop has a
// persistent sidebar and a slide-over site drawer, the mobile has a bottom tab
// bar and full-screen pushes. Trying to express both in one component is what
// produces chrome that is half of each.
import { onMounted } from "vue";
import { useBreakpoint } from "@/composables/useBreakpoint";
import { useSitesStore } from "@/stores/sites";
import DesktopShell from "@/layouts/DesktopShell.vue";
import MobileShell from "@/layouts/MobileShell.vue";

const { isMobile } = useBreakpoint();

// The site list is shell-level data — the sidebar count, the site switcher and
// the drawer all read it — so it is loaded once here rather than by whichever
// screen happens to mount first.
const sites = useSitesStore();
onMounted(() => void sites.ensureLoaded());
</script>

<template>
  <a class="nx-skip" href="#nx-main">Skip to content</a>
  <MobileShell v-if="isMobile" />
  <DesktopShell v-else />
</template>
