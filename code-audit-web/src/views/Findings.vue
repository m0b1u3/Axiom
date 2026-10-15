<script setup lang="ts">
import { computed, h, onMounted, ref } from "vue";
import { useRouter, useRoute } from "vue-router";
import type { ColumnDef } from "@tanstack/vue-table";
import { api } from "../api";
import type { FindingSummary } from "../types";
import PageHeader from "../components/PageHeader.vue";
import DataTable from "../components/DataTable.vue";
import Input from "../components/ui/Input.vue";
import Select from "../components/ui/Select.vue";
import Button from "../components/ui/Button.vue";
import Tooltip from "../components/ui/Tooltip.vue";
import { message } from "../lib/toast";
import { relativeTime, absoluteTime } from "../utils/time";

/* 漏洞池：跨扫描按指纹去重的全局风险视图 */
const router = useRouter();
const items = ref<FindingSummary[]>([]);
const allItems = ref<FindingSummary[]>([]);
const loading = ref(false);
const loadError = ref("");
const severityFilter = ref(String(useRoute().query.severity || ""));
const statusFilter = ref("");
const search = ref("");

const sevChips = computed(() => {
  const c = { critical: 0, high: 0, medium: 0, low: 0, info: 0 };
  for (const f of allItems.value) {
    const k = f.severity as keyof typeof c;
    if (k in c) c[k]++;
  }
  return [
    { key: "", label: "全部", count: allItems.value.length },
    { key: "critical", label: "Critical", count: c.critical },
    { key: "high", label: "High", count: c.high },
    { key: "medium", label: "Medium", count: c.medium },
    { key: "low", label: "Low", count: c.low },
    { key: "info", label: "Info", count: c.info },
  ];
});

const statusOptions = [
  { value: "open", label: "未处理" },
  { value: "confirmed", label: "已确认" },
  { value: "false_positive", label: "误报" },
  { value: "fixed", label: "已修复" },
];

function statusLabel(st: string): string {
  return statusOptions.find((o) => o.value === st)?.label ?? st;
}

function statusClass(st: string): Record<string, string> {
  switch (st) {
    case "confirmed":
      return { color: "var(--state-warning)" };
    case "false_positive":
      return { color: "var(--text-tertiary)", textDecoration: "line-through" };
    case "fixed":
      return { color: "var(--state-success)" };
    default:
      return { color: "var(--text-secondary)" };
  }
}

async function load(): Promise<void> {
  if (loading.value) return;
  loading.value = true;
  loadError.value = "";
  try {
    allItems.value = await api.listFindingsPool({});
    const q = search.value.trim().toLowerCase();
    items.value = allItems.value.filter(f => (!severityFilter.value || f.severity === severityFilter.value) && (!statusFilter.value || f.status === statusFilter.value) && (!q || (f.title+" "+f.filePath+" "+f.cwe.join(" ")).toLowerCase().includes(q)));;
  } catch (error) {
    loadError.value = (error as Error).message;
  } finally {
    loading.value = false;
  }
}

function gotTo(f: FindingSummary): void {
  void router.push({ name: "scan-detail", params: { id: f.scanId }, query: { finding: String(f.id) } });
}

const columns: ColumnDef<FindingSummary>[] = [
  {
    accessorKey: "severity",
    header: "严重度",
    size: 100,
    cell: (c) =>
      h("span", { class: `sev-pill ${c.getValue<string>().toLowerCase()}` }, c.getValue<string>()),
  },
  {
    accessorKey: "title",
    header: "漏洞",
    cell: ({ row }) =>
      h("span", { style: { display: "flex", flexDirection: "column", gap: "2px", minWidth: 0 } }, [
        h(
          "span",
          { style: { fontWeight: 500, color: "var(--text-primary)", lineHeight: 1.4 } },
          row.original.title,
        ),
        row.original.cwe?.length
          ? h("span", { class: "mono", style: { fontSize: "10px", color: "var(--text-tertiary)" } }, row.original.cwe.join(", "))
          : null,
      ]),
  },
  {
    id: "loc",
    header: "位置",
    size: 260,
    cell: ({ row }) =>
      h(
        "span",
        { class: "mono", style: { fontSize: "11px", color: "var(--text-tertiary)" } },
        `${row.original.filePath}${row.original.startLine ? ":" + row.original.startLine : ""}`,
      ),
  },
  {
    accessorKey: "status",
    header: "状态",
    size: 110,
    cell: (c) => h("span", { style: { fontSize: "12px", ...statusClass(c.getValue<string>()) } }, statusLabel(c.getValue<string>())),
  },
  {
    accessorKey: "lastSeenAt",
    header: "最近出现",
    size: 110,
    cell: (c) =>
      h(Tooltip, { content: absoluteTime(c.getValue<string>()) }, () =>
        h("span", { style: { fontSize: "12px", color: "var(--text-secondary)" } }, relativeTime(c.getValue<string>())),
      ),
  },
];

onMounted(load);
</script>

<template>
  <div>
    <div v-if="loadError" class="request-error" role="alert"><span>数据加载失败：{{loadError}}</span><Button variant="outline" size="sm" @click="load">重试</Button></div>
    <PageHeader title="漏洞" description="全局风险视图：跨扫描按指纹去重" />

    <div class="toolbar">
      <div class="filters">
        <Button
          v-for="c in sevChips"
          :key="c.key"
          size="sm"
          :variant="severityFilter === c.key ? 'secondary' : 'ghost'"
          @click="severityFilter = c.key; load()"
        >
          <span>{{ c.label }}</span>
          <span class="count">{{ c.count }}</span>
        </Button>
      </div>
      <div class="toolbar-right">
        <Select
          v-model="statusFilter"
          :options="statusOptions"
          placeholder="全部状态"
          clearable
          class="status-select"
          @change="load"
        />
        <Input v-model="search" class="search" placeholder="搜索漏洞 / 文件 / CWE…" @enter="load" />
      </div>
    </div>

    <DataTable
      :loading="loading"
      :data="items"
      :columns="columns"
      :row-click="gotTo"
      empty-title="暂无匹配漏洞"
      empty-hint="漏洞在扫描完成后按指纹聚合到此处。"
    />
  </div>
</template>

<style scoped>
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  margin-bottom: var(--space-3);
  min-height: var(--height-toolbar);
}
.filters {
  display: inline-flex;
  align-items: center;
  background: var(--surface-secondary);
  border: 1px solid var(--border-default);
  border-radius: var(--radius-md);
  padding: 2px;
  gap: 2px;
  flex-shrink: 0;
}
.count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  line-height: 1;
  padding: 1px 5px;
  border-radius: var(--radius-xs);
  background: var(--surface-tertiary);
  color: var(--text-tertiary);
  font-weight: 500;
}
.toolbar-right {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  margin-left: auto;
  flex-shrink: 0;
}
.status-select {
  width: 125px;
}
.search {
  width: 250px;
}

@media (max-width: 960px) {
  .toolbar {
    flex-wrap: wrap;
  }
  .toolbar-right {
    margin-left: 0;
    width: 100%;
  }
  .search {
    flex: 1;
  }
}

.toolbar{flex-wrap:wrap}.filters{max-width:100%;overflow-x:auto;min-width:0}.toolbar-right{min-width:0}.search{max-width:100%}@media(max-width:760px){.toolbar-right{width:100%;margin-left:0;flex-shrink:1}.search{flex:1;min-width:0}.filters{width:100%}.total{font-size:var(--font-size-xs)}.sec-right{flex-wrap:wrap;min-width:0}.section-title-bar{gap:var(--space-3);flex-wrap:wrap}}
</style>
