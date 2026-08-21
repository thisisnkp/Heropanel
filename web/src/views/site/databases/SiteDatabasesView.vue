<script setup lang="ts">
/** Databases — create one, and the MySQL databases already attached to this site. */
import { computed, ref } from "vue";
import { databases, dbPrefix } from "@/data/siteDetail";
import { useSitesStore } from "@/stores/sites";
import { useUiStore } from "@/stores/ui";

const sites = useSitesStore();
const ui = useUiStore();

const site = computed(() => sites.current);
const rows = computed(() => (site.value ? databases(site.value) : []));

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

function generate() {
  const bytes = new Uint32Array(20);
  crypto.getRandomValues(bytes);
  // Rejection-free because the alphabet length divides evenly enough that the
  // modulo bias over 2^32 is far below anything that matters at 20 characters.
  password.value = Array.from(bytes, (b) => ALPHABET[b % ALPHABET.length]).join("");
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

const dbError = computed(() => check(dbName.value, DB_MAX, "database"));
const userError = computed(() => check(userName.value, USER_MAX, "user"));

const complete = computed(
  () => Boolean(dbName.value && userName.value && password.value) && !dbError.value && !userError.value,
);

function create() {
  if (!complete.value) return;
  ui.toast(
    `Creating ${prefix.value}${dbName.value} is not wired up yet.`,
    "info",
  );
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
            <p v-if="dbName && !dbError" class="db__preview nx-mono">
              {{ prefix }}{{ dbName }}
            </p>
            <span class="nx-row__grow" />
            <NxButton type="submit" variant="primary" size="lg" :disabled="!complete">
              Create database
            </NxButton>
          </div>
        </form>
      </NxCard>

      <NxCard title="MySQL databases" flush class="db__list">
        <template #action>
          <NxButton @click="$router.push({ name: 'site-phpmyadmin' })">Open phpMyAdmin</NxButton>
        </template>

        <NxTable
          :columns="[
            { key: 'name', label: 'Database', width: '1.4fr' },
            { key: 'user', label: 'User', width: '1fr' },
            { key: 'size', label: 'Size', width: '0.7fr' },
            { key: 'actions', label: '', width: '90px', align: 'end' },
          ]"
          :rows="rows"
          :row-key="(d) => d.name"
        >
          <template #default="{ row }">
            <div class="db__name nx-mono nx-truncate">{{ row.name }}</div>
            <div class="db__muted nx-mono">{{ row.user }}</div>
            <div class="db__muted">{{ row.size }}</div>
            <div class="db__actions">
              <NxButton @click="ui.toast('Export is not wired up yet.', 'info')">Export</NxButton>
            </div>
          </template>
        </NxTable>
      </NxCard>
    </div>
  </div>
  <NxSkeleton v-else height="200px" />
</template>

<style scoped>
.db__form { display: flex; flex-direction: column; gap: 16px; }
/* One field per row, each spanning the card. */
.db__grid {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
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
.db__actions { display: flex; justify-content: flex-end; }
</style>
