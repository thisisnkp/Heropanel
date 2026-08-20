<script setup lang="ts">
/**
 * A labelled form control.
 *
 * The label is a real <label for>, generated id and all, so clicking it focuses
 * the input and a screen reader announces the two together. Hint and error text
 * are wired through aria-describedby for the same reason — the design drew them
 * as loose lines of grey text, which is invisible to anyone not looking at it.
 */
import { useId } from "vue";

defineProps<{
  label?: string;
  /** Muted line under the control. */
  hint?: string;
  /** Replaces the hint and marks the control invalid. */
  error?: string;
  required?: boolean;
}>();

const id = useId();
const describedBy = id + "-desc";
</script>

<template>
  <div class="nx-field">
    <label v-if="label" class="nx-field__label" :for="id">
      {{ label }}
      <span v-if="required" class="nx-field__req" aria-hidden="true">*</span>
    </label>

    <slot :id="id" :described-by="hint || error ? describedBy : undefined" :invalid="Boolean(error)" />

    <p v-if="error" :id="describedBy" class="nx-field__error">{{ error }}</p>
    <p v-else-if="hint" :id="describedBy" class="nx-field__hint">{{ hint }}</p>
  </div>
</template>

<style scoped>
.nx-field { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
.nx-field__label {
  font-size: var(--nx-text-sm);
  font-weight: 500;
  color: var(--nx-text-2);
}
.nx-field__req { color: var(--nx-danger); }
.nx-field__hint {
  margin: 0;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
}
.nx-field__error {
  margin: 0;
  font-size: var(--nx-text-sm);
  color: var(--nx-danger);
}
</style>
