<script setup lang="ts" generic="T">
import { computed, ref } from "vue";
import {
  FlexRender,
  getCoreRowModel,
  getSortedRowModel,
  type SortingState,
  useVueTable,
  type ColumnDef,
} from "@tanstack/vue-table";

/* AXIOM DataTable：TanStack Table 核心 + 设计令牌样式
   - 语义化 <table>、sticky 表头、行点击、空态 */
const props = withDefaults(
  defineProps<{
    data: T[];
    rowKey?: (row: T) => string;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    columns: ColumnDef<T, any>[];
    rowClick?: ((row: T) => void) | null;
    emptyTitle?: string;
    emptyHint?: string;
    loading?: boolean;
  }>(),
  { rowClick: null, emptyTitle: "暂无数据", emptyHint: "" },
);

const sorting = ref<SortingState>([]);
const table = useVueTable({
  get data() {
    return props.data;
  },
  get columns() {
    return props.columns;
  },
  getRowId: (row, index) => props.rowKey?.(row) ?? String(index),
  getCoreRowModel: getCoreRowModel(),
  getSortedRowModel: getSortedRowModel(),
  state: { get sorting() { return sorting.value; } },
  onSortingChange: updater => { sorting.value = typeof updater === "function" ? updater(sorting.value) : updater; },
});

const rows = computed(() => table.getRowModel().rows);
</script>

<template>
  <div class="dt-wrap" :aria-busy="loading">
    <div v-if="loading" class="dt-loading" role="status">正在加载数据…</div>
    <table class="dt">
      <thead>
        <tr v-for="hg in table.getHeaderGroups()" :key="hg.id">
          <th
            v-for="h in hg.headers"
            :key="h.id"
            :aria-sort="h.column.getIsSorted() === 'asc' ? 'ascending' : h.column.getIsSorted() === 'desc' ? 'descending' : undefined"
            :style="{ width: h.column.columnDef.size ? h.column.columnDef.size + 'px' : undefined }"
          >
            <button v-if="!h.isPlaceholder && h.column.getCanSort()" class="sort-button" @click="h.column.toggleSorting()"><FlexRender :render="h.column.columnDef.header" :props="h.getContext()"/><span aria-hidden="true">{{h.column.getIsSorted() === 'asc' ? '↑' : h.column.getIsSorted() === 'desc' ? '↓' : '↕'}}</span></button><FlexRender v-else-if="!h.isPlaceholder" :render="h.column.columnDef.header" :props="h.getContext()" />
          </th>
        </tr>
      </thead>
      <tbody v-if="rows.length">
        <tr
          v-for="r in rows"
          :key="r.id"
          :class="{ clickable: !!rowClick }"
          :tabindex="rowClick ? 0 : undefined"
          @keydown.enter.self="rowClick?.(r.original)"
          @keydown.space.self.prevent="rowClick?.(r.original)"
          @click="rowClick?.(r.original)"
        >
          <td v-for="c in r.getVisibleCells()" :key="c.id">
            <FlexRender :render="c.column.columnDef.cell" :props="c.getContext()" />
          </td>
        </tr>
      </tbody>
    </table>
    <div v-if="!rows.length && !loading" class="dt-empty">
      <p class="empty-title">{{ emptyTitle }}</p>
      <p v-if="emptyHint" class="empty-hint">{{ emptyHint }}</p>
      <slot name="empty" />
    </div>
  </div>
</template>

<style scoped>
.dt-wrap {
  border: 1px solid var(--border-default);
  border-radius: var(--radius-lg);
  background: var(--surface-primary);
  overflow: auto;
}
.dt {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--font-size-md);
}
.dt thead th {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--surface-secondary);
  border-bottom: 1px solid var(--border-default);
  color: var(--text-tertiary);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-normal);
  text-align: left;
  padding: var(--space-2-5) var(--space-4);
  white-space: nowrap;
}
.dt tbody td {
  border-bottom: 1px solid var(--border-subtle);
  padding: var(--space-3) var(--space-5);
  height: 54px;
  vertical-align: middle;
  white-space: nowrap;
}
.dt tbody tr:last-child td {
  border-bottom: none;
}
.dt tbody tr.clickable {
  cursor: pointer;
  transition: background-color var(--duration-fast) var(--ease-ui);
}
.dt tbody tr.clickable:hover {
  background: var(--surface-hover);
}
.dt-empty {
  padding: var(--space-8);
  text-align: center;
}
.empty-title {
  margin: 0;
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  color: var(--text-secondary);
}
.empty-hint {
  margin: var(--space-1) 0 0;
  font-size: var(--font-size-xs);
  color: var(--text-tertiary);
}
.dt-loading{padding:var(--space-3) var(--space-5);font-size:var(--font-size-xs);color:var(--text-tertiary);border-bottom:1px solid var(--border-default)}
.sort-button{display:inline-flex;gap:var(--space-2);align-items:center;text-align:left;font:inherit;color:inherit}.sort-button span{color:var(--text-tertiary);font-size:var(--font-size-2xs)}.sort-button:hover{color:var(--text-primary)}
</style>
