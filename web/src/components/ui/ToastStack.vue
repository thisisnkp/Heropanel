<script setup lang="ts">
/**
 * Transient messages, bottom-right on desktop and above the tab bar on mobile.
 *
 * role="status" with aria-live="polite" rather than an alert: these announce
 * completions, and interrupting whatever a screen reader is mid-sentence on to
 * say "saved" is worse than waiting for a pause.
 */
import { useUiStore } from "@/stores/ui";

const ui = useUiStore();
</script>

<template>
  <Teleport to="body">
    <div class="nx-toasts" role="status" aria-live="polite">
      <TransitionGroup name="nx-toast">
        <div v-for="t in ui.toasts" :key="t.id" :class="['nx-toast', `nx-toast--${t.tone}`]">
          <span class="nx-toast__text">{{ t.text }}</span>
          <button type="button" class="nx-toast__close" aria-label="Dismiss" @click="ui.dismiss(t.id)">
            <NxIcon name="close" size="sm" />
          </button>
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
</template>

<style scoped>
.nx-toasts {
  position: fixed;
  right: 16px;
  bottom: calc(16px + env(safe-area-inset-bottom));
  z-index: 80;
  display: flex;
  flex-direction: column;
  gap: 8px;
  pointer-events: none;
}
@media (max-width: 900px) {
  .nx-toasts {
    left: 12px;
    right: 12px;
    bottom: calc(74px + env(safe-area-inset-bottom));
  }
}
.nx-toast {
  pointer-events: auto;
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 240px;
  max-width: 380px;
  padding: 10px 10px 10px 14px;
  border-radius: var(--nx-radius-md);
  background: var(--nx-text);
  color: var(--nx-primary-on);
  font-size: var(--nx-text-base);
  box-shadow: 0 8px 24px rgba(27, 27, 25, 0.22);
}
.nx-toast--success { background: var(--nx-success); }
.nx-toast--danger { background: var(--nx-danger); }
.nx-toast__text { flex: 1; }
.nx-toast__close {
  display: flex;
  border: 0;
  background: transparent;
  color: inherit;
  opacity: 0.8;
  cursor: pointer;
  padding: 4px;
  border-radius: var(--nx-radius-sm);
}
.nx-toast__close:hover { opacity: 1; background: rgba(255, 255, 255, 0.14); }

.nx-toast-enter-active,
.nx-toast-leave-active { transition: opacity 200ms ease, transform 200ms ease; }
.nx-toast-enter-from,
.nx-toast-leave-to { opacity: 0; transform: translateY(10px); }
</style>
