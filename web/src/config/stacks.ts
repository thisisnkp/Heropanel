/** The runtimes a site can be created on, with the badge styling the design gives each. */
export type StackKey = "static" | "php" | "node" | "python" | "wp";

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
};

export const STACK_KEYS = Object.keys(STACKS) as StackKey[];

/** The label the "language version" screen carries, per stack. Static has none. */
export const LANG_VERSION_LABEL: Partial<Record<StackKey, string>> = {
  php: "PHP version",
  wp: "PHP version",
  node: "Node.js version",
  python: "Python version",
};
