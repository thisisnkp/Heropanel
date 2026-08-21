<script setup lang="ts">
/**
 * The background job tray: bottom-right, above the toasts, collapsible.
 *
 * It shifts left when the AI panel is open rather than sliding underneath it —
 * a progress bar hidden behind a drawer is the same as no progress bar, and the
 * AI panel is precisely where jobs get started from.
 */
import { computed } from "vue";
import { useAiStore } from "@/stores/ai";
import { useJobsStore } from "@/stores/jobs";

const jobs = useJobsStore();
const ai = useAiStore();

const offset = computed(() => (ai.open ? "404px" : "24px"));
</script>

<template>
  <div v-if="jobs.jobs.length" class="jt" :style="{ right: offset }" aria-live="polite">
    <Transition name="jt">
      <div v-if="jobs.open" class="jt__panel">
        <article v-for="j in jobs.jobs" :key="j.id" class="jt__job">
          <div class="jt__head">
            <span class="jt__text">
              <span class="jt__name">{{ j.name }}</span>
              <span class="jt__target nx-mono">{{ j.target }}</span>
            </span>

            <button
              v-if="j.state === 'running'"
              type="button"
              class="jt__cancel"
              @click="jobs.cancel(j.id)"
            >
              Cancel
            </button>
            <span v-else-if="j.state === 'done'" class="jt__done">
              <NxIcon name="check-circle" size="sm" />Done
            </span>
            <span v-else class="jt__failed">
              <NxIcon name="error" size="sm" />Failed
            </span>
          </div>

          <div class="jt__bar">
            <div class="jt__fill" :class="'is-' + j.state" :style="{ width: j.pct + '%' }" />
          </div>

          <div class="jt__foot">
            <span class="jt__step">{{ j.step }}</span>
            <span class="jt__pct nx-mono">{{ j.pct }}%</span>
          </div>
        </article>
      </div>
    </Transition>

    <button type="button" class="jt__toggle" :aria-expanded="jobs.open" @click="jobs.toggle()">
      <span class="jt__dot" aria-hidden="true" />
      {{ jobs.label }}
      <NxIcon :name="jobs.open ? 'expand-more' : 'expand-less'" size="sm" />
    </button>
  </div>
</template>

<style scoped>
.jt {
  position: fixed;
  bottom: 24px;
  z-index: 58;
  width: 340px;
  max-width: calc(100vw - 48px);
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: stretch;
  transition: right 240ms cubic-bezier(0.16, 1, 0.3, 1);
}
/* Clear of the mobile tab bar; otherwise the toggle sits under it and the last
   job's progress is the part that gets covered. */
@media (max-width: 900px) {
  .jt { bottom: calc(84px + env(safe-area-inset-bottom)); right: 16px !important; }
}
.jt__panel {
  background: var(--nx-surface);
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-lg);
  box-shadow: 0 12px 32px rgba(27, 27, 25, 0.14);
  overflow: hidden;
  max-height: 50vh;
  overflow-y: auto;
}
.jt__job { padding: 12px 16px; border-bottom: 1px solid var(--nx-active); }
.jt__job:last-child { border-bottom: 0; }
.jt__head { display: flex; align-items: center; gap: 8px; }
.jt__text { flex: 1; min-width: 0; }
.jt__name {
  display: block;
  font-size: var(--nx-text-base);
  font-weight: 500;
  color: var(--nx-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.jt__target {
  display: block;
  font-size: var(--nx-text-xs);
  color: var(--nx-text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.jt__cancel {
  border: 1px solid var(--nx-border);
  background: var(--nx-surface);
  border-radius: var(--nx-radius-sm);
  padding: 4px 8px;
  font-size: var(--nx-text-xs);
  font-family: inherit;
  cursor: pointer;
  color: var(--nx-text-2);
  white-space: nowrap;
}
.jt__cancel:hover { background: var(--nx-hover); }
.jt__done,
.jt__failed {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: var(--nx-text-xs);
  font-weight: 500;
  white-space: nowrap;
}
.jt__done { color: var(--nx-success); }
.jt__failed { color: var(--nx-danger); }

.jt__bar {
  height: 4px;
  border-radius: var(--nx-radius-pill);
  background: var(--nx-hover);
  overflow: hidden;
  margin: 8px 0 4px;
}
.jt__fill { height: 100%; border-radius: var(--nx-radius-pill); transition: width 400ms ease; }
.jt__fill.is-running { background: var(--nx-primary); }
.jt__fill.is-done { background: var(--nx-success); }
.jt__fill.is-failed { background: var(--nx-danger); }

.jt__foot { display: flex; align-items: baseline; gap: 8px; }
.jt__step {
  flex: 1;
  min-width: 0;
  font-size: var(--nx-text-xs);
  color: var(--nx-text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.jt__pct { font-size: var(--nx-text-xs); color: var(--nx-text-muted); }

.jt__toggle {
  align-self: flex-end;
  display: flex;
  align-items: center;
  gap: 8px;
  background: var(--nx-text);
  color: var(--nx-primary-on);
  border: 0;
  border-radius: var(--nx-radius-pill);
  padding: 8px 16px;
  font-size: var(--nx-text-base);
  font-family: inherit;
  font-weight: 500;
  cursor: pointer;
  box-shadow: 0 4px 16px rgba(27, 27, 25, 0.18);
}
.jt__toggle:hover { background: var(--nx-dark-2); }
.jt__dot {
  width: 6px;
  height: 6px;
  border-radius: var(--nx-radius-full);
  background: var(--nx-gold-400);
}

.jt-enter-active,
.jt-leave-active { transition: opacity 200ms cubic-bezier(0.16, 1, 0.3, 1), transform 200ms cubic-bezier(0.16, 1, 0.3, 1); }
.jt-enter-from,
.jt-leave-to { opacity: 0; transform: translateY(8px) scale(0.985); }
</style>
