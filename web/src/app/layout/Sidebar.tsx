import { useState } from "react";
import { NavLink, useLocation } from "react-router-dom";
import { Logo } from "@/components/Logo";
import { cn } from "@/components/ui";
import { can } from "@/lib/api";
import { useMe } from "@/features/auth/auth";

interface NavLeaf {
  to: string;
  label: string;
  icon: string;
  /**
   * Hide the item without this permission. Only set where the permission is a
   * narrow one: the other entries lead to pages that explain a 403, which is
   * friendlier than a menu that silently differs between operators. Recordings
   * are different — reading other people's session transcripts is granted to
   * few, so an item that 403s for nearly everyone would be noise.
   */
  perm?: string;
}

// A NavGroup is a collapsible parent with child links (e.g. Websites → its
// related tools, Domain → domain / DNS / nameserver management). The group is
// shown only when at least one child is visible to the caller (or it has its own
// `to`), and it opens automatically when the group or one of its children is the
// active route.
//
// `to` is optional: when set, the group header is itself a link (clicking the
// label navigates there — e.g. Websites opens the site list) and a separate
// chevron toggles the children. Without `to` the whole header is the toggle.
interface NavGroup {
  label: string;
  icon: string;
  to?: string;
  children: NavLeaf[];
}

type NavItem = NavLeaf | NavGroup;

function isGroup(item: NavItem): item is NavGroup {
  return "children" in item;
}

// Primary navigation. The shape follows the layout the operator asked for: a
// small set of top-level destinations, with the rest folded into the group they
// belong to (site tooling under Websites; DNS under Domains) and the
// panel-administration screens under a pinned Settings group at the bottom.
const items: NavItem[] = [
  { to: "/", label: "Dashboard", icon: "M3 12l9-9 9 9M5 10v10h14V10" },
  {
    label: "Websites",
    to: "/sites",
    icon: "M4 5h16v11H4zM4 9h16M8 20h8M12 16v4",
    children: [
      { to: "/databases", label: "Databases", icon: "" },
      { to: "/ssl", label: "SSL", icon: "" },
      { to: "/security", label: "Security", icon: "", perm: "security.read" },
    ],
  },
  {
    label: "Domains",
    icon: "M12 2a10 10 0 100 20 10 10 0 000-20M2 12h20M12 2a15 15 0 010 20M12 2a15 15 0 000 20",
    children: [
      { to: "/domains", label: "Domain management", icon: "", perm: "site.read" },
      { to: "/dns", label: "DNS management", icon: "", perm: "dns.read" },
      { to: "/nameservers", label: "Nameserver management", icon: "", perm: "dns.read" },
    ],
  },
  {
    to: "/docker",
    label: "Docker",
    icon: "M4 12h16v5a3 3 0 01-3 3H7a3 3 0 01-3-3zM7 12V9h3v3M12 12V9h3v3M12 9V6h3v3",
    perm: "docker.read",
  },
  {
    to: "/apps",
    label: "Apps",
    icon: "M12 2l3 6 6 1-4.5 4 1 6-5.5-3-5.5 3 1-6L3 9l6-1z",
    perm: "docker.read",
  },
  {
    to: "/mail",
    label: "Mail",
    icon: "M3 6h18v12H3zM3 7l9 6 9-6",
    perm: "mail.read",
  },
  {
    to: "/marketplace",
    label: "Marketplace",
    icon: "M3 9l1.5-5h15L21 9M3 9h18M3 9v10a1 1 0 001 1h16a1 1 0 001-1V9M8 13h8",
    perm: "module.read",
  },
  { to: "/users", label: "Users", icon: "M16 14a4 4 0 10-8 0M12 7a3 3 0 100 6 3 3 0 000-6M4 20a8 8 0 0116 0" },
];

// Settings is pinned to the bottom of the sidebar (rendered outside the
// scrolling nav) and gathers the panel-administration screens.
const settingsGroup: NavGroup = {
  label: "Settings",
  icon:
    "M12 9a3 3 0 100 6 3 3 0 000-6M19.4 13a7.9 7.9 0 000-2l2-1.5-2-3.5-2.4 1a8 8 0 00-1.7-1L15 3H9l-.3 2.5a8 8 0 00-1.7 1l-2.4-1-2 3.5 2 1.5a7.9 7.9 0 000 2l-2 1.5 2 3.5 2.4-1a8 8 0 001.7 1L9 21h6l.3-2.5a8 8 0 001.7-1l2.4 1 2-3.5z",
  children: [
    { to: "/monitor", label: "Monitoring", icon: "", perm: "monitor.read" },
    { to: "/audit", label: "Audit logs", icon: "" },
    { to: "/recordings", label: "Recordings", icon: "", perm: "terminal.recordings.read" },
    { to: "/modules", label: "Modules", icon: "" },
    { to: "/help", label: "Help", icon: "" },
  ],
};

function Icon({ path }: { path: string }) {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round">
      <path d={path} />
    </svg>
  );
}

const leafClass = ({ isActive }: { isActive: boolean }) =>
  cn(
    "flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors",
    isActive ? "bg-brand/15 text-fg" : "text-muted hover:bg-border/40 hover:text-fg",
  );

function Leaf({ item }: { item: NavLeaf }) {
  return (
    <NavLink to={item.to} end={item.to === "/"} className={leafClass}>
      <Icon path={item.icon} />
      {item.label}
    </NavLink>
  );
}

function Chevron({ expanded }: { expanded: boolean }) {
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      className={cn("h-4 w-4 transition-transform", expanded ? "rotate-90" : "")}
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M9 6l6 6-6 6" />
    </svg>
  );
}

function Group({ group, me }: { group: NavGroup; me: ReturnType<typeof useMe>["data"] }) {
  const { pathname } = useLocation();
  const children = group.children.filter((c) => !c.perm || can(me, c.perm));
  const selfActive = group.to != null && (pathname === group.to || pathname.startsWith(group.to + "/"));
  const hasActiveChild = children.some((c) => pathname === c.to || pathname.startsWith(c.to + "/"));
  const [open, setOpen] = useState(hasActiveChild || selfActive);
  // A group with no visible children and no destination of its own has nothing
  // to show.
  if (children.length === 0 && !group.to) return null;
  const expanded = open || hasActiveChild;

  return (
    <div>
      {group.to ? (
        // Linkable group: the label navigates, the chevron toggles.
        <div
          className={cn(
            "flex items-center rounded-lg text-sm transition-colors",
            selfActive || hasActiveChild ? "bg-brand/15 text-fg" : "text-muted hover:bg-border/40 hover:text-fg",
          )}
        >
          <NavLink to={group.to} className="flex flex-1 items-center gap-3 px-3 py-2">
            <Icon path={group.icon} />
            <span className="flex-1 text-left">{group.label}</span>
          </NavLink>
          {children.length > 0 && (
            <button
              type="button"
              onClick={() => setOpen((v) => !v)}
              aria-expanded={expanded}
              aria-label={`Toggle ${group.label}`}
              className="rounded-lg px-2 py-2 hover:text-fg"
            >
              <Chevron expanded={expanded} />
            </button>
          )}
        </div>
      ) : (
        // Pure group: the whole header toggles.
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={expanded}
          className={cn(
            "flex w-full items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors",
            hasActiveChild ? "text-fg" : "text-muted hover:bg-border/40 hover:text-fg",
          )}
        >
          <Icon path={group.icon} />
          <span className="flex-1 text-left">{group.label}</span>
          <Chevron expanded={expanded} />
        </button>
      )}
      {expanded && children.length > 0 && (
        <div className="mt-1 space-y-1 border-l border-border/60 pl-3">
          {children.map((c) => (
            <NavLink
              key={c.to}
              to={c.to}
              className={({ isActive }) =>
                cn(
                  "block rounded-lg px-3 py-1.5 text-sm transition-colors",
                  isActive ? "bg-brand/15 text-fg" : "text-muted hover:bg-border/40 hover:text-fg",
                )
              }
            >
              {c.label}
            </NavLink>
          ))}
        </div>
      )}
    </div>
  );
}

export function Sidebar() {
  const { data: me } = useMe();
  const visible = items.filter((it) => (isGroup(it) ? true : !it.perm || can(me, it.perm)));
  return (
    <aside className="flex w-60 shrink-0 flex-col border-r border-border bg-panel">
      <div className="flex h-14 items-center gap-2 px-4">
        <Logo className="h-7 w-7" />
        <span className="text-sm font-semibold tracking-tight text-fg">HeroPanel</span>
      </div>
      <nav aria-label="Primary" className="flex-1 space-y-1 overflow-y-auto px-3 py-2">
        {visible.map((it) =>
          isGroup(it) ? <Group key={it.label} group={it} me={me} /> : <Leaf key={it.to} item={it} />,
        )}
      </nav>
      {/* Settings is pinned to the bottom, always reachable regardless of how
          far the primary nav has scrolled. */}
      <div className="border-t border-border px-3 py-2">
        <Group group={settingsGroup} me={me} />
      </div>
      <div className="border-t border-border px-4 py-2 text-xs text-muted">v0 · single-node</div>
    </aside>
  );
}
