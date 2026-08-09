import { Outlet, useLocation } from "react-router-dom";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import { Sidebar } from "./Sidebar";
import { Topbar } from "./Topbar";

export function AppLayout() {
  const { pathname } = useLocation();
  return (
    <div className="flex h-screen bg-surface">
      {/* Skip link: the first thing a keyboard user reaches, letting them jump
          past the nav straight to the page. Off-screen until focused. */}
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-[60] focus:rounded-md focus:bg-brand focus:px-3 focus:py-2 focus:text-sm focus:text-brand-fg"
      >
        Skip to content
      </a>
      <Sidebar />
      <div className="flex min-w-0 flex-1 flex-col">
        <Topbar />
        <main id="main-content" tabIndex={-1} className="flex-1 overflow-auto p-6 focus:outline-none">
          {/* Per-route boundary: a page crash is contained here, so the sidebar
              and top bar stay usable. Keyed on the path so navigating to another
              route clears a caught error and renders the new page fresh. */}
          <ErrorBoundary title="This page" resetKeys={[pathname]}>
            <Outlet />
          </ErrorBoundary>
        </main>
      </div>
    </div>
  );
}
