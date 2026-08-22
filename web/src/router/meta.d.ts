import "vue-router";

declare module "vue-router" {
  interface RouteMeta {
    /** Browser tab title, suffixed with the product name. */
    title?: string;
    /**
     * Render with no panel chrome. Used by the pre-session screens (which have
     * nothing to navigate to) and by the file manager (which is its own window
     * and brings its own sidebar).
     */
    standalone?: boolean;
    /** Reachable without a session. Only the pre-session screens set this. */
    public?: boolean;
  }
}
