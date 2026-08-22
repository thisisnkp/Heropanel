<script setup lang="ts">
/**
 * Websites — everything hosted here, with the create-site entry point.
 *
 * Delete is gated on typing the domain back. That is the design's behaviour and
 * worth keeping literally: this list is the one place a whole site can be
 * removed, and the rows differ by a subdomain in several cases, so an
 * accidental click on the wrong row is a plausible way to lose the wrong site.
 */
import { computed, ref } from "vue";
import { useRouter } from "vue-router";
import { STACKS, STACK_KEYS, type StackKey } from "@/config/stacks";
import { AUTOMATION_TILES } from "@/data/dashboard";
import { api, ApiRequestError } from "@/lib/api";
import { useSitesStore, type Site } from "@/stores/sites";
import { useUiStore } from "@/stores/ui";

const sites = useSitesStore();
const ui = useUiStore();
const router = useRouter();

const addMenuOpen = ref(false);

const stats = computed(() => [
  { label: "Websites", value: String(sites.count), sub: sites.sites.filter((s) => s.status === "live").length + " live" },
  { label: "Deploys today", value: "7", sub: "1 failed, retried" },
  { label: "Disk used", value: "38 GB", sub: "of 200 GB" },
  { label: "Automations", value: "15", sub: "OpenClaw + n8n" },
]);

const STATUS_LABEL: Record<Site["status"], string> = {
  live: "live",
  building: "building…",
  suspended: "suspended",
};

function openSite(uid: string) {
  void router.push({ name: "site-overview", params: { uid } });
}

const wizardOpen = ref(false);
const wizardStack = ref<StackKey>("static");

function startCreate(stack: StackKey) {
  addMenuOpen.value = false;
  wizardStack.value = stack;
  wizardOpen.value = true;
}

// ---- delete confirmation ----------------------------------------------------
const pendingDelete = ref<Site | null>(null);
const typedName = ref("");
const deleting = ref(false);
const deleteError = ref<string | null>(null);

const canDelete = computed(
  () => pendingDelete.value !== null && typedName.value.trim() === pendingDelete.value.domain,
);

function askDelete(site: Site) {
  pendingDelete.value = site;
  typedName.value = "";
  deleteError.value = null;
}

/**
 * Deletes on the server, then drops the row.
 *
 * In that order. Removing the row first shows the site as gone whether or not
 * it is — and a delete that failed halfway is exactly the state an operator has
 * to be able to see rather than have hidden behind an optimistic list.
 */
async function confirmDelete() {
  if (!canDelete.value || !pendingDelete.value) return;
  const { uid, name } = pendingDelete.value;
  deleting.value = true;
  deleteError.value = null;
  try {
    await api.del(`/sites/${uid}`);
    sites.remove(uid);
    pendingDelete.value = null;
    typedName.value = "";
    ui.toast(name + " deleted.", "success");
  } catch (e) {
    deleteError.value = e instanceof ApiRequestError ? e.message : "The website could not be deleted.";
  } finally {
    deleting.value = false;
  }
}
</script>

<template>
  <div class="nx-view">
    <header class="list__head">
      <div class="nx-row__grow">
        <h1 class="list__title">Websites</h1>
        <p class="list__sub">Everything you host, in one list. Open a site to manage it.</p>
      </div>

      <NxMenu v-model:open="addMenuOpen" width="268px">
        <template #trigger="{ toggle }">
          <NxButton variant="primary" size="lg" :aria-expanded="addMenuOpen" @click="toggle">
            Add Website
            <NxIcon name="expand-more" size="sm" />
          </NxButton>
        </template>

        <p class="list__menu-caption">Choose a type</p>
        <button
          v-for="key in STACK_KEYS"
          :key="key"
          type="button"
          class="list__menu-item"
          role="menuitem"
          @click="startCreate(key)"
        >
          <span class="list__tag" :style="{ background: STACKS[key].bg, color: STACKS[key].fg }">
            {{ STACKS[key].tag }}
          </span>
          <span class="nx-row__grow">
            <span class="list__menu-label">{{ STACKS[key].label }}</span>
            <span class="list__menu-hint">{{ STACKS[key].hint }}</span>
          </span>
        </button>

        <div class="list__menu-item is-soon">
          <span class="list__tag list__tag--soon">WB</span>
          <span class="nx-row__grow">
            <span class="list__menu-label">Website Builder</span>
            <span class="list__menu-hint">Coming soon</span>
          </span>
        </div>
      </NxMenu>
    </header>

    <div class="nx-grid nx-grid--4 list__block">
      <NxStat v-for="s in stats" :key="s.label" :label="s.label" :value="s.value" :sub="s.sub" />
    </div>

    <div class="nx-grid nx-grid--2 list__block">
      <div v-for="a in AUTOMATION_TILES" :key="a.name" class="list__auto">
        <div class="list__auto-tag" :class="'list__auto-tag--' + a.tone">{{ a.tag }}</div>
        <div class="nx-row__grow">
          <div class="list__auto-name">{{ a.name }}</div>
          <div class="list__auto-sub">{{ a.sub }}</div>
        </div>
        <NxButton @click="router.push({ name: a.to })">Open</NxButton>
      </div>
    </div>

    <!-- Three states, not two. "No websites yet" is a claim about the server,
         and showing it while the list is still in flight — or after the request
         failed — makes the panel assert something it does not know. -->
    <NxCallout v-if="sites.error" tone="danger" title="Could not load your websites">
      {{ sites.error }}
      <template #actions>
        <NxButton size="sm" @click="sites.reload()">Try again</NxButton>
      </template>
    </NxCallout>

    <NxCard v-else-if="sites.loading && sites.count === 0" flush>
      <div class="list__loading"><NxSkeleton height="160px" /></div>
    </NxCard>

    <NxCard v-else-if="sites.count > 0" flush>
      <NxTable
        :columns="[
          { key: 'name', label: 'Website', width: '1.9fr' },
          { key: 'type', label: 'Type', width: '0.9fr' },
          { key: 'source', label: 'Source', width: '0.9fr' },
          { key: 'deploy', label: 'Last deploy', width: '1fr' },
          { key: 'actions', label: '', width: '168px', align: 'end' },
        ]"
        :rows="sites.sites"
        :row-key="(s) => s.uid"
      >
        <template #default="{ row }">
          <div class="list__name-cell">
            <span class="list__status-dot" :class="'list__status-dot--' + row.status" aria-hidden="true" />
            <span class="nx-truncate">
              <span class="list__name nx-truncate">{{ row.name }}</span>
              <span class="list__status nx-mono">{{ STATUS_LABEL[row.status] }}</span>
            </span>
          </div>
          <div>
            <NxBadge :bg="STACKS[row.stackKey].bg" :fg="STACKS[row.stackKey].fg">
              {{ STACKS[row.stackKey].label }}
            </NxBadge>
          </div>
          <div class="list__muted nx-mono nx-truncate">{{ row.deploy }}</div>
          <div class="list__muted nx-truncate">{{ row.lastDeploy }}</div>
          <div class="list__actions">
            <NxButton variant="primary" @click="openSite(row.uid)">Dashboard</NxButton>
            <NxButton variant="danger" @click="askDelete(row)">Delete</NxButton>
          </div>
        </template>
      </NxTable>
    </NxCard>

    <div v-else class="list__empty">
      <div class="list__empty-mark" aria-hidden="true" />
      <h2 class="list__empty-title">No websites yet</h2>
      <p class="list__empty-text">
        Pick a type, point a domain at it, then upload files or connect GitHub. Takes about a minute.
      </p>
      <NxButton variant="primary" size="lg" @click="addMenuOpen = true">Add your first website</NxButton>
    </div>

    <CreateSiteWizard v-model:open="wizardOpen" :stack="wizardStack" />

    <NxModal
      :open="pendingDelete !== null"
      title="Delete this website?"
      description="Files and databases are deleted; backups remain for 14 days."
      :dismissible="false"
      @update:open="(v) => { if (!v) pendingDelete = null; }"
    >
      <NxCallout v-if="deleteError" tone="danger">{{ deleteError }}</NxCallout>

      <NxField label="Type the domain to confirm" :hint="'Enter ' + (pendingDelete?.domain ?? '') + ' exactly.'">
        <template #default="{ id, describedBy }">
          <NxInput
            :id="id"
            v-model="typedName"
            mono
            :aria-describedby="describedBy"
            :placeholder="pendingDelete?.domain"
            autocomplete="off"
          />
        </template>
      </NxField>

      <template #footer>
        <NxButton @click="pendingDelete = null">Cancel</NxButton>
        <NxButton
          variant="primary"
          class="list__delete-btn"
          :disabled="!canDelete"
          :loading="deleting"
          @click="confirmDelete"
        >
          Delete website
        </NxButton>
      </template>
    </NxModal>
  </div>
</template>

<style scoped>
.list__head {
  display: flex;
  align-items: flex-end;
  gap: 16px;
  padding-bottom: 24px;
}
.list__title {
  margin: 0;
  font-size: var(--nx-text-xl);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
}
.list__sub {
  margin: 6px 0 0;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
}
.list__block { padding-bottom: 24px; }

.list__menu-caption {
  margin: 0;
  font-size: var(--nx-text-xs);
  letter-spacing: var(--nx-ls-caps);
  color: var(--nx-text-placeholder);
  padding: 8px 12px 6px;
  font-weight: 600;
  text-transform: uppercase;
}
.list__menu-item {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  text-align: left;
  border: 0;
  background: transparent;
  cursor: pointer;
  padding: 8px 12px;
  border-radius: var(--nx-radius-md);
  font-family: inherit;
}
.list__menu-item:hover { background: var(--nx-hover); }
.list__menu-item.is-soon {
  cursor: default;
  opacity: 0.55;
  margin-top: 4px;
  border-top: 1px solid var(--nx-active);
  border-radius: 0;
}
.list__menu-item.is-soon:hover { background: transparent; }
.list__menu-label { display: block; font-size: var(--nx-text-base); color: var(--nx-text); }
.list__menu-hint { display: block; font-size: var(--nx-text-sm); color: var(--nx-text-muted); }

.list__tag {
  width: 26px;
  height: 26px;
  flex: 0 0 26px;
  border-radius: var(--nx-radius-sm);
  font-size: var(--nx-text-xs);
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
  font-family: "JetBrains Mono", ui-monospace, monospace;
}
.list__tag--soon { background: var(--nx-hover); color: var(--nx-text-muted); }

.list__auto {
  display: flex;
  align-items: center;
  gap: 16px;
  background: var(--nx-surface);
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-lg);
  padding: 16px;
}
.list__auto-tag {
  width: 38px;
  height: 38px;
  flex: 0 0 38px;
  border-radius: var(--nx-radius-md);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: var(--nx-text-sm);
  font-weight: 600;
  font-family: "JetBrains Mono", ui-monospace, monospace;
}
.list__auto-tag--info { background: var(--nx-info-soft); color: var(--nx-stack-php); }
.list__auto-tag--danger { background: var(--nx-danger-soft); color: var(--nx-danger); }
.list__auto-name {
  font-size: var(--nx-text-md);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
}
.list__auto-sub {
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
  padding-top: 4px;
}

.list__name-cell { display: flex; align-items: center; gap: 12px; min-width: 0; }
.list__status-dot {
  width: 8px;
  height: 8px;
  flex: 0 0 8px;
  border-radius: var(--nx-radius-full);
}
.list__status-dot--live { background: var(--nx-success); }
.list__status-dot--building { background: var(--nx-warning); }
.list__status-dot--suspended { background: var(--nx-text-placeholder); }
.list__name {
  display: block;
  font-size: var(--nx-text-md);
  font-weight: 500;
  letter-spacing: var(--nx-ls-tight);
}
.list__status {
  display: block;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
}
.list__muted { color: var(--nx-text-muted); }
.list__actions { display: flex; gap: 8px; justify-content: flex-end; }

.list__loading { padding: 16px; }
.list__empty {
  background: var(--nx-surface);
  border: 1px dashed var(--nx-border-strong);
  border-radius: var(--nx-radius-lg);
  padding: 56px 32px;
  text-align: center;
}
.list__empty-mark {
  width: 44px;
  height: 44px;
  border-radius: var(--nx-radius-lg);
  background: var(--nx-hover);
  margin: 0 auto 16px;
}
.list__empty-title {
  margin: 0;
  font-size: var(--nx-text-lg);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
}
.list__empty-text {
  margin: 6px auto 20px;
  max-width: 52ch;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
  text-wrap: pretty;
}

/* The confirm button is primary-shaped but destructive-coloured: it is the
 * affirmative action in the dialog, and the danger variant reads as a secondary
 * "cancel-adjacent" control next to a filled button. */
.list__delete-btn:not(:disabled) { background: var(--nx-danger); }
.list__delete-btn:not(:disabled):hover { background: var(--nx-danger); filter: brightness(0.94); }
</style>
