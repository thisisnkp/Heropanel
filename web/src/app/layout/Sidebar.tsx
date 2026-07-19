import { NavLink } from "react-router-dom";
import { Logo } from "@/components/Logo";
import { cn } from "@/components/ui";

interface NavItem {
  to: string;
  label: string;
  icon: string;
}

const items: NavItem[] = [
  { to: "/", label: "Dashboard", icon: "M3 12l9-9 9 9M5 10v10h14V10" },
  { to: "/sites", label: "Websites", icon: "M2 12h20M12 2a15 15 0 010 20M12 2a15 15 0 000 20M2 12a10 10 0 0120 0" },
  { to: "/databases", label: "Databases", icon: "M4 6c0-1.7 3.6-3 8-3s8 1.3 8 3-3.6 3-8 3-8-1.3-8-3zM4 6v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3" },
  { to: "/dns", label: "DNS", icon: "M2 12h20M12 2a15 15 0 010 20M12 2a15 15 0 000 20M12 2a10 10 0 010 20" },
  { to: "/ssl", label: "SSL", icon: "M12 2l7 4v6c0 5-3.5 8-7 10-3.5-2-7-5-7-10V6zM9 12l2 2 4-4" },
  { to: "/audit", label: "Audit log", icon: "M4 4h16v16H4zM8 9h8M8 13h8M8 17h5" },
  { to: "/modules", label: "Modules", icon: "M4 4h7v7H4zM13 4h7v7h-7zM13 13h7v7h-7zM4 13h7v7H4z" },
  { to: "/users", label: "Users", icon: "M16 14a4 4 0 10-8 0M12 7a3 3 0 100 6 3 3 0 000-6M4 20a8 8 0 0116 0" },
];

function Icon({ path }: { path: string }) {
  return (
    <svg viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round">
      <path d={path} />
    </svg>
  );
}

export function Sidebar() {
  return (
    <aside className="flex w-60 shrink-0 flex-col border-r border-border bg-panel">
      <div className="flex h-14 items-center gap-2 px-4">
        <Logo className="h-7 w-7" />
        <span className="text-sm font-semibold tracking-tight text-fg">HeroPanel</span>
      </div>
      <nav className="flex-1 space-y-1 px-3 py-2">
        {items.map((it) => (
          <NavLink
            key={it.to}
            to={it.to}
            end={it.to === "/"}
            className={({ isActive }) =>
              cn(
                "flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors",
                isActive ? "bg-brand/15 text-fg" : "text-muted hover:bg-border/40 hover:text-fg",
              )
            }
          >
            <Icon path={it.icon} />
            {it.label}
          </NavLink>
        ))}
      </nav>
      <div className="border-t border-border px-4 py-3 text-xs text-muted">v0 · single-node</div>
    </aside>
  );
}
