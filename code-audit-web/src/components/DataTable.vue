<script setup lang="ts" generic="T">
import { computed, ref } from "vue";
import {
  FlexRender,
  getCoreRowModel,
  getSortedRowModel,
  type SortingState,
  type ColumnSizingState,
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
    unavailable?: boolean;
    manualSorting?: boolean;
    sorting?: SortingState;
  }>(),
  { rowClick: null, emptyTitle: "暂无数据", emptyHint: "" },
);
const emit = defineEmits<{ 'update:sorting': [sorting: SortingState] }>();

const sorting = ref<SortingState>([]);
const columnSizing = ref<ColumnSizingState>({});
const selectedId = ref<string | null>(null);
const body = ref<HTMLTableSectionElement | null>(null);
const table = useVueTable({
  get data() {
    return props.data;
  },
  get columns() {
    return props.columns;
  },
  getRowId: (row, index) => props.rowKey?.(row) ?? String((row as {id?: unknown})?.id ?? index),
  defaultColumn: { size: 180, minSize: 48, maxSize: 1000 },
  columnResizeMode: 'onChange',
  getCoreRowModel: getCoreRowModel(),
  getSortedRowModel: getSortedRowModel(),
  get manualSorting() { return props.manualSorting; },
  get enableMultiSort() { return !props.manualSorting; },
  state: { get sorting() { return props.sorting ?? sorting.value; }, get columnSizing() { return columnSizing.value; } },
  onSortingChange: updater => {
    const next = typeof updater === "function" ? updater(props.sorting ?? sorting.value) : updater;
    if (props.manualSorting) emit('update:sorting', next); else sorting.value = next;
  },
  onColumnSizingChange: updater => { columnSizing.value = typeof updater === 'function' ? updater(columnSizing.value) : updater; },
});

const rows = computed(() => table.getRowModel().rows);
const tabRow = computed(() => rows.value.some(row => row.id === selectedId.value) ? selectedId.value : rows.value[0]?.id);
function onRowKey(event: KeyboardEvent, index: number): void {
  if (!props.rowClick || event.target !== event.currentTarget || event.altKey || event.ctrlKey || event.metaKey) return;
  const movement: Record<string, number> = { ArrowDown: index + 1, j: index + 1, ArrowUp: index - 1, k: index - 1, Home: 0, End: rows.value.length - 1 };
  if (event.key in movement) {
    event.preventDefault();
    const target = Math.max(0, Math.min(rows.value.length - 1, movement[event.key]!));
    const row = rows.value[target];
    if (row) { selectedId.value = row.id; body.value?.rows[target]?.focus(); }
  } else if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault(); props.rowClick(rows.value[index]!.original);
  }
}
function openRow(event: MouseEvent, id: string, row: T): void {
  if ((event.target as Element).closest('button,a,input,textarea,select,[role="switch"],[role="combobox"]')) return;
  selectedId.value = id; props.rowClick?.(row);
}
function resizeKey(event: KeyboardEvent, id: string, size: number): void {
  if (!['ArrowLeft', 'ArrowRight', 'Home'].includes(event.key)) return;
  event.preventDefault();
  if (event.key === 'Home') table.getColumn(id)?.resetSize();
  else columnSizing.value = {...columnSizing.value, [id]: Math.max(48, Math.min(1000, size + (event.key === 'ArrowRight' ? 16 : -16)))};
}
</script>

<template>
  <div class="dt-wrap" :aria-busy="loading">
    <div v-if="loading" class="dt-loading" role="status">正在加载数据…</div>
    <p v-if="unavailable && rows.length" class="dt-loading" role="status">显示上次成功读取的数据，请重试以确认最新状态。</p>
    <table class="dt" :style="{width: table.getTotalSize() + 'px'}">
      <colgroup><col v-for="column in table.getVisibleLeafColumns()" :key="column.id" :style="{width: column.getSize() + 'px'}" /></colgroup>
      <thead>
        <tr v-for="hg in table.getHeaderGroups()" :key="hg.id">
          <th
            v-for="h in hg.headers"
            :key="h.id"
            :aria-sort="h.column.getIsSorted() === 'asc' ? 'ascending' : h.column.getIsSorted() === 'desc' ? 'descending' : undefined"
            :style="{ width: h.getSize() + 'px' }"
          >
            <button v-if="!h.isPlaceholder && h.column.getCanSort()" class="sort-button" @click="h.column.toggleSorting()"><FlexRender :render="h.column.columnDef.header" :props="h.getContext()"/><span aria-hidden="true">{{h.column.getIsSorted() === 'asc' ? '↑' : h.column.getIsSorted() === 'desc' ? '↓' : '↕'}}</span></button><FlexRender v-else-if="!h.isPlaceholder" :render="h.column.columnDef.header" :props="h.getContext()" />
            <span v-if="h.column.getCanResize()" class="column-resizer" role="separator" tabindex="0" aria-orientation="vertical" :aria-label="`调整列宽 ${typeof h.column.columnDef.header === 'string' ? h.column.columnDef.header || '操作' : '操作'}`" :aria-valuenow="h.getSize()" :aria-valuemin="48" :aria-valuemax="1000" @mousedown="h.getResizeHandler()($event)" @touchstart="h.getResizeHandler()($event)" @dblclick="h.column.resetSize()" @keydown="resizeKey($event,h.column.id,h.getSize())" />
          </th>
        </tr>
      </thead>
      <tbody v-if="rows.length" ref="body">
        <tr
          v-for="(r, index) in rows"
          :key="r.id"
          :class="{ clickable: !!rowClick, selected: !!rowClick && selectedId === r.id }"
          :tabindex="rowClick ? (tabRow === r.id ? 0 : -1) : undefined"
          @focus="selectedId = r.id"
          @keydown="onRowKey($event,index)"
          @click="openRow($event,r.id,r.original)"
        >
          <td v-for="c in r.getVisibleCells()" :key="c.id">
            <FlexRender :render="c.column.columnDef.cell" :props="c.getContext()" />
          </td>
        </tr>
      </tbody>
    </table>
    <div v-if="!rows.length && !loading && !unavailable" class="dt-empty">
      <p class="empty-title">{{ emptyTitle }}</p>
      <p v-if="emptyHint" class="empty-hint">{{ emptyHint }}</p>
      <slot name="empty" />
    </div>
  </div>
</template>

<style scoped>
.dt-wrap {
  border: 1px solid var(--border-default);
  border-radius: 0;
  background: var(--surface-primary);
  overflow: auto;
}
.dt {
  min-width: 100%;
  table-layout: fixed;
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
  padding: var(--space-2) var(--space-4);
  height: var(--height-table-row);
  vertical-align: middle;
  overflow-wrap: anywhere;
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
.dt tbody tr.selected{background:var(--surface-selected)}
.dt tbody tr:focus-visible{outline-offset:-2px}
:deep(.dt-cell-text){display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;max-height:calc(2em * var(--line-height-ui));line-height:var(--line-height-ui);overflow:hidden;overflow-wrap:anywhere}
.column-resizer{position:absolute;right:0;top:0;width:var(--space-2);height:100%;cursor:col-resize;touch-action:none;user-select:none}
.column-resizer:hover,.column-resizer:focus-visible{background:var(--accent-subtle)}
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
