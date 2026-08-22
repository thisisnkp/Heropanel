/**
 * What a site runs, with the badge styling the design gives each.
 *
 * This mirrors npd's `stack` field on a site rather than being derived here.
 * The server knows which runtime a reverse-proxy site was configured with; a
 * client only sees `type: "proxy"`, which is the same answer for Node, Python
 * and a Docker-backed app.
 *
 * "app" is not creatable — those sites appear when a one-click Docker app is
 * given a domain — so it is in STACKS (it has to render) but not in
 * CREATABLE_STACKS (the wizard must not offer it).
 */
export type StackKey = "static" | "php" | "node" | "python" | "wp" | "app";

export interface StackMeta {
  readonly label: string;
  /** Two-or-three letter badge shown on site cards and rows. */
  readonly tag: string;
  readonly bg: string;
  readonly fg: string;
  /** One line of "what is this for", shown in the create-site picker. */
  readonly hint: string;
}

export const STACKS: Readonly<Record<StackKey, StackMeta>> = {
  static: { label: "Static Site", tag: "ST", bg: "var(--nx-stack-static-soft)", fg: "var(--nx-stack-static)", hint: "HTML, CSS, JS — nothing to run" },
  php: { label: "PHP", tag: "PHP", bg: "var(--nx-stack-php-soft)", fg: "var(--nx-stack-php)", hint: "Laravel, custom PHP, legacy apps" },
  node: { label: "Node.js", tag: "JS", bg: "var(--nx-stack-node-soft)", fg: "var(--nx-stack-node)", hint: "Express, Next.js, APIs, bots" },
  python: { label: "Python", tag: "PY", bg: "var(--nx-stack-python-soft)", fg: "var(--nx-stack-python)", hint: "Django, Flask, FastAPI" },
  wp: { label: "WordPress", tag: "WP", bg: "var(--nx-stack-wp-soft)", fg: "var(--nx-stack-wp)", hint: "One-click install with SSL + cache" },
  app: { label: "Docker app", tag: "APP", bg: "var(--nx-stack-static-soft)", fg: "var(--nx-stack-static)", hint: "A one-click app fronted by a domain" },
};

/** Every stack a site can report — used by filters, badges and lookups. */
export const STACK_KEYS = Object.keys(STACKS) as StackKey[];

/** The stacks the create-site wizard offers. An app site is not made this way. */
export const CREATABLE_STACKS: readonly StackKey[] = ["static", "php", "node", "python", "wp"];

/** The label the "language version" screen carries, per stack. Static has none. */
export const LANG_VERSION_LABEL: Partial<Record<StackKey, string>> = {
  php: "PHP version",
  wp: "PHP version",
  node: "Node.js version",
  python: "Python version",
};
