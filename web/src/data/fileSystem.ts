/**
 * The file-manager fixture tree, from panel_ui_ref/NexPanel File Manager.dc.html.
 *
 * Keyed by path so a listing is a lookup rather than a walk — the real panel
 * fetches one directory at a time from `GET /api/v1/sites/:id/files?path=…`,
 * and shaping the fixture the same way means the store's call site does not
 * change when that lands.
 */

export interface FsEntry {
  readonly name: string;
  readonly type: "dir" | "file";
  readonly size: string;
  readonly mod: string;
  readonly perm: string;
  /** Short note shown beside the name, e.g. "document root". */
  readonly tag: string;
  /** Set on search hits from another folder, so the row can say where it lives. */
  readonly where?: string;
}

export const DIRS: Readonly<Record<string, readonly FsEntry[]>> = {
  "": [
    { name: "public_html", type: "dir", size: "4.2 MB", mod: "18 Aug 2026, 14:32", perm: "0750", tag: "document root" },
    { name: "DO_NOT_UPLOAD_HERE", type: "file", size: "128 B", mod: "02 Jun 2026, 09:10", perm: "0640", tag: "" },
  ],
  public_html: [
    { name: "wp-admin", type: "dir", size: "11.4 MB", mod: "12 Aug 2026, 03:14", perm: "0755", tag: "" },
    { name: "wp-content", type: "dir", size: "186 MB", mod: "19 Aug 2026, 21:47", perm: "0755", tag: "themes, plugins, uploads" },
    { name: "wp-includes", type: "dir", size: "24.8 MB", mod: "12 Aug 2026, 03:14", perm: "0755", tag: "" },
    { name: ".htaccess", type: "file", size: "412 B", mod: "14 Aug 2026, 10:02", perm: "0644", tag: "" },
    { name: "index.php", type: "file", size: "405 B", mod: "12 Aug 2026, 03:14", perm: "0644", tag: "" },
    { name: "robots.txt", type: "file", size: "218 B", mod: "05 Jul 2026, 16:20", perm: "0644", tag: "" },
    { name: "wp-config.php", type: "file", size: "3.4 KB", mod: "02 Jun 2026, 09:12", perm: "0600", tag: "secrets" },
    { name: "wp-login.php", type: "file", size: "52.1 KB", mod: "12 Aug 2026, 03:14", perm: "0644", tag: "" },
    { name: "xmlrpc.php", type: "file", size: "3.2 KB", mod: "12 Aug 2026, 03:14", perm: "0644", tag: "" },
  ],
  "public_html/wp-content": [
    { name: "plugins", type: "dir", size: "84.2 MB", mod: "19 Aug 2026, 21:47", perm: "0755", tag: "" },
    { name: "themes", type: "dir", size: "18.6 MB", mod: "11 Aug 2026, 12:05", perm: "0755", tag: "" },
    { name: "uploads", type: "dir", size: "82.9 MB", mod: "19 Aug 2026, 18:30", perm: "0755", tag: "" },
    { name: "index.php", type: "file", size: "28 B", mod: "12 Aug 2026, 03:14", perm: "0644", tag: "" },
  ],
};

export const INITIAL_TRASH: readonly FsEntry[] = [
  { name: "old-theme-backup.zip", type: "file", size: "18.4 MB", mod: "02 Aug 2026, 11:40", perm: "0644", tag: "from public_html" },
];

/**
 * Files whose name alone should carry a warning.
 *
 * `wp-config.php` holds database credentials and `DO_NOT_UPLOAD_HERE` marks the
 * account root that is not served — both are files where an accidental edit or
 * upload has consequences, so they are marked before they are opened.
 */
export const SENSITIVE = new Set(["wp-config.php", "DO_NOT_UPLOAD_HERE"]);

export interface FileContents {
  readonly lang: string;
  readonly code: readonly string[];
}

export const FILES: Readonly<Record<string, FileContents>> = {
  DO_NOT_UPLOAD_HERE: {
    lang: "Plain Text",
    code: [
      "This folder is your account root, not your website.",
      "Anything placed here is NOT served to visitors.",
      "",
      "Upload your site files into public_html instead.",
      "",
      "# Support: help@nexpanel.io",
    ],
  },
  "index.php": {
    lang: "PHP",
    code: [
      "<?php",
      "/**",
      " * Front to the WordPress application. This file does not do",
      " * anything, but loads wp-blog-header.php which sets up",
      " * WordPress and renders the active theme.",
      " */",
      "",
      "/**",
      " * Tells WordPress to load the theme and output it.",
      " */",
      "define( 'WP_USE_THEMES', true );",
      "",
      "/** Loads the WordPress environment and template */",
      "require __DIR__ . '/wp-blog-header.php';",
    ],
  },
  "wp-config.php": {
    lang: "PHP",
    code: [
      "<?php",
      "/** Database settings — managed by NexPanel */",
      "define( 'DB_NAME', 'nexp_novaretail' );",
      "define( 'DB_USER', 'nexp_nova' );",
      "define( 'DB_PASSWORD', '••••••••••••••••' );",
      "define( 'DB_HOST', 'localhost' );",
      "define( 'DB_CHARSET', 'utf8mb4' );",
      "",
      "/** Performance and debugging */",
      "define( 'WP_CACHE', true );",
      "define( 'WP_DEBUG', false );",
      "define( 'DISALLOW_FILE_EDIT', true );",
      "",
      "$table_prefix = 'wp_';",
      "",
      "if ( ! defined( 'ABSPATH' ) ) {",
      "  define( 'ABSPATH', __DIR__ . '/' );",
      "}",
      "",
      "require_once ABSPATH . 'wp-settings.php';",
    ],
  },
  ".htaccess": {
    lang: "Apache Conf",
    code: [
      "# BEGIN WordPress",
      "<IfModule mod_rewrite.c>",
      "RewriteEngine On",
      "RewriteBase /",
      "RewriteRule ^index\\.php$ - [L]",
      "RewriteCond %{REQUEST_FILENAME} !-f",
      "RewriteCond %{REQUEST_FILENAME} !-d",
      "RewriteRule . /index.php [L]",
      "</IfModule>",
      "# END WordPress",
      "",
      "# Deny access to sensitive files",
      '<FilesMatch "^(wp-config\\.php|\\.htaccess)$">',
      "  Require all denied",
      "</FilesMatch>",
    ],
  },
  "robots.txt": {
    lang: "Plain Text",
    code: [
      "User-agent: *",
      "Disallow: /wp-admin/",
      "Allow: /wp-admin/admin-ajax.php",
      "",
      "Sitemap: https://novaretail.in/wp-sitemap.xml",
    ],
  },
  "wp-login.php": {
    lang: "PHP",
    code: [
      "<?php",
      "/**",
      " * WordPress user login page.",
      " * Core file — edits are overwritten on update.",
      " */",
      "",
      "require __DIR__ . '/wp-load.php';",
      "",
      "if ( force_ssl_admin() && ! is_ssl() ) {",
      "  wp_safe_redirect( set_url_scheme( wp_login_url(), 'https' ) );",
      "  exit;",
      "}",
    ],
  },
  "xmlrpc.php": {
    lang: "PHP",
    code: [
      "<?php",
      "/** XML-RPC protocol support for WordPress */",
      "",
      "define( 'XMLRPC_REQUEST', true );",
      "",
      "require_once __DIR__ . '/wp-load.php';",
      "",
      "$wp_xmlrpc_server = new wp_xmlrpc_server();",
      "$wp_xmlrpc_server->serve_request();",
    ],
  },
};

export const DISK = { used: "38 GB", total: "200 GB", pct: 19 };
