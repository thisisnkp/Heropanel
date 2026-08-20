<script setup lang="ts">
/**
 * The anchored dropdown: "Add Website", the row overflow menus, the DNS record
 * type picker.
 *
 * Closes on outside click and on Escape, and returns focus to the trigger when
 * it does — without that, dismissing a menu with the keyboard drops focus onto
 * <body> and the next Tab starts from the top of the page.
 */
import { onBeforeUnmount, ref, watch } from "vue";

const open = defineModel<boolean>("open", { default: false });

withDefaults(defineProps<{ align?: "start" | "end"; width?: string }>(), {
  align: "end",
  width: "268px",
});

const root = ref<HTMLElement | null>(null);
const triggerEl = ref<HTMLElement | null>(null);

function onDocumentPointerDown(e: PointerEvent) {
  if (!root.value) return;
  if (!root.value.contains(e.target as Node)) open.value = false;
}

function onKeydown(e: KeyboardEvent) {
  if (e.key !== "Escape" || !open.value) return;
  open.value = false;
  triggerEl.value?.querySelector<HTMLElement>("button, [href]")?.focus();
}

watch(open, (isOpen) => {
  if (isOpen) {
    document.addEventListener("pointerdown", onDocumentPointerDown);
    document.addEventListener("keydown", onKeydown);
  } else {
    document.removeEventListener("pointerdown", onDocumentPointerDown);
    document.removeEventListener("keydown", onKeydown);
  }
});

onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", onDocumentPointerDown);
  document.removeEventListener("keydown", onKeydown);
});
</script>

<template>
  <div ref="root" class="nx-menu">
    <div ref="triggerEl" class="nx-menu__trigger">
      <slot name="trigger" :open="open" :toggle="() => (open = !open)" />
    </div>

    <Transition name="nx-menu">
      <div
        v-if="open"
        class="nx-menu__panel"
        :class="'nx-menu__panel--' + align"
        :style="{ width }"
        role="menu"
        @click="open = false"
      >
        <slot />
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.nx-menu { position: relative; }
.nx-menu__panel {
  position: absolute;
  top: calc(100% + 8px);
  z-index: 20;
  background: var(--nx-surface);
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-lg);
  box-shadow: 0 12px 32px rgba(27, 27, 25, 0.13);
  padding: 8px;
  max-height: min(60vh, 420px);
  overflow-y: auto;
  transform-origin: top;
}
.nx-menu__panel--end { right: 0; }
.nx-menu__panel--start { left: 0; }

.nx-menu-enter-active,
.nx-menu-leave-active { transition: opacity 150ms cubic-bezier(0.16, 1, 0.3, 1), transform 150ms cubic-bezier(0.16, 1, 0.3, 1); }
.nx-menu-enter-from,
.nx-menu-leave-to { opacity: 0; transform: translateY(-6px); }
</style>
