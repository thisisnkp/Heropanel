import { Button } from "@/components/ui";
import { useTheme } from "@/stores/theme";
import { useMe, useLogout } from "@/features/auth/auth";
import { activeJobCount, useJobs } from "@/stores/jobs";

function SunMoon({ dark }: { dark: boolean }) {
  return (
    <svg viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round">
      {dark ? (
        <path d="M21 12.8A9 9 0 1111.2 3a7 7 0 009.8 9.8Z" />
      ) : (
        <>
          <circle cx="12" cy="12" r="4" />
          <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
        </>
      )}
    </svg>
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
    <header className="flex h-14 shrink-0 items-center justify-between border-b border-border bg-panel/80 px-4 backdrop-blur">
      <button
        onClick={() => {
          // The palette is keyboard-first, but a click target discovers it.
          window.dispatchEvent(new KeyboardEvent("keydown", { key: "k", metaKey: true }));
        }}
        className="flex items-center gap-2 rounded-lg border border-border bg-surface px-3 py-1.5 text-sm text-muted hover:text-fg"
      >
        <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round">
          <circle cx="11" cy="11" r="7" />
          <path d="M21 21l-4.3-4.3" />
        </svg>
        <span className="hidden sm:inline">Search…</span>
        <kbd className="rounded border border-border bg-panel px-1.5 py-0.5 text-xs">⌘K</kbd>
      </button>
      <div className="flex items-center gap-2">
        <button
          onClick={() => setJobsOpen(true)}
          className="relative grid h-9 w-9 place-items-center rounded-lg text-muted hover:bg-border/40 hover:text-fg"
          aria-label="Activity"
        >
          <svg viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round">
            <path d="M22 12h-4l-3 9L9 3l-3 9H2" />
          </svg>
          {active > 0 && (
            <span className="absolute -right-0.5 -top-0.5 grid h-4 min-w-4 place-items-center rounded-full bg-brand px-1 text-[10px] font-semibold text-brand-fg">
              {active}
            </span>
          )}
        </button>
        <button
          onClick={toggle}
          className="grid h-9 w-9 place-items-center rounded-lg text-muted hover:bg-border/40 hover:text-fg"
          aria-label="Toggle theme"
        >
          <SunMoon dark={theme === "dark"} />
        </button>
        <div className="hidden text-right sm:block">
          <div className="text-sm font-medium text-fg">{me?.display_name ?? me?.username}</div>
          <div className="text-xs text-muted">{me?.email}</div>
        </div>
        <Button variant="ghost" className="h-9 px-3" loading={logout.isPending} onClick={() => logout.mutate()}>
          Sign out
        </Button>
      </div>
    </header>
  );
}
