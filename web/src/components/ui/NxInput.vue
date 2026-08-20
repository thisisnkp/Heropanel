<script setup lang="ts">
/**
 * Text input with the design's border, focus ring and monospace variant.
 *
 * `mono` exists because the panel asks people to type exact strings — domains,
 * record values, confirmation phrases — and a proportional font makes an
 * accidental double space or a confusable character invisible.
 */
// Attributes land on the <input>, not on the wrapper that draws the border —
// otherwise aria-describedby from NxField would attach to a <div> and never
// reach the control it describes.
defineOptions({ inheritAttrs: false });

const model = defineModel<string>({ default: "" });

withDefaults(
  defineProps<{
    type?: "text" | "password" | "email" | "number" | "search";
    placeholder?: string;
    disabled?: boolean;
    readonly?: boolean;
    mono?: boolean;
    invalid?: boolean;
    /** Leading glyph, as the search fields use. */
    icon?: string;
  }>(),
  { type: "text", disabled: false, readonly: false, mono: false, invalid: false },
);
</script>

<template>
  <div :class="['nx-input', { 'is-invalid': invalid, 'is-disabled': disabled }]">
    <NxIcon v-if="icon" :name="icon" size="sm" class="nx-input__icon" />
    <input
      v-model="model"
      :type="type"
      :placeholder="placeholder"
      :disabled="disabled"
      :readonly="readonly"
      :aria-invalid="invalid || undefined"
      :class="['nx-input__el', { 'is-mono': mono }]"
      v-bind="$attrs"
    />
    <slot name="suffix" />
  </div>
</template>

<style scoped>
.nx-input {
  display: flex;
  align-items: center;
  gap: 8px;
  background: var(--nx-surface);
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-md);
  padding: 8px 12px;
  min-width: 0;
}
.nx-input:focus-within { border-color: var(--nx-primary); box-shadow: 0 0 0 3px var(--nx-primary-soft); }
.nx-input.is-invalid { border-color: var(--nx-danger); }
.nx-input.is-invalid:focus-within { box-shadow: 0 0 0 3px var(--nx-danger-soft); }
.nx-input.is-disabled { background: var(--nx-hover); }
.nx-input__icon { color: var(--nx-text-muted); }
.nx-input__el {
  flex: 1;
  min-width: 0;
  border: 0;
  outline: 0;
  background: transparent;
  font-size: var(--nx-text-base);
  color: var(--nx-text);
}
.nx-input__el.is-mono { font-family: "JetBrains Mono", ui-monospace, monospace; }
.nx-input__el:disabled { cursor: not-allowed; color: var(--nx-text-muted); }
</style>
