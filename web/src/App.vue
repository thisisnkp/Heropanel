<script setup lang="ts">
// The shell is chosen by viewport, not by route: every route renders inside
// whichever chrome fits the screen. Two shells rather than one responsive one
// because the design treats them as different products — the desktop has a
// persistent sidebar and a slide-over site drawer, the mobile has a bottom tab
// bar and full-screen pushes. Trying to express both in one component is what
// produces chrome that is half of each.
//
// A third case sits outside both: a route marked `standalone` renders with no
// panel chrome at all. The file manager is the one screen the design ships as a
// separate window, opened from the panel and kept open beside it; it brings its
// own sidebar, so wrapping it in the panel's would give it two.
import { computed, onMounted } from "vue";
import { useRoute } from "vue-router";
import { useBreakpoint } from "@/composables/useBreakpoint";
import { useSitesStore } from "@/stores/sites";
import DesktopShell from "@/layouts/DesktopShell.vue";
import MobileShell from "@/layouts/MobileShell.vue";
import ToastStack from "@/components/ui/ToastStack.vue";

const { isMobile } = useBreakpoint();
const route = useRoute();

const standalone = computed(() => route.matched.some((r) => r.meta.standalone));

// The site list is shell-level data — the sidebar count, the site switcher and
// the drawer all read it — so it is loaded once here rather than by whichever
// screen happens to mount first. A standalone window needs it too: the file
// manager names the site it is browsing.
const sites = useSitesStore();
onMounted(() => void sites.ensureLoaded());
</script>

<template>
  <a class="nx-skip" href="#nx-main">Skip to content</a>

  <!-- No shell, but still the toasts: the file manager confirms every
       destructive action through one, and offers the undo there. -->
  <template v-if="standalone">
    <RouterView />
    <ToastStack />
  </template>

  <MobileShell v-else-if="isMobile" />
  <DesktopShell v-else />
</template>
