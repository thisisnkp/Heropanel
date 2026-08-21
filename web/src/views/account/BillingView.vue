<script setup lang="ts">
/** License & billing — what the plan includes and how much of it is used. */
import { PLAN, USAGE } from "@/data/system";
import { useUiStore } from "@/stores/ui";

const ui = useUiStore();
</script>

<template>
  <div class="nx-view">
    <NxPageHeader title="License &amp; billing" :subtitle="PLAN.name + ' plan · ' + PLAN.renews" />

    <div class="nx-grid bill__split">
      <NxCard title="Usage this cycle">
        <div class="nx-stack nx-stack--lg">
          <NxMeter
            v-for="u in USAGE"
            :key="u.label"
            :value="u.pct"
            :label="u.label"
            :value-text="u.text"
            height="6px"
          />
        </div>
      </NxCard>

      <NxCard :title="PLAN.name" class="bill__plan">
        <div class="bill__price">
          {{ PLAN.price }}<span class="bill__period">{{ PLAN.period }}</span>
        </div>
        <p class="bill__includes">{{ PLAN.includes }}</p>
        <div class="bill__spacer" />
        <NxButton class="bill__upgrade" @click="ui.toast('Upgrade is not wired up yet.', 'info')">
          Upgrade to Cloud
        </NxButton>
      </NxCard>
    </div>
  </div>
</template>

<style scoped>
.bill__split { grid-template-columns: minmax(0, 1.3fr) minmax(0, 1fr); }
@media (max-width: 1100px) {
  .bill__split { grid-template-columns: minmax(0, 1fr); }
}
.bill__plan :deep(.nx-card__body) { display: flex; flex-direction: column; height: 100%; }
.bill__price {
  font-size: var(--nx-text-2xl);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
  padding-bottom: 4px;
}
.bill__period {
  font-size: var(--nx-text-md);
  font-weight: 400;
  color: var(--nx-text-muted);
}
.bill__includes {
  margin: 12px 0 0;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
  line-height: 1.6;
}
.bill__spacer { flex: 1; min-height: 20px; }
.bill__upgrade {
  background: var(--nx-text);
  color: var(--nx-primary-on);
  border-color: var(--nx-text);
  padding: 12px 16px;
}
.bill__upgrade:hover:not(:disabled) { background: var(--nx-dark-2); color: var(--nx-primary-on); }
</style>
