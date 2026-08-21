<script setup lang="ts">
/**
 * Databases — create one, and manage the MySQL databases on this site.
 *
 * The list is this site's databases plus every database on the server that no
 * site owns. Orphans are real — made in phpMyAdmin, over SSH, or left behind by
 * a deleted site — and a panel that lists only linked databases hides them
 * forever while they keep their disk and their grants. They appear here with
 * nothing but an Assign control, which is what makes them recoverable.
 */
import { computed, ref } from "vue";
import { useRouter } from "vue-router";
import { dbPrefix } from "@/data/siteDetail";
import { useDatabasesStore, PRIVILEGES, type Database, type Privilege } from "@/stores/databases";
import { useJobsStore } from "@/stores/jobs";
import { useSitesStore } from "@/stores/sites";
import { useUiStore } from "@/stores/ui";

const router = useRouter();
const sites = useSitesStore();
const dbs = useDatabasesStore();
const jobs = useJobsStore();
const ui = useUiStore();

const site = computed(() => sites.current);
const rows = computed(() => (site.value ? dbs.forSite(site.value.id) : []));

function siteName(id: number | null) {
  return sites.sites.find((s) => s.id === id)?.domain ?? "";
}

/** "14 Mar 2026" — an ISO date is stored, a readable one is shown. */
function created(iso: string) {
  return new Date(iso + "T00:00:00Z").toLocaleDateString("en-GB", {
    day: "numeric",
    month: "short",
    year: "numeric",
    timeZone: "UTC",
  });
}

// ---- create ----------------------------------------------------------------

/**
 * Every database and user on this site is prefixed with the site's own
 * identifier, and the prefix is fixed rather than typed.
 *
 * Two reasons, and the second is the one that matters: MySQL names are global to
 * the server, so without a per-site prefix the first site to claim `shop` denies
 * it to every other site on the box — and worse, a name collision is how one
 * tenant's grant ends up pointing at another tenant's data.
 */
const prefix = computed(() => (site.value ? dbPrefix(site.value.domain) : ""));

const dbName = ref("");
const userName = ref("");
const password = ref("");

/**
 * Generated passwords are revealed, always.
 *
 * A password you cannot read is one you cannot put in your connection string,
 * and this is the only moment it will ever be shown — the panel stores a hash,
 * not the string. Hiding it behind the same dots as a typed one would mean the
 * generate button produces something unusable.
 */
const revealed = ref(false);

/**
 * crypto.getRandomValues, not Math.random.
 *
 * Math.random is seeded per page and is not a CSPRNG; passwords generated from
 * it are guessable from a handful of samples. This is the one place in the UI
 * that mints a credential, so it uses the real source.
 *
 * The alphabet omits characters that are read wrong out of a terminal or a
 * screenshot — 0/O, 1/l/I — and every symbol MySQL or a shell would need quoted.
 * A password that has to be escaped in a DSN is a support ticket.
 */
const ALPHABET = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789-_";

function newPassword() {
  const bytes = new Uint32Array(20);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => ALPHABET[b % ALPHABET.length]).join("");
}

function generate() {
  password.value = newPassword();
  revealed.value = true;
}

/**
 * MySQL's own limits, checked here because the prefix makes them non-obvious:
 * you are typing into a 64-character budget that is already part spent, and the
 * only other place to find that out is a server error after you press Create.
 */
const DB_MAX = 64;
const USER_MAX = 32;
const LEGAL = /^[A-Za-z0-9_]*$/;

function check(value: string, max: number, what: string): string {
  if (!value) return "";
  if (!LEGAL.test(value)) return "Letters, digits and underscores only.";
  if (prefix.value.length + value.length > max) {
    const left = max - prefix.value.length;
    return `Too long — ${what} names are ${max} characters, leaving ${left} after the prefix.`;
  }
  return "";
}

const dbError = computed(() => {
  const base = check(dbName.value, DB_MAX, "database");
  if (base) return base;
  // Names are server-wide, so the clash can be with another site's database —
  // one this screen does not even list.
  if (dbName.value && dbs.exists(prefix.value + dbName.value)) return "That database already exists.";
  return "";
});
const userError = computed(() => check(userName.value, USER_MAX, "user"));

const complete = computed(
  () => Boolean(dbName.value && userName.value && password.value) && !dbError.value && !userError.value,
);

function create() {
  if (!complete.value || !site.value) return;
  dbs.add(prefix.value + dbName.value, prefix.value + userName.value, site.value.id);
  ui.toast(`Created ${prefix.value}${dbName.value}.`, "success");
  dbName.value = "";
  userName.value = "";
  password.value = "";
  revealed.value = false;
}

// ---- row actions -----------------------------------------------------------

const openMenu = ref<string | null>(null);

function assign(db: Database) {
  if (!site.value) return;
  dbs.assign(db.name, site.value.id);
  ui.toast(`${db.name} is now linked to ${site.value.domain}.`, "success");
}

/** Repair is a long-running table scan, so it reports in the job tray. */
function repair(db: Database) {
  jobs.start("Repair " + db.name, siteName(db.siteId) || "unassigned", "Checking tables");
}

const pwFor = ref<Database | null>(null);
const pwValue = ref("");
const pwRevealed = ref(false);

function openPassword(db: Database) {
  pwFor.value = db;
  pwValue.value = "";
  pwRevealed.value = false;
}

function savePassword() {
  if (!pwFor.value || !pwValue.value) return;
  ui.toast(`Password changed for ${pwFor.value.user}.`, "success");
  pwFor.value = null;
}

const permsFor = ref<Database | null>(null);
const perms = ref<Privilege[]>([]);

function openPerms(db: Database) {
  permsFor.value = db;
  // Everything except DROP: the common grant, and the one destructive privilege
  // is the one worth making someone tick deliberately.
  perms.value = PRIVILEGES.filter((p) => p !== "DROP");
}

function togglePriv(p: Privilege) {
  perms.value = perms.value.includes(p) ? perms.value.filter((x) => x !== p) : [...perms.value, p];
}

function savePerms() {
  if (!permsFor.value) return;
  ui.toast(`${perms.value.length} privileges set on ${permsFor.value.name}.`, "success");
  permsFor.value = null;
}

const deleteFor = ref<Database | null>(null);
const typedName = ref("");
const canDelete = computed(() => typedName.value.trim() === deleteFor.value?.name);

function openDelete(db: Database) {
  deleteFor.value = db;
  typedName.value = "";
}

function confirmDelete() {
  const db = deleteFor.value;
  if (!db || !canDelete.value) return;
  dbs.remove(db.name);
  deleteFor.value = null;
  // Dropping a database is not reversible from here, so the toast says where the
  // copy is rather than offering an Undo it cannot honour.
  ui.toast(`${db.name} dropped. The last nightly backup still has it.`, "danger");
}
</script>

<template>
  <div v-if="site">
    <SiteHeader kicker="Data" title="Databases" sub="MySQL databases attached to this site." />

    <div class="nx-view">
      <NxCard title="Create database" subtitle="The site prefix is added for you and cannot be changed.">
        <form class="db__form" @submit.prevent="create">
          <div class="db__grid">
            <NxField label="MySQL database name" :error="dbError">
              <template #default="{ id, describedBy, invalid }">
                <NxInput
                  :id="id"
                  v-model="dbName"
                  mono
                  placeholder="Enter Database Name"
                  autocomplete="off"
                  spellcheck="false"
                  :invalid="invalid"
                  :aria-describedby="describedBy"
                >
                  <template #prefix>{{ prefix }}</template>
                </NxInput>
              </template>
            </NxField>

            <NxField label="MySQL username" :error="userError">
              <template #default="{ id, describedBy, invalid }">
                <NxInput
                  :id="id"
                  v-model="userName"
                  mono
                  placeholder="Enter User Name"
                  autocomplete="off"
                  spellcheck="false"
                  :invalid="invalid"
                  :aria-describedby="describedBy"
                >
                  <template #prefix>{{ prefix }}</template>
                </NxInput>
              </template>
            </NxField>

            <NxField label="Password">
              <template #default="{ id }">
                <NxInput
                  :id="id"
                  v-model="password"
                  :type="revealed ? 'text' : 'password'"
                  :mono="revealed"
                  placeholder="Password"
                  autocomplete="new-password"
                  spellcheck="false"
                >
                  <template #suffix>
                    <button
                      type="button"
                      class="db__icon"
                      title="Generate a strong password"
                      aria-label="Generate a strong password"
                      @click="generate"
                    >
                      <NxIcon name="autorenew" size="md" />
                    </button>
                    <button
                      type="button"
                      class="db__icon"
                      :title="revealed ? 'Hide password' : 'Show password'"
                      :aria-label="revealed ? 'Hide password' : 'Show password'"
                      :aria-pressed="revealed"
                      @click="revealed = !revealed"
                    >
                      <NxIcon :name="revealed ? 'visibility-off' : 'visibility'" size="md" />
                    </button>
                  </template>
                </NxInput>
              </template>
            </NxField>
          </div>

          <div class="db__actions-row">
            <!-- The name it will actually get, spelled out. The prefix is inside
                 the field, so the finished string is the one thing you cannot
                 read off the form in one piece. -->
            <p v-if="dbName && !dbError" class="db__preview nx-mono">{{ prefix }}{{ dbName }}</p>
            <span class="nx-row__grow" />
            <NxButton type="submit" variant="primary" size="lg" :disabled="!complete">
              Create database
            </NxButton>
          </div>
        </form>
      </NxCard>

      <NxCard title="MySQL databases" flush class="db__list">
        <NxTable
          :columns="[
            { key: 'name', label: 'MySQL Database', width: '1.3fr' },
            { key: 'user', label: 'MySQL User', width: '1fr' },
            { key: 'created', label: 'Created at', width: '0.9fr' },
            { key: 'website', label: 'Website', width: '1.1fr' },
            { key: 'actions', label: 'Actions', width: '230px', align: 'end' },
          ]"
          :rows="rows"
          :row-key="(d) => d.name"
        >
          <template #default="{ row }">
            <div class="db__name nx-mono nx-truncate">{{ row.name }}</div>
            <div class="db__muted nx-mono nx-truncate">{{ row.user }}</div>
            <div class="db__muted">{{ created(row.createdAt) }}</div>

            <div class="db__site">
              <!-- Owned by this site, or owned by nobody. There is no third case
                   here: another site's databases are not in this list. -->
              <span v-if="!dbs.isOrphan(row)" class="db__linked nx-mono nx-truncate">
                {{ siteName(row.siteId) }}
              </span>
              <NxButton v-else size="sm" @click="assign(row)">
                <NxIcon name="add-link" size="sm" />
                Assign
              </NxButton>
            </div>

            <div class="db__row-actions">
              <NxButton size="sm" @click="router.push({ name: 'site-phpmyadmin' })">
                Enter phpMyAdmin
              </NxButton>

              <NxMenu
                :open="openMenu === row.name"
                width="220px"
                @update:open="(v) => (openMenu = v ? row.name : null)"
              >
                <template #trigger="{ toggle }">
                  <button
                    type="button"
                    class="db__dots"
                    :aria-label="'More actions for ' + row.name"
                    :aria-expanded="openMenu === row.name"
                    @click="toggle"
                  >
                    <NxIcon name="more-vert" size="md" />
                  </button>
                </template>

                <button type="button" class="db__menu-item" role="menuitem" @click="repair(row)">
                  <NxIcon name="build" size="sm" />Repair
                </button>
                <button type="button" class="db__menu-item" role="menuitem" @click="openPassword(row)">
                  <NxIcon name="key" size="sm" />Change Password
                </button>
                <button type="button" class="db__menu-item" role="menuitem" @click="openPerms(row)">
                  <NxIcon name="admin-panel-settings" size="sm" />Change Permissions
                </button>
                <button
                  type="button"
                  class="db__menu-item is-danger"
                  role="menuitem"
                  @click="openDelete(row)"
                >
                  <NxIcon name="delete" size="sm" />Delete
                </button>
              </NxMenu>
            </div>
          </template>
        </NxTable>
      </NxCard>
    </div>

    <!-- Change password -->
    <NxModal
      :open="pwFor !== null"
      title="Change password"
      :description="pwFor ? 'For the MySQL user ' + pwFor.user + '.' : ''"
      @update:open="(v) => { if (!v) pwFor = null; }"
    >
      <NxField label="New password">
        <template #default="{ id }">
          <NxInput
            :id="id"
            v-model="pwValue"
            :type="pwRevealed ? 'text' : 'password'"
            :mono="pwRevealed"
            placeholder="Password"
            autocomplete="new-password"
            spellcheck="false"
          >
            <template #suffix>
              <button
                type="button"
                class="db__icon"
                aria-label="Generate a strong password"
                title="Generate a strong password"
                @click="pwValue = newPassword(); pwRevealed = true"
              >
                <NxIcon name="autorenew" size="md" />
              </button>
              <button
                type="button"
                class="db__icon"
                :aria-label="pwRevealed ? 'Hide password' : 'Show password'"
                :aria-pressed="pwRevealed"
                @click="pwRevealed = !pwRevealed"
              >
                <NxIcon :name="pwRevealed ? 'visibility-off' : 'visibility'" size="md" />
              </button>
            </template>
          </NxInput>
        </template>
      </NxField>
      <NxCallout tone="warning" class="db__note">
        Anything connecting with the old password stops working the moment this is
        saved — update your site's config in the same sitting.
      </NxCallout>

      <template #footer>
        <NxButton @click="pwFor = null">Cancel</NxButton>
        <NxButton variant="primary" :disabled="!pwValue" @click="savePassword">Change password</NxButton>
      </template>
    </NxModal>

    <!-- Change permissions -->
    <NxModal
      :open="permsFor !== null"
      title="Change permissions"
      :description="permsFor ? permsFor.user + ' on ' + permsFor.name : ''"
      @update:open="(v) => { if (!v) permsFor = null; }"
    >
      <ul class="db__privs">
        <li v-for="p in PRIVILEGES" :key="p">
          <label class="db__priv">
            <input type="checkbox" :checked="perms.includes(p)" @change="togglePriv(p)" />
            <span class="nx-mono">{{ p }}</span>
            <!-- Named, because DROP is the one on this list that loses data. -->
            <span v-if="p === 'DROP'" class="db__priv-warn">destroys tables</span>
          </label>
        </li>
      </ul>

      <template #footer>
        <NxButton @click="permsFor = null">Cancel</NxButton>
        <NxButton variant="primary" @click="savePerms">Save permissions</NxButton>
      </template>
    </NxModal>

    <!-- Delete -->
    <NxModal
      :open="deleteFor !== null"
      title="Drop this database?"
      description="Every table in it goes. The last nightly backup still has a copy."
      :dismissible="false"
      @update:open="(v) => { if (!v) deleteFor = null; }"
    >
      <NxField
        label="Type the database name to confirm"
        :hint="'Enter ' + (deleteFor?.name ?? '') + ' exactly.'"
      >
        <template #default="{ id, describedBy }">
          <NxInput
            :id="id"
            v-model="typedName"
            mono
            :placeholder="deleteFor?.name"
            :aria-describedby="describedBy"
            autocomplete="off"
          />
        </template>
      </NxField>

      <template #footer>
        <NxButton @click="deleteFor = null">Cancel</NxButton>
        <NxButton variant="danger" :disabled="!canDelete" @click="confirmDelete">Drop database</NxButton>
      </template>
    </NxModal>
  </div>
  <NxSkeleton v-else height="200px" />
</template>

<style scoped>
.db__form { display: flex; flex-direction: column; gap: 16px; }
/* One field per row, each spanning the card. */
.db__grid { display: flex; flex-direction: column; gap: 14px; }
.db__icon {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: 0;
  background: transparent;
  border-radius: var(--nx-radius-sm);
  cursor: pointer;
  color: var(--nx-text-muted);
  padding: 0;
  /* Pulled back into the field's own padding so a 28px target does not make this
     one control eleven pixels taller than the two above it. */
  margin: -6px 0;
}
.db__icon:hover { background: var(--nx-hover); color: var(--nx-text); }
.db__actions-row { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.db__preview {
  margin: 0;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
  min-width: 0;
  overflow-wrap: anywhere;
}

.db__list { margin-top: 12px; }
.db__name { font-weight: 500; }
.db__muted { color: var(--nx-text-muted); }
.db__site { display: flex; align-items: center; min-width: 0; }
.db__linked { color: var(--nx-text-muted); }
.db__row-actions { display: flex; align-items: center; justify-content: flex-end; gap: 8px; }
.db__dots {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  flex: 0 0 30px;
  border: 1px solid var(--nx-border);
  background: var(--nx-surface);
  border-radius: var(--nx-radius-md);
  cursor: pointer;
  color: var(--nx-text-2);
  padding: 0;
}
.db__dots:hover { background: var(--nx-hover); }
.db__menu-item {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  text-align: left;
  border: 0;
  background: transparent;
  border-radius: var(--nx-radius-md);
  padding: 9px 10px;
  font-size: var(--nx-text-base);
  font-family: inherit;
  color: var(--nx-text-2);
  cursor: pointer;
}
.db__menu-item:hover { background: var(--nx-hover); color: var(--nx-text); }
.db__menu-item.is-danger { color: var(--nx-danger); }
.db__menu-item.is-danger:hover { background: var(--nx-danger-soft); }

.db__privs { list-style: none; margin: 0; padding: 0; display: grid; grid-template-columns: 1fr 1fr; gap: 4px 16px; }
.db__priv { display: flex; align-items: center; gap: 8px; padding: 6px 0; cursor: pointer; font-size: var(--nx-text-base); }
.db__priv-warn { font-size: var(--nx-text-xs); color: var(--nx-danger); }
.db__note { margin-top: 12px; }
</style>
