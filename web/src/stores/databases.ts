import { computed, ref } from "vue";
import { defineStore } from "pinia";

export interface Database {
  readonly name: string;
  readonly user: string;
  /** ISO date the database was created. Formatted at render, not stored formatted. */
  readonly createdAt: string;
  /**
   * The website this database belongs to, or null when nothing owns it.
   *
   * Orphans are real: a database made in phpMyAdmin, over SSH, or left behind by
   * a deleted site has no owner, and a panel that only ever lists *linked*
   * databases hides them forever — they keep using disk and keep their grants,
   * invisible. Listing them here with nothing but an "Assign" control is what
   * makes them recoverable.
   */
  readonly siteId: number | null;
  readonly size: string;
}

/** Privileges the panel can grant, as MySQL names them. */
export const PRIVILEGES = [
  "SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "DROP", "INDEX", "ALTER",
] as const;

export type Privilege = (typeof PRIVILEGES)[number];

const SEED: Database[] = [
  { name: "nexp_novaretail", user: "nexp_admin", createdAt: "2026-03-14", siteId: 1, size: "184 MB" },
  { name: "nexp_novaretail_stg", user: "nexp_admin", createdAt: "2026-06-02", siteId: 1, size: "22 MB" },
  { name: "nexp_api", user: "nexp_api_rw", createdAt: "2026-04-21", siteId: 2, size: "96 MB" },
  { name: "nexp_billing_portal", user: "nexp_admin", createdAt: "2026-02-27", siteId: 3, size: "41 MB" },
  // Two with no owner: one made by hand over SSH, one left behind by a site that
  // was deleted. Both are exactly the case the Assign control exists for.
  { name: "legacy_wp_import", user: "nexp_admin", createdAt: "2025-11-08", siteId: null, size: "312 MB" },
  { name: "analytics_scratch", user: "nexp_reports", createdAt: "2026-01-19", siteId: null, size: "8 MB" },
];

/**
 * The MySQL databases on this server.
 *
 * A store rather than a fixture function because two of the actions on this
 * screen change what the list contains — assigning an orphan to a site, and
 * dropping one — and a screen that says "assigned" while the row still reads
 * "Not linked" is worse than one that cannot assign at all.
 */
export const useDatabasesStore = defineStore("databases", () => {
  const all = ref<Database[]>(SEED.map((d) => ({ ...d })));

  /**
   * What one site's screen shows: its own databases, plus every unowned one.
   *
   * The orphans are included on purpose. They are not this site's, and the row
   * says so — but they have to be visible somewhere to be claimed, and this is
   * the only screen in the panel that talks about databases.
   */
  function forSite(siteId: number) {
    return all.value.filter((d) => d.siteId === siteId || d.siteId === null);
  }

  function isOrphan(db: Database) {
    return db.siteId === null;
  }

  function assign(name: string, siteId: number) {
    all.value = all.value.map((d) => (d.name === name ? { ...d, siteId } : d));
  }

  function remove(name: string) {
    all.value = all.value.filter((d) => d.name !== name);
  }

  function add(name: string, user: string, siteId: number) {
    all.value = [
      ...all.value,
      { name, user, createdAt: new Date().toISOString().slice(0, 10), siteId, size: "0 B" },
    ];
  }

  /** True when the server already has this name — MySQL names are server-wide. */
  function exists(name: string) {
    return all.value.some((d) => d.name === name);
  }

  const orphanCount = computed(() => all.value.filter((d) => d.siteId === null).length);

  return { all, forSite, isOrphan, assign, remove, add, exists, orphanCount };
});
