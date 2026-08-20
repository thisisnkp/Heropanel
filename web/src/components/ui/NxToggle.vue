<script setup lang="ts">
/**
 * The switch the security and settings screens are built from.
 *
 * A real <input type="checkbox"> underneath, visually hidden: it brings keyboard
 * activation, form participation and the checked state for assistive tech for
 * free, none of which a styled <div> with a click handler has.
 */
const model = defineModel<boolean>({ required: true });

withDefaults(defineProps<{ label?: string; description?: string; disabled?: boolean }>(), {
  disabled: false,
});
</script>

<template>
  <label :class="['nx-toggle', { 'is-disabled': disabled }]">
    <input v-model="model" type="checkbox" class="nx-toggle__input" :disabled="disabled" />
    <span class="nx-toggle__track" aria-hidden="true"><span class="nx-toggle__thumb" /></span>
    <span v-if="label || description" class="nx-toggle__text">
      <span v-if="label" class="nx-toggle__label">{{ label }}</span>
      <span v-if="description" class="nx-toggle__desc">{{ description }}</span>
    </span>
  </label>
</template>

<style scoped>
.nx-toggle {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  cursor: pointer;
}
.nx-toggle.is-disabled { cursor: not-allowed; opacity: 0.6; }
.nx-toggle__input {
  position: absolute;
  opacity: 0;
  width: 0;
  height: 0;
}
.nx-toggle__track {
  flex: 0 0 auto;
  width: 34px;
  height: 20px;
  border-radius: var(--nx-radius-pill);
  background: var(--nx-border-strong);
  transition: background 140ms ease;
  padding: 2px;
  margin-top: 1px;
}
.nx-toggle__thumb {
  display: block;
  width: 16px;
  height: 16px;
  border-radius: var(--nx-radius-full);
  background: var(--nx-surface);
  transition: transform 140ms ease;
}
.nx-toggle__input:checked + .nx-toggle__track { background: var(--nx-primary); }
.nx-toggle__input:checked + .nx-toggle__track .nx-toggle__thumb { transform: translateX(14px); }
.nx-toggle__input:focus-visible + .nx-toggle__track {
  outline: 2px solid var(--nx-focus-ring);
  outline-offset: 2px;
}
.nx-toggle__text { display: flex; flex-direction: column; gap: 2px; }
.nx-toggle__label { font-size: var(--nx-text-base); color: var(--nx-text); }
.nx-toggle__desc { font-size: var(--nx-text-sm); color: var(--nx-text-muted); }
</style>
