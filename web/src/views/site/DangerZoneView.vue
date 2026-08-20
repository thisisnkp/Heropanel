<script setup lang="ts">
/**
 * Danger zone — deleting the site.
 *
 * The same typed-domain confirmation as the websites list, on purpose: a
 * destructive action should not be easier to reach from inside the site than
 * from the list, and someone who has learned the gesture in one place should
 * find it unchanged in the other.
 */
import { computed, ref } from "vue";
import { useRouter } from "vue-router";
import { useSitesStore } from "@/stores/sites";
import { useUiStore } from "@/stores/ui";

const sites = useSitesStore();
const ui = useUiStore();
const router = useRouter();

const site = computed(() => sites.current);
const open = ref(false);
const typed = ref("");

const canDelete = computed(() => site.value !== null && typed.value.trim() === site.value.domain);

function ask() {
  typed.value = "";
  open.value = true;
}

function confirmDelete() {
  if (!canDelete.value || !site.value) return;
  const name = site.value.name;
  sites.remove(site.value.id);
  open.value = false;
  ui.toast(name + " deleted.", "success");
  void router.push({ name: "websites" });
}
</script>

<template>
  <div v-if="site">
    <SiteHeader kicker="Danger zone" title="Delete website" sub="Irreversible actions for this site." />

    <div class="nx-view">
      <section class="dz">
        <h2 class="dz__title">Delete this website</h2>
        <p class="dz__text">
          Files, databases and deployment settings for {{ site.domain }} are removed. Backups stay available for
          14 days. This cannot be undone from the panel.
        </p>
        <NxButton variant="danger" size="lg" class="dz__btn" @click="ask">Delete website</NxButton>
      </section>
    </div>

    <NxModal
      v-model:open="open"
      title="Delete this website?"
      description="Files and databases are deleted; backups remain for 14 days."
      :dismissible="false"
    >
      <NxField label="Type the domain to confirm" :hint="'Enter ' + site.domain + ' exactly.'">
        <template #default="{ id, describedBy }">
          <NxInput
            :id="id"
            v-model="typed"
            mono
            :aria-describedby="describedBy"
            :placeholder="site.domain"
            autocomplete="off"
          />
        </template>
      </NxField>

      <template #footer>
        <NxButton @click="open = false">Cancel</NxButton>
        <NxButton variant="primary" class="dz__confirm" :disabled="!canDelete" @click="confirmDelete">
          Delete permanently
        </NxButton>
      </template>
    </NxModal>
  </div>
  <NxSkeleton v-else height="200px" />
</template>

<style scoped>
.dz {
  background: var(--nx-surface);
  border: 1px solid var(--nx-danger-border);
  border-radius: var(--nx-radius-lg);
  padding: 20px;
}
.dz__title {
  margin: 0;
  font-size: var(--nx-text-base);
  font-weight: 600;
  color: var(--nx-danger);
}
.dz__text {
  margin: 6px 0 16px;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
  line-height: 1.6;
  max-width: 560px;
  text-wrap: pretty;
}
.dz__btn {
  border-color: var(--nx-danger-border);
  background: var(--nx-danger-soft);
}
.dz__confirm:not(:disabled) { background: var(--nx-danger); }
.dz__confirm:not(:disabled):hover { background: var(--nx-danger); filter: brightness(0.94); }
</style>
