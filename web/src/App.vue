<script setup lang="ts">
// The shell is chosen by viewport, not by route: every route renders inside
// whichever chrome fits the screen. Two shells rather than one responsive one
// because the design treats them as different products — the desktop has a
// persistent sidebar and a slide-over site drawer, the mobile has a bottom tab
// bar and full-screen pushes. Trying to express both in one component is what
// produces chrome that is half of each.
//
// A third case sits outside both: a route marked `standalone` renders with no
// panel chrome at all. That covers the pre-session screens (there is nothing to
// navigate to yet) and the file manager, which the design ships as a separate
// window, opened from the panel and kept open beside it; it brings its own
// sidebar, so wrapping it in the panel's would give it two.
//
// And one case sits outside even that: npd unreachable. It is not "signed out"
// and not an empty panel, so it gets said plainly rather than being dressed up
// as either.
import { computed, watch } from "vue";
import { useRoute } from "vue-router";
import { useBreakpoint } from "@/composables/useBreakpoint";
import { useSessionStore } from "@/stores/session";
import { useSitesStore } from "@/stores/sites";
import DesktopShell from "@/layouts/DesktopShell.vue";
import MobileShell from "@/layouts/MobileShell.vue";
import ToastStack from "@/components/ui/ToastStack.vue";

const { isMobile } = useBreakpoint();
const route = useRoute();
const session = useSessionStore();
const sites = useSitesStore();

const standalone = computed(() => route.matched.some((r) => r.meta.standalone));

// The site list is shell-level data — the sidebar count, the site switcher and
// the drawer all read it — so it is loaded once here rather than by whichever
// screen happens to mount first.
//
// It waits for a session because /sites is authenticated: firing it during the
// login screen would spend a round trip to be told 401, and the store would
// then be holding an error the operator can do nothing about.
watch(
  () => session.isAuthenticated,
  (yes) => {
    if (yes) void sites.ensureLoaded();
  },
  { immediate: true },
);
</script>

<template>
  <a class="nx-skip" href="#nx-main">Skip to content</a>

  <!-- Nothing at all until /auth/status answers. A shell rendered first would
       paint a signed-in panel for the instant before the guard redirects. -->
  <template v-if="session.loading" />

  <main v-else-if="session.unreachable" id="nx-main" class="nx-down">
    <div class="nx-down__box" role="alert">
      <h1 class="nx-down__title">Cannot reach the panel</h1>
      <p class="nx-down__text">{{ session.unreachable }}</p>
      <button type="button" class="nx-down__retry" @click="session.load()">Try again</button>
    </div>
  </main>

  <!-- No shell, but still the toasts: the file manager confirms every
       destructive action through one, and offers the undo there. -->
  <template v-else-if="standalone">
    <RouterView />
    <ToastStack />
  </template>

  <MobileShell v-else-if="isMobile" />
  <DesktopShell v-else />
</template>

<style scoped>
.nx-down {
  min-height: 100dvh;
  display: grid;
  place-items: center;
  padding: 24px;
  background: var(--nx-bg);
}
.nx-down__box {
  max-width: 460px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  align-items: flex-start;
  padding: 24px;
  background: var(--nx-surface);
  border: 1px solid var(--nx-danger-border);
  border-radius: var(--nx-radius-lg);
}
.nx-down__title { margin: 0; font-size: var(--nx-text-lg); font-weight: 600; color: var(--nx-text); }
.nx-down__text { margin: 0; font-size: var(--nx-text-base); color: var(--nx-text-2); line-height: 1.55; }
.nx-down__retry {
  font: inherit;
  font-size: var(--nx-text-base);
  padding: 8px 14px;
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-md);
  background: var(--nx-surface);
  color: var(--nx-text);
  cursor: pointer;
}
.nx-down__retry:hover { background: var(--nx-hover); }
</style>
