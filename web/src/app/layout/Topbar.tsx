import { Button } from "@/components/ui";
import { useTheme } from "@/stores/theme";
import { useMe, useLogout } from "@/features/auth/auth";

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

  return (
    <header className="flex h-14 shrink-0 items-center justify-between border-b border-border bg-panel/80 px-4 backdrop-blur">
      <div className="text-sm text-muted">
        <kbd className="rounded border border-border bg-surface px-1.5 py-0.5 text-xs">⌘K</kbd>{" "}
        <span className="hidden sm:inline">Search coming soon</span>
      </div>
      <div className="flex items-center gap-2">
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
