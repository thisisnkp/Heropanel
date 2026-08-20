import { onScopeDispose, readonly, ref } from "vue";

/** Where the design switches from the sidebar shell to the tab-bar shell. */
export const MOBILE_MAX = 900;

/**
 * Viewport size as reactive state.
 *
 * Driven by matchMedia rather than a resize listener: the browser only fires it
 * when the answer actually changes, so dragging a window edge does not re-render
 * the whole shell on every frame.
 */
export function useBreakpoint() {
  const query = window.matchMedia(`(max-width: ${MOBILE_MAX}px)`);
  const isMobile = ref(query.matches);

  const onChange = (e: MediaQueryListEvent) => {
    isMobile.value = e.matches;
  };
  query.addEventListener("change", onChange);
  onScopeDispose(() => query.removeEventListener("change", onChange));

  return { isMobile: readonly(isMobile) };
}
