<script setup lang="ts">
/**
 * A native <select> wearing the design's border.
 *
 * Native rather than a custom listbox: the design's dropdowns are plain value
 * pickers with no icons or multi-line rows, and the native control brings
 * keyboard type-ahead, mobile wheel pickers and screen-reader support that a
 * div-based rebuild would have to re-earn.
 */
export interface NxOption {
  readonly value: string;
  readonly label: string;
}

const model = defineModel<string>({ default: "" });

defineOptions({ inheritAttrs: false });

withDefaults(defineProps<{ options: readonly NxOption[]; disabled?: boolean; invalid?: boolean }>(), {
  disabled: false,
  invalid: false,
});
</script>

<template>
  <div :class="['nx-select', { 'is-invalid': invalid, 'is-disabled': disabled }]">
    <select
      v-model="model"
      class="nx-select__el"
      :disabled="disabled"
      :aria-invalid="invalid || undefined"
      v-bind="$attrs"
    >
      <option v-for="o in options" :key="o.value" :value="o.value">{{ o.label }}</option>
    </select>
    <NxIcon name="expand-more" size="sm" class="nx-select__caret" />
  </div>
</template>

<style scoped>
.nx-select {
  position: relative;
  display: flex;
  align-items: center;
  background: var(--nx-surface);
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-md);
  min-width: 0;
}
.nx-select:focus-within { border-color: var(--nx-primary); box-shadow: 0 0 0 3px var(--nx-primary-soft); }
.nx-select.is-invalid { border-color: var(--nx-danger); }
.nx-select.is-disabled { background: var(--nx-hover); }
.nx-select__el {
  flex: 1;
  min-width: 0;
  appearance: none;
  border: 0;
  outline: 0;
  background: transparent;
  font-size: var(--nx-text-base);
  color: var(--nx-text);
  padding: 8px 32px 8px 12px;
  cursor: pointer;
}
.nx-select__el:disabled { cursor: not-allowed; color: var(--nx-text-muted); }
.nx-select__caret {
  position: absolute;
  right: 10px;
  color: var(--nx-text-muted);
  pointer-events: none;
}
</style>
