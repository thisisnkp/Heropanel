<script setup lang="ts" generic="T extends { key: string | number; label: string; sub?: string }">
/**
 * The context switcher that sits at the top of a scoped sidebar: the website
 * switcher inside a site, the domain switcher inside a zone.
 *
 * Not NxMenu. That component dismisses on any click inside its panel, which is
 * right for a menu of commands and wrong here — this panel contains a search
 * field, and a switcher that closes when you click into its own filter box is
 * unusable. The list is filtered rather than scrolled because these sidebars
 * appear on installations with a hundred sites, where "scroll until you see it"
 * is not a way to find one.
 *
 * The trigger is a slot: a site shows its stack and deploy mode, a domain shows
 * its certificate state, and flattening both into one prop shape would mean
 * passing pre-rendered markup as strings.
 */
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";

const props = withDefaults(
  defineProps<{
    items: readonly T[];
    current: string | number | null;
    placeholder?: string;
    emptyText?: string;
    /** Domains read as identifiers; sites read as names. */
    mono?: boolean;
    label?: string;
  }>(),
  { placeholder: "Search", emptyText: "Nothing matches.", mono: false, label: "Switch" },
);

const emit = defineEmits<{ (e: "pick", key: string | number): void }>();

const open = ref(false);
const query = ref("");
const root = ref<HTMLElement | null>(null);
const trigger = ref<HTMLButtonElement | null>(null);
const search = ref<HTMLInputElement | null>(null);

const matches = computed(() => {
  const q = query.value.trim().toLowerCase();
  if (!q) return props.items;
  return props.items.filter((i) => i.label.toLowerCase().includes(q));
});

function toggle() {
  open.value = !open.value;
}

function pick(key: string | number) {
  emit("pick", key);
  open.value = false;
}

function onPointerDown(e: PointerEvent) {
  if (root.value && !root.value.contains(e.target as Node)) open.value = false;
}

function onKeydown(e: KeyboardEvent) {
  if (e.key !== "Escape") return;
  open.value = false;
  // Escape has to hand focus back, or the next Tab restarts from <body> at the
  // top of the page rather than from the sidebar you were just in.
  trigger.value?.focus();
}

watch(open, async (isOpen) => {
  // The query is cleared on every open: a filter left over from last time hides
  // the site you came here to pick, and looks like the list is short.
  query.value = "";
  if (isOpen) {
    document.addEventListener("pointerdown", onPointerDown);
    document.addEventListener("keydown", onKeydown);
    await nextTick();
    search.value?.focus();
  } else {
    document.removeEventListener("pointerdown", onPointerDown);
    document.removeEventListener("keydown", onKeydown);
  }
});

onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", onPointerDown);
  document.removeEventListener("keydown", onKeydown);
});
</script>

<template>
  <div ref="root" class="nx-sw">
    <button
      ref="trigger"
      type="button"
      class="nx-sw__trigger"
      :aria-expanded="open"
      :aria-label="label"
      @click="toggle"
    >
      <span class="nx-sw__trigger-body">
        <slot name="trigger" />
      </span>
      <NxIcon :name="open ? 'expand-less' : 'expand-more'" size="md" class="nx-sw__caret" />
    </button>

    <Transition name="nx-sw">
      <div v-if="open" class="nx-sw__panel">
        <div class="nx-sw__search">
          <NxIcon name="search" size="sm" class="nx-sw__search-icon" />
          <input
            ref="search"
            v-model="query"
            type="text"
            :placeholder="placeholder"
            :aria-label="placeholder"
          />
        </div>

        <div class="nx-sw__list nxhide">
          <button
            v-for="item in matches"
            :key="item.key"
            type="button"
            class="nx-sw__item"
            :class="{ 'is-current': item.key === current, 'is-mono': mono }"
            @click="pick(item.key)"
          >
            <span class="nx-sw__item-text">
              <span class="nx-sw__item-name">{{ item.label }}</span>
              <span v-if="item.sub" class="nx-sw__item-sub">{{ item.sub }}</span>
            </span>
            <NxIcon v-if="item.key === current" name="check" size="sm" class="nx-sw__tick" />
          </button>

          <p v-if="!matches.length" class="nx-sw__empty">{{ emptyText }}</p>
        </div>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.nx-sw { position: relative; padding-bottom: 16px; }
.nx-sw__trigger {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  text-align: left;
  border: 1px solid var(--nx-border);
  background: var(--nx-surface);
  border-radius: var(--nx-radius-md);
  padding: 8px 12px;
  cursor: pointer;
  font-family: inherit;
  color: var(--nx-text);
}
.nx-sw__trigger:hover { background: var(--nx-surface-2); border-color: var(--nx-border-strong); }
.nx-sw__trigger-body { flex: 1; min-width: 0; }
.nx-sw__caret { color: var(--nx-text-muted); }

.nx-sw__panel {
  position: absolute;
  left: 0;
  right: 0;
  top: calc(100% - 8px);
  background: var(--nx-surface);
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-md);
  box-shadow: 0 12px 32px rgba(27, 27, 25, 0.14);
  padding: 8px;
  z-index: 30;
  transform-origin: top;
}
.nx-sw__search {
  display: flex;
  align-items: center;
  gap: 8px;
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-md);
  padding: 8px;
  margin-bottom: 6px;
}
.nx-sw__search-icon { color: var(--nx-text-placeholder); }
.nx-sw__search input {
  border: 0;
  outline: 0;
  font-size: var(--nx-text-base);
  font-family: inherit;
  background: transparent;
  width: 100%;
  min-width: 0;
  color: var(--nx-text);
}
.nx-sw__list { max-height: 240px; overflow-y: auto; }
.nx-sw__item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  text-align: left;
  border: 0;
  background: transparent;
  cursor: pointer;
  padding: 8px;
  border-radius: var(--nx-radius-md);
  font-family: inherit;
  color: var(--nx-text);
}
.nx-sw__item:hover { background: var(--nx-hover); }
.nx-sw__item.is-current { background: var(--nx-primary-soft); }
.nx-sw__item-text { flex: 1; min-width: 0; }
.nx-sw__item-name {
  display: block;
  font-size: var(--nx-text-base);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.nx-sw__item.is-current .nx-sw__item-name { font-weight: 600; }
.nx-sw__item.is-mono .nx-sw__item-name { font-family: "JetBrains Mono", ui-monospace, monospace; }
.nx-sw__item-sub {
  display: block;
  font-size: var(--nx-text-xs);
  color: var(--nx-text-muted);
  padding-top: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.nx-sw__tick { color: var(--nx-primary); }
.nx-sw__empty {
  margin: 0;
  padding: 12px 8px;
  font-size: var(--nx-text-base);
  color: var(--nx-text-placeholder);
}

.nx-sw-enter-active,
.nx-sw-leave-active { transition: opacity 150ms cubic-bezier(0.16, 1, 0.3, 1), transform 150ms cubic-bezier(0.16, 1, 0.3, 1); }
.nx-sw-enter-from,
.nx-sw-leave-to { opacity: 0; transform: translateY(-6px); }
</style>
