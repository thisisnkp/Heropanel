<script setup lang="ts">
/**
 * Notifications — the alerts that need a decision.
 *
 * Separate from Activity on purpose, and the screen says so: activity is
 * everything that happened, notifications are the subset worth interrupting
 * someone for. The "what we notify about" panel is part of the screen rather
 * than buried in settings, so an empty list reads as "nothing is wrong" instead
 * of "notifications are probably broken".
 */
import { NOTIFICATIONS, NOTIFY_ABOUT } from "@/data/dashboard";
import { useUiStore } from "@/stores/ui";

const ui = useUiStore();
</script>

<template>
  <div class="nx-view">
    <NxPageHeader title="Notifications" subtitle="Things that need a decision. Routine events stay in Activity." />

    <NxCard v-if="NOTIFICATIONS.length" flush class="notif__block">
      <ul class="notif__list">
        <li v-for="n in NOTIFICATIONS" :key="n.label" class="notif__row">
          <span class="notif__icon" :class="'notif__icon--' + n.severity">
            <NxIcon :name="n.icon" size="sm" />
          </span>
          <span class="nx-row__grow">
            <span class="notif__label">{{ n.label }}</span>
            <span class="notif__sub">{{ n.sub }}</span>
            <span class="notif__when">{{ n.when }}</span>
          </span>
        </li>
      </ul>
    </NxCard>

    <NxCard v-else class="notif__block">
      <NxEmptyState
        icon="notifications"
        title="You're all caught up"
        description="Anything that needs you shows up here. Routine events stay in Activity."
      >
        <NxButton @click="$router.push({ name: 'activity' })">Open Activity</NxButton>
      </NxEmptyState>
    </NxCard>

    <NxCard title="What we notify about">
      <p class="notif__about">{{ NOTIFY_ABOUT }}</p>
      <NxButton class="notif__settings" @click="ui.toast('Notification settings are not wired up yet.', 'info')">
        Notification settings
      </NxButton>
    </NxCard>
  </div>
</template>

<style scoped>
.notif__block { margin-bottom: 12px; }
.notif__list { list-style: none; margin: 0; padding: 0; }
.notif__row {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 14px 16px;
  border-bottom: 1px solid var(--nx-hover);
}
.notif__row:last-child { border-bottom: 0; }
.notif__icon {
  width: 30px;
  height: 30px;
  flex: 0 0 30px;
  border-radius: var(--nx-radius-sm);
  display: flex;
  align-items: center;
  justify-content: center;
  margin-top: 1px;
}
.notif__icon--critical { background: var(--nx-danger-soft); color: var(--nx-danger); }
.notif__icon--warning { background: var(--nx-warning-soft); color: var(--nx-warning); }
.notif__label {
  display: block;
  font-size: var(--nx-text-base);
  font-weight: 500;
  text-wrap: pretty;
}
.notif__sub {
  display: block;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
  padding-top: 2px;
  text-wrap: pretty;
}
.notif__when {
  display: block;
  font-size: var(--nx-text-xs);
  color: var(--nx-text-placeholder);
  padding-top: 3px;
}
.notif__about {
  margin: 0;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
  line-height: 1.55;
  text-wrap: pretty;
}
.notif__settings { margin-top: 12px; }
</style>
