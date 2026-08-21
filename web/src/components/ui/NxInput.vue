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
    <!-- A fixed, unselectable segment inside the control rather than a separate
         read-only input: the prefix is part of the value the server will store,
         not a second field. Tab moves past it, and copying the control copies
         the whole name. -->
    <span v-if="$slots.prefix" class="nx-input__prefix nx-mono"><slot name="prefix" /></span>
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
.nx-input__prefix {
  flex: 0 0 auto;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
  user-select: none;
  white-space: nowrap;
  /* A hairline rather than a filled block: it reads as one control that happens
     to start with a fixed part, not as two inputs jammed together. */
  border-right: 1px solid var(--nx-border);
  padding-right: 8px;
  margin: -8px 0;
  padding-top: 8px;
  padding-bottom: 8px;
}
.nx-input__el {
  flex: 1;
  min-width: 0;
  border: 0;
  outline: 0;
  background: transparent;
  font-size: var(--nx-text-base);
  color: var(--nx-text);
  /* Fixed, so the control is the same height whichever face it renders in.
     Inter and JetBrains Mono have different default line boxes at 13px, which
     made a `mono` input a pixel taller than a plain one beside it — and made the
     password field jump the moment revealing it switched the font. */
  line-height: 15px;
}
.nx-input__el.is-mono { font-family: "JetBrains Mono", ui-monospace, monospace; }
.nx-input__el:disabled { cursor: not-allowed; color: var(--nx-text-muted); }
</style>
