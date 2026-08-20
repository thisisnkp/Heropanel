<script setup lang="ts">
/**
 * The centred dialog: delete confirmations, the create-site wizard, DNS record
 * editing.
 *
 * Teleported to <body> so an ancestor's `overflow:hidden` or transform cannot
 * clip it — the site drawer and the scrolling main column both create those.
 * Focus is moved into the dialog on open and restored to whatever opened it on
 * close, and body scrolling is locked while it is up.
 */
import { nextTick, onBeforeUnmount, ref, watch } from "vue";

const open = defineModel<boolean>("open", { required: true });

const props = withDefaults(
  defineProps<{
    title: string;
    /** Muted line under the title. */
    description?: string;
    width?: string;
    /** Clicking the backdrop dismisses. Off for destructive flows mid-typing. */
    dismissible?: boolean;
  }>(),
  { width: "480px", dismissible: true },
);

const panel = ref<HTMLElement | null>(null);
let restoreFocus: HTMLElement | null = null;

function close() {
  open.value = false;
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === "Escape") close();
}

watch(open, async (isOpen) => {
  if (isOpen) {
    restoreFocus = document.activeElement as HTMLElement | null;
    document.addEventListener("keydown", onKeydown);
    document.body.style.overflow = "hidden";
    await nextTick();
    const focusable = panel.value?.querySelector<HTMLElement>(
      "input, textarea, select, button, [href], [tabindex]:not([tabindex='-1'])",
    );
    (focusable ?? panel.value)?.focus();
  } else {
    document.removeEventListener("keydown", onKeydown);
    document.body.style.overflow = "";
    restoreFocus?.focus();
  }
});

onBeforeUnmount(() => {
  document.removeEventListener("keydown", onKeydown);
  document.body.style.overflow = "";
});
</script>

<template>
  <Teleport to="body">
    <Transition name="nx-modal">
      <div v-if="open" class="nx-modal" @click.self="props.dismissible && close()">
        <div
          ref="panel"
          class="nx-modal__panel"
          :style="{ width }"
          role="dialog"
          aria-modal="true"
          :aria-label="title"
          tabindex="-1"
        >
          <header class="nx-modal__head">
            <div class="nx-modal__titles">
              <h2 class="nx-modal__title">{{ title }}</h2>
              <p v-if="description" class="nx-modal__desc">{{ description }}</p>
            </div>
            <button type="button" class="nx-modal__close" aria-label="Close" @click="close">
              <NxIcon name="close" size="md" />
            </button>
          </header>

          <div class="nx-modal__body"><slot /></div>

          <footer v-if="$slots.footer" class="nx-modal__foot"><slot name="footer" :close="close" /></footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.nx-modal {
  position: fixed;
  inset: 0;
  z-index: 90;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgba(27, 27, 25, 0.42);
  backdrop-filter: blur(2px);
}
.nx-modal__panel {
  max-width: 100%;
  max-height: calc(100vh - 48px);
  display: flex;
  flex-direction: column;
  background: var(--nx-surface);
  border-radius: var(--nx-radius-lg);
  box-shadow: 0 24px 64px rgba(27, 27, 25, 0.28);
  overflow: hidden;
}
.nx-modal__head {
  display: flex;
  align-items: flex-start;
  gap: 16px;
  padding: 20px 20px 12px;
}
.nx-modal__titles { flex: 1; min-width: 0; }
.nx-modal__title {
  margin: 0;
  font-size: var(--nx-text-lg);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
}
.nx-modal__desc {
  margin: 6px 0 0;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
  line-height: 1.5;
}
.nx-modal__close {
  flex: 0 0 auto;
  display: flex;
  border: 0;
  background: transparent;
  color: var(--nx-text-muted);
  cursor: pointer;
  padding: 4px;
  border-radius: var(--nx-radius-sm);
}
.nx-modal__close:hover { background: var(--nx-hover); color: var(--nx-text); }
.nx-modal__body { padding: 0 20px 20px; overflow-y: auto; }
.nx-modal__foot {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 20px;
  border-top: 1px solid var(--nx-active);
  background: var(--nx-surface-2);
}

.nx-modal-enter-active,
.nx-modal-leave-active { transition: opacity 180ms ease; }
.nx-modal-enter-active .nx-modal__panel,
.nx-modal-leave-active .nx-modal__panel { transition: transform 180ms cubic-bezier(0.16, 1, 0.3, 1), opacity 180ms ease; }
.nx-modal-enter-from,
.nx-modal-leave-to { opacity: 0; }
.nx-modal-enter-from .nx-modal__panel,
.nx-modal-leave-to .nx-modal__panel { opacity: 0; transform: translateY(8px) scale(0.985); }
</style>
