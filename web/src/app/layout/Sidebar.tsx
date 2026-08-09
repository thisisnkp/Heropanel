import { useState } from "react";
import { NavLink, useLocation } from "react-router-dom";
import {
  ChevronRight,
  Container,
  Globe,
  LayoutDashboard,
  LayoutGrid,
  type LucideIcon,
  Mail,
  Network,
  Server,
  Settings,
  Store,
  Users,
} from "lucide-react";
import { Logo } from "@/components/Logo";
import { cn, microLabel } from "@/components/ui";
import { can } from "@/lib/api";
import { useMe } from "@/features/auth/auth";

interface NavLeaf {
  to: string;
  label: string;
  icon?: LucideIcon;
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
  icon: LucideIcon;
  to?: string;
  children: NavLeaf[];
}

type NavItem = NavLeaf | NavGroup;

function isGroup(item: NavItem): item is NavGroup {
  return "children" in item;
}

// Primary navigation, banded into sections. The bands are the point: a flat
// list of eleven destinations is a list to read, whereas three short labelled
// groups are something the eye lands in. Each band is a different *kind* of
// work — what you host, what you run alongside it, who may touch it — so the
// grouping survives new entries being added to any of them.
const sections: { label: string; items: NavItem[] }[] = [
  {
    label: "Platform",
    items: [
      { to: "/", label: "Dashboard", icon: LayoutDashboard },
      {
        label: "Websites",
        to: "/sites",
        icon: Globe,
        children: [
          { to: "/databases", label: "Databases" },
          { to: "/ssl", label: "SSL" },
          { to: "/security", label: "Security", perm: "security.read" },
        ],
      },
      {
        label: "Domains",
        icon: Network,
        children: [
          { to: "/domains", label: "Domain management", perm: "site.read" },
          { to: "/dns", label: "DNS management", perm: "dns.read" },
          { to: "/nameservers", label: "Nameserver management", perm: "dns.read" },
        ],
      },
    ],
  },
  {
    label: "Services",
    items: [
      { to: "/docker", label: "Docker", icon: Container, perm: "docker.read" },
      { to: "/apps", label: "Apps", icon: LayoutGrid, perm: "docker.read" },
      { to: "/mail", label: "Mail", icon: Mail, perm: "mail.read" },
      { to: "/marketplace", label: "Marketplace", icon: Store, perm: "module.read" },
    ],
  },
  {
    label: "Administration",
    items: [{ to: "/users", label: "Users", icon: Users }],
  },
];

// Settings is pinned to the bottom of the sidebar (rendered outside the
// scrolling nav) and gathers the panel-administration screens.
const settingsGroup: NavGroup = {
  label: "Settings",
  icon: Settings,
  children: [
    { to: "/monitor", label: "Monitoring", perm: "monitor.read" },
    { to: "/audit", label: "Audit logs" },
    { to: "/recordings", label: "Recordings", perm: "terminal.recordings.read" },
    { to: "/modules", label: "Modules" },
    { to: "/help", label: "Help" },
  ],
};

// The selected destination is marked three ways at once — a tinted fill, the
// accent colour on the label, and a rail against its leading edge. One alone
// (a pale tint over the whole row) is easy to miss when scanning a long nav.
const activeItem =
  "bg-brand-subtle font-medium text-brand " +
  "before:absolute before:inset-y-1 before:left-0 before:w-0.5 before:rounded-full before:bg-brand before:content-['']";
const idleItem = "text-muted hover:bg-panel-hover hover:text-fg";

const leafClass = ({ isActive }: { isActive: boolean }) =>
  cn(
    "relative flex items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-[13px] transition-colors",
    isActive ? activeItem : idleItem,
  );

function Leaf({ item }: { item: NavLeaf }) {
  const Icon = item.icon;
  return (
    <NavLink to={item.to} end={item.to === "/"} className={leafClass}>
      {Icon && <Icon className="h-[18px] w-[18px] shrink-0" strokeWidth={1.75} aria-hidden />}
      {item.label}
    </NavLink>
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
  const Icon = group.icon;

  return (
    <div>
      {group.to ? (
        // Linkable group: the label navigates, the chevron toggles.
        <div
          className={cn(
            "relative flex items-center rounded-lg text-[13px] transition-colors",
            selfActive || hasActiveChild ? activeItem : idleItem,
          )}
        >
          <NavLink to={group.to} className="flex flex-1 items-center gap-2.5 px-2.5 py-1.5">
            <Icon className="h-[18px] w-[18px] shrink-0" strokeWidth={1.75} aria-hidden />
            <span className="flex-1 text-left">{group.label}</span>
          </NavLink>
          {children.length > 0 && (
            <button
              type="button"
              onClick={() => setOpen((v) => !v)}
              aria-expanded={expanded}
              aria-label={`Toggle ${group.label}`}
              className="rounded-lg px-1.5 py-1.5 opacity-70 transition-opacity hover:opacity-100"
            >
              <ChevronRight
                className={cn("h-3.5 w-3.5 transition-transform", expanded && "rotate-90")}
                strokeWidth={2.25}
                aria-hidden
              />
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
            "flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-[13px] transition-colors",
            hasActiveChild ? "font-medium text-fg" : idleItem,
          )}
        >
          <Icon className="h-[18px] w-[18px] shrink-0" strokeWidth={1.75} aria-hidden />
          <span className="flex-1 text-left">{group.label}</span>
          <ChevronRight
            className={cn("h-3.5 w-3.5 opacity-70 transition-transform", expanded && "rotate-90")}
            strokeWidth={2.25}
            aria-hidden
          />
        </button>
      )}
      {expanded && children.length > 0 && (
        // The rail is indented to sit under the parent's icon, so the children
        // hang off it rather than starting at an unrelated left edge.
        <div className="ml-[1.3125rem] mt-0.5 space-y-0.5 border-l border-border pl-2.5">
          {children.map((c) => (
            <NavLink
              key={c.to}
              to={c.to}
              className={({ isActive }) =>
                cn(
                  "block rounded-md px-2.5 py-1.5 text-[13px] transition-colors",
                  isActive ? "bg-brand-subtle font-medium text-brand" : idleItem,
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

  return (
    <aside className="flex w-[15rem] shrink-0 flex-col border-r border-border bg-panel">
      {/* h-14 and the bottom rule match the top bar exactly, so the header line
          runs unbroken across the sidebar seam instead of stepping at it. */}
      <div className="flex h-14 shrink-0 items-center gap-2.5 border-b border-border px-4">
        <Logo className="h-7 w-7 shrink-0" />
        <div className="min-w-0">
          <div className="truncate text-[15px] font-semibold leading-tight tracking-tight text-fg">NexPanel</div>
          <div className="flex items-center gap-1 text-2xs text-muted">
            <Server className="h-3 w-3" strokeWidth={2} aria-hidden />
            single-node
          </div>
        </div>
      </div>

      <nav aria-label="Primary" className="flex-1 space-y-5 overflow-y-auto px-2.5 py-3">
        {sections.map((section) => {
          const visible = section.items.filter((it) => (isGroup(it) ? true : !it.perm || can(me, it.perm)));
          if (visible.length === 0) return null;
          return (
            <div key={section.label} className="space-y-0.5">
              <div className={cn(microLabel, "px-2.5 pb-1.5")}>{section.label}</div>
              {visible.map((it) =>
                isGroup(it) ? <Group key={it.label} group={it} me={me} /> : <Leaf key={it.to} item={it} />,
              )}
            </div>
          );
        })}
      </nav>

      {/* Settings is pinned to the bottom, always reachable regardless of how
          far the primary nav has scrolled. */}
      <div className="border-t border-border px-2.5 py-2">
        <Group group={settingsGroup} me={me} />
      </div>
    </aside>
  );
}
