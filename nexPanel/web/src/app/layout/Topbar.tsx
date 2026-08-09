import { Link, useLocation } from "react-router-dom";
import { Activity, ChevronRight, Moon, Search, Sun } from "lucide-react";
import { Button, IconButton, cn } from "@/components/ui";
import { useTheme } from "@/stores/theme";
import { useMe, useLogout } from "@/features/auth/auth";
import { activeJobCount, useJobs } from "@/stores/jobs";

// Route segments the breadcrumb should show with a proper name rather than a
// title-cased slug. Anything not listed falls back to the segment itself, which
// is right for ids and uids — those *are* their own label.
const segmentNames: Record<string, string> = {
  sites: "Websites",
  new: "New website",
  dns: "DNS",
  ssl: "SSL",
  domains: "Domains",
  nameservers: "Nameservers",
  databases: "Databases",
  docker: "Docker",
  apps: "Apps",
  mail: "Mail",
  marketplace: "Marketplace",
  users: "Users",
  monitor: "Monitoring",
  audit: "Audit logs",
  recordings: "Recordings",
  modules: "Modules",
  security: "Security",
  account: "Account",
  help: "Help",
};

// Breadcrumbs answer "where am I" on the deep pages — a site's workspace is
// four levels from the dashboard and otherwise gives no clue how to get back
// up. On a top-level page there is nothing to trace, so it renders nothing
// rather than a lone crumb pointing at itself.
function Breadcrumbs() {
  const { pathname } = useLocation();
  const segments = pathname.split("/").filter(Boolean);
  if (segments.length === 0) return <span className="text-sm font-medium text-fg">Dashboard</span>;

  return (
    <nav aria-label="Breadcrumb" className="flex min-w-0 items-center gap-1 text-sm">
      <Link to="/" className="shrink-0 text-muted transition-colors hover:text-fg">
        Home
      </Link>
      {segments.map((seg, i) => {
        const href = "/" + segments.slice(0, i + 1).join("/");
        const last = i === segments.length - 1;
        const label = segmentNames[seg] ?? seg;
        return (
          <span key={href} className="flex min-w-0 items-center gap-1">
            <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted/70" strokeWidth={2} aria-hidden />
            {last ? (
              <span aria-current="page" className="truncate font-medium text-fg">
                {label}
              </span>
            ) : (
              <Link to={href} className="truncate text-muted transition-colors hover:text-fg">
                {label}
              </Link>
            )}
          </span>
        );
      })}
    </nav>
  );
}

export function Topbar() {
  const { theme, toggle } = useTheme();
  const { data: me } = useMe();
  const logout = useLogout();
  const jobs = useJobs((s) => s.jobs);
  const setJobsOpen = useJobs((s) => s.setOpen);
  const active = activeJobCount(jobs);

  return (
    <header className="flex h-14 shrink-0 items-center gap-4 border-b border-border bg-panel/85 px-4 backdrop-blur">
      <div className="min-w-0 flex-1">
        <Breadcrumbs />
      </div>

      <button
        onClick={() => {
          // The palette is keyboard-first, but a click target discovers it.
          window.dispatchEvent(new KeyboardEvent("keydown", { key: "k", metaKey: true }));
        }}
        className={cn(
          "hidden items-center gap-2 rounded-lg border border-border-strong bg-surface py-1.5 pl-2.5 pr-2 text-sm text-muted",
          "transition-colors hover:border-muted/50 hover:text-fg md:flex",
          "focus:outline-none focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2",
        )}
      >
        <Search className="h-4 w-4 shrink-0" strokeWidth={2} aria-hidden />
        <span className="pr-8">Search…</span>
        <kbd className="rounded border border-border bg-panel px-1.5 py-0.5 font-sans text-2xs font-medium text-muted">
          ⌘K
        </kbd>
      </button>

      <div className="flex shrink-0 items-center gap-1">
        <IconButton label="Activity" onClick={() => setJobsOpen(true)} className="relative">
          <Activity className="h-[18px] w-[18px]" strokeWidth={1.75} aria-hidden />
          {active > 0 && (
            <span className="absolute -right-0.5 -top-0.5 grid h-4 min-w-4 place-items-center rounded-full bg-brand px-1 text-2xs font-semibold text-brand-fg">
              {active}
            </span>
          )}
        </IconButton>

        <IconButton label="Toggle theme" onClick={toggle}>
          {theme === "dark" ? (
            <Moon className="h-[18px] w-[18px]" strokeWidth={1.75} aria-hidden />
          ) : (
            <Sun className="h-[18px] w-[18px]" strokeWidth={1.75} aria-hidden />
          )}
        </IconButton>

        <span className="mx-1 hidden h-6 w-px bg-border sm:block" />

        <Link
          to="/account"
          className="hidden max-w-[12rem] rounded-lg px-2.5 py-1 text-right leading-tight transition-colors hover:bg-panel-hover sm:block"
          title="Account & sessions"
        >
          <div className="truncate text-[13px] font-medium text-fg">{me?.display_name ?? me?.username}</div>
          <div className="truncate text-2xs text-muted">{me?.email}</div>
        </Link>

        <Button variant="ghost" size="sm" loading={logout.isPending} onClick={() => logout.mutate()}>
          Sign out
        </Button>
      </div>
    </header>
  );
}
