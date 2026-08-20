/**
 * The navigation model, in one place.
 *
 * Both shells read this: the desktop sidebar renders the groups, the mobile tab
 * bar and rail render a flattened subset. Keeping it as data rather than as
 * markup in two components is what stops the two from drifting — a screen added
 * here appears in both, or in neither.
 *
 * `icon` is a Material Symbols name; components resolve it through
 * `<component :is="...">` against the unplugin-icons collection.
 */

export interface NavEntry {
  /** Route name. */
  readonly to: string;
  readonly label: string;
  /** Material Symbols glyph name, kebab-cased for Iconify. */
  readonly icon: string;
  /** Nested entries, rendered under a disclosure caret. */
  readonly children?: readonly NavEntry[];
}

export interface NavGroup {
  readonly id: string;
  /** Section caption; omitted for the first group, which needs no heading. */
  readonly label?: string;
  readonly entries: readonly NavEntry[];
}

export const NAV_GROUPS: readonly NavGroup[] = [
  {
    id: "manage",
    label: "Manage",
    entries: [
      { to: "home", label: "Home", icon: "home" },
      { to: "websites", label: "Websites", icon: "language" },
      { to: "mail", label: "Mail", icon: "mail" },
      {
        to: "domains",
        label: "Domains",
        icon: "dns",
        children: [
          { to: "domains", label: "All domains", icon: "list" },
          { to: "dns", label: "DNS & nameservers", icon: "travel-explore" },
        ],
      },
      { to: "backups", label: "Backups", icon: "backup" },
      { to: "security", label: "Security", icon: "shield" },
    ],
  },
  {
    id: "automation",
    label: "Automation",
    entries: [
      { to: "openclaw", label: "OpenClaw", icon: "smart-toy" },
      { to: "n8n", label: "n8n", icon: "account-tree" },
    ],
  },
  {
    id: "system",
    label: "System",
    entries: [
      { to: "apps", label: "Apps", icon: "apps" },
      {
        to: "docker",
        label: "Advanced",
        icon: "tune",
        children: [
          { to: "docker", label: "Containers", icon: "deployed-code" },
          { to: "compose", label: "Compose", icon: "stacks" },
        ],
      },
      { to: "settings", label: "Panel settings", icon: "settings" },
    ],
  },
  {
    id: "account",
    label: "Account",
    entries: [
      { to: "billing", label: "License & billing", icon: "card-membership" },
      { to: "api", label: "API", icon: "api" },
      { to: "temp-access", label: "Temp access", icon: "schedule-send" },
    ],
  },
];

/**
 * The compact icon rail / mobile tab source.
 *
 * A flat list rather than a flatten() of the groups above: the rail is ordered
 * by how often a thing is reached, not by which section it files under, and the
 * labels are shortened to fit.
 */
export const RAIL_ENTRIES: readonly NavEntry[] = [
  { to: "home", label: "Home", icon: "home" },
  { to: "websites", label: "Sites", icon: "language" },
  { to: "mail", label: "Mail", icon: "mail" },
  { to: "domains", label: "Domains", icon: "dns" },
  { to: "dns", label: "DNS", icon: "travel-explore" },
  { to: "backups", label: "Backups", icon: "backup" },
  { to: "security", label: "Security", icon: "shield" },
  { to: "openclaw", label: "OpenClaw", icon: "smart-toy" },
  { to: "n8n", label: "n8n", icon: "account-tree" },
  { to: "billing", label: "License", icon: "card-membership" },
  { to: "apps", label: "Apps", icon: "apps" },
  { to: "api", label: "API", icon: "api" },
  { to: "temp-access", label: "Temp", icon: "schedule-send" },
  { to: "settings", label: "Settings", icon: "settings" },
];

/**
 * The mobile tab bar, exactly as the design draws it: four destinations plus a
 * "More" sheet. Note this is not the first four of RAIL_ENTRIES — the design
 * promotes Activity on mobile, where checking what just happened is the common
 * errand, and demotes Mail, which is a desktop job.
 */
export const MOBILE_TABS: readonly NavEntry[] = [
  { to: "home", label: "Home", icon: "home" },
  { to: "websites", label: "Websites", icon: "language" },
  { to: "security", label: "Security", icon: "shield" },
  { to: "activity", label: "Activity", icon: "history" },
];
