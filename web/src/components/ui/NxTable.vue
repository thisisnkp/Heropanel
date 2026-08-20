<script setup lang="ts" generic="T">
/**
 * The list table the design repeats on ~20 screens.
 *
 * CSS grid rather than <table>: every one of those screens uses a fixed column
 * template with an actions cell pinned right, which grid expresses directly and
 * a table only fakes. The semantics are kept with role="table"/"row"/"cell" so
 * it still reads correctly to a screen reader.
 */
export interface NxColumn {
  readonly key: string;
  readonly label: string;
  /** A grid-template-columns track, e.g. "2fr" or "150px". */
  readonly width: string;
  readonly align?: "start" | "end";
}

const props = withDefaults(
  defineProps<{
    columns: readonly NxColumn[];
    rows: readonly T[];
    /** Used as the :key; falls back to the index when absent. */
    rowKey?: (row: T, index: number) => string | number;
    /** Shown in place of the rows when there are none. */
    emptyText?: string;
  }>(),
  { emptyText: "Nothing here yet." },
);

const template = () => props.columns.map((c) => c.width).join(" ");
</script>

<template>
  <div class="nx-table" role="table">
    <div class="nx-table__head" role="row" :style="{ gridTemplateColumns: template() }">
      <div
        v-for="c in columns"
        :key="c.key"
        role="columnheader"
        :style="{ textAlign: c.align === 'end' ? 'right' : 'left' }"
      >
        {{ c.label }}
      </div>
    </div>

    <div v-if="!rows.length" class="nx-table__empty">{{ emptyText }}</div>

    <div
      v-for="(row, i) in rows"
      :key="rowKey ? rowKey(row, i) : i"
      class="nx-table__row"
      role="row"
      :style="{ gridTemplateColumns: template() }"
    >
      <slot :row="row" :index="i" />
    </div>
  </div>
</template>

<style scoped>
.nx-table { width: 100%; }
.nx-table__head,
.nx-table__row {
  display: grid;
  gap: 16px;
  align-items: center;
  padding: 12px 16px;
}
.nx-table__head {
  border-bottom: 1px solid var(--nx-hover);
  font-size: var(--nx-text-xs);
  letter-spacing: var(--nx-ls-caps);
  color: var(--nx-text-placeholder);
  font-weight: 600;
  text-transform: uppercase;
}
.nx-table__row {
  border-bottom: 1px solid var(--nx-hover);
  font-size: var(--nx-text-base);
}
.nx-table__row:last-child { border-bottom: 0; }
.nx-table__row:hover { background: var(--nx-surface-2); }
.nx-table__empty {
  padding: 28px 16px;
  text-align: center;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
}
</style>
