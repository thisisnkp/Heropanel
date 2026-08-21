<script setup lang="ts">
/**
 * Activity — the full audit trail the dashboard shows the top of.
 *
 * The dashboard's "Recent activity" card is the first five rows of this; the
 * design had no separate screen for it because the prototype's sidebar had no
 * Activity entry, but the mobile design's tab bar does. Rather than invent new
 * content, this shows the same feed with the filter and the retention note that
 * a full trail needs.
 */
import { computed, ref } from "vue";
import { ACTIVITY } from "@/data/dashboard";
import { useUiStore } from "@/stores/ui";

const ui = useUiStore();
const query = ref("");

/** The dashboard feed, plus the older entries a full trail would carry. */
const ALL = [
  ...ACTIVITY,
  { icon: "person", text: "priya opened the web terminal on novaretail.in", when: "yesterday 16:20" },
  { icon: "settings", text: "PHP version changed 8.2 → 8.3 on billing-portal.co", when: "5 days ago" },
  { icon: "restore", text: "Backup 08 Aug 02:00 restored to novaretail.in", when: "last week" },
  { icon: "key", text: "API token 'Old backup script' last used", when: "3 months ago" },
];

const rows = computed(() => {
  const q = query.value.trim().toLowerCase();
  if (!q) return ALL;
  return ALL.filter((r) => r.text.toLowerCase().includes(q));
});
</script>

<template>
  <div class="nx-view">
    <NxPageHeader title="Activity" subtitle="Every action taken on this server, and by whom.">
      <template #actions>
        <NxButton size="lg" @click="ui.toast('Export is not wired up yet.', 'info')">Export CSV</NxButton>
      </template>
    </NxPageHeader>

    <div class="act__search">
      <NxInput v-model="query" icon="search" type="search" placeholder="Search activity" aria-label="Search activity" />
      <span class="act__count">{{ rows.length }} of {{ ALL.length }} events</span>
    </div>

    <NxCard flush>
      <ul v-if="rows.length" class="act__list">
        <li v-for="(r, i) in rows" :key="i" class="act__row">
          <NxIcon :name="r.icon" size="md" class="act__icon" />
          <span class="nx-row__grow act__text">{{ r.text }}</span>
          <span class="act__when">{{ r.when }}</span>
        </li>
      </ul>
      <NxEmptyState
        v-else
        icon="search-off"
        title="Nothing matches that"
        description="Try a shorter phrase, or clear the search to see the full trail."
      />
    </NxCard>

    <p class="act__note">Events are kept for one year and exported as CSV on request.</p>
  </div>
</template>

<style scoped>
.act__search {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
  flex-wrap: wrap;
}
.act__search > :first-child { flex: 1 1 280px; }
.act__count { font-size: var(--nx-text-sm); color: var(--nx-text-muted); white-space: nowrap; }

.act__list { list-style: none; margin: 0; padding: 0; }
.act__row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--nx-hover);
}
.act__row:last-child { border-bottom: 0; }
.act__icon { color: var(--nx-text-muted); }
.act__text { font-size: var(--nx-text-base); color: var(--nx-text-2); }
.act__when {
  font-size: var(--nx-text-sm);
  color: var(--nx-text-placeholder);
  white-space: nowrap;
}
.act__note {
  margin: 12px 0 0;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
}
</style>
