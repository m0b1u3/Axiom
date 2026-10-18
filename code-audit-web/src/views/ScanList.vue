<script setup lang="ts">
import { useAuth } from "../composables/auth";
const { canOperate, canAdmin } = useAuth();
import { computed, h, onMounted, onUnmounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { Plus, Search, RefreshCw, Trash2 } from "lucide-vue-next";
import type { ColumnDef } from "@tanstack/vue-table";
import { api } from "../api";
import type { QueueOverview, ScanTask } from "../types";
import PageHeader from "../components/PageHeader.vue";
import StatusBadge from "../components/StatusBadge.vue";
import DataTable from "../components/DataTable.vue";
import Button from "../components/ui/Button.vue";
import Input from "../components/ui/Input.vue";
import Tooltip from "../components/ui/Tooltip.vue";
import { message } from "../lib/toast";
import ResourceCleanupDialog from "../components/ResourceCleanupDialog.vue";
import { relativeTime, absoluteTime } from "../utils/time";

import QueueCapacity from '../components/QueueCapacity.vue';
import {queueReasons, elapsed} from '../utils/execution';
const overview = ref<QueueOverview|null>(null);
const queueError=ref('');
const queueById=computed(()=>new Map(overview.value?.tasks.map(t=>[t.taskId,t])??[]));
const router = useRouter();
const scans = ref<ScanTask[]>([]);
const loading = ref(false);
const fetching = ref(false);
const refreshing = ref(false);
const initialized = ref(false);
const loadError = ref("");
const keyword = ref("");
const statusFilter = ref("");
let timer: number | undefined;

const RUNNING = new Set(["pending", "claimed", "scanning", "cancel_requested"]);
const failedSet = new Set(["failed", "timeout"]);

const page = ref(1);
const total = ref(0);
const counts = ref<Record<string,number>>({});
let filterVersion = 0;
let searchTimer: ReturnType<typeof setTimeout> | undefined;
let disposed = false;
const groups = computed(() => [
 {key:"",label:"全部"},{key:"pending",label:"排队中"},{key:"running",label:"运行中"},
 {key:"done",label:"已完成"},{key:"failed",label:"失败"},
].map(g=>({...g,count:counts.value[g.key]??0})));
const filtered = computed(()=>scans.value);
watch([keyword,statusFilter],()=>{filterVersion++;page.value=1;clearTimeout(searchTimer);searchTimer=setTimeout(()=>void load(),250)});
watch(page,()=>{filterVersion++;void load()});

async function load(manual = false): Promise<void> {
  if (manual) refreshing.value = true;
  if (fetching.value) return;
  fetching.value = true;
  const version = filterVersion;
  loading.value = !initialized.value;
  try {
    const [list, queue] = await Promise.allSettled([api.scanPage(page.value,keyword.value,statusFilter.value),api.queueOverview()]);
    if (disposed || version !== filterVersion) return;
    if(queue.status==='fulfilled'){overview.value=queue.value;queueError.value=''}else{overview.value=null;queueError.value=queue.reason.message}
    if(list.status==='rejected')throw list.reason;
    scans.value = list.value.items;
    total.value = list.value.total;
    counts.value = list.value.counts;
    page.value = list.value.page;
    initialized.value = true;
    loadError.value = "";
  } catch (error) {
    if (!disposed && version === filterVersion) loadError.value = (error as Error).message;
  } finally {
    loading.value = false;
    fetching.value = false;
    refreshing.value = false;
    if (!disposed && version !== filterVersion) void load();
  }
}

const cleanupOpen = ref(false), cleanupTask = ref(0);
function removeScan(s: ScanTask): void { cleanupTask.value = s.id; cleanupOpen.value = true; }
async function scanDeleted(): Promise<void> { message.success("已删除扫描与产物"); await load(); }

function sourceLabel(task: ScanTask): string {
  const segs = task.sourcePath.split(/[\\/]/).filter(Boolean);
  const last = segs[segs.length - 1] ?? task.sourcePath;
  const m = last.match(/^snap-([0-9a-f]{6,})$/i);
  if (m) return `快照 snap-${m[1].slice(0, 6)}…`;
  return last;
}

const TERMINAL = ["completed", "partial", "failed", "canceled", "timeout"];

const columns: ColumnDef<ScanTask>[] = [
  {
    accessorKey: "scanNo",
    header: "扫描任务编号",
    size: 190,
    cell: (c) =>
      h("span", { class: "mono", style: { fontWeight: 600, color: "var(--accent-primary)" } }, (c.row.original.sdkOperationKind==='preflight'?'SDK 预检 · ':c.row.original.sdkOperationKind==='validate'?'SDK 复核 · ':c.row.original.sdkOperationKind==='component_normalize'?'组件校验 · ':c.row.original.sdkOperationKind==='component_suggest'?'组件建议 · ':'')+c.getValue<string>()),
  },
  {
    id: "project",
    header: "项目",
    size: 130,
    cell: ({ row }) => {
      const s = row.original;
      if (!s.projectName) return h("span", { style: { color: "var(--text-disabled)", fontSize: "11px" } }, "—");
      return h(
        "button",
        {
          class: "proj-link",
          title: `查看项目「${s.projectName}」`,
          onClick: (e: Event) => {
            e.stopPropagation();
            void router.push({ name: "project-detail", params: { id: s.projectId } });
          },
        },
        s.projectName,
      );
    },
  },
  {
    accessorKey: "sourcePath",
    header: "目标快照 / 源码",
    cell: ({ row }) =>
      h(Tooltip, { content: row.original.sourcePath }, () =>
        h("span", { class: "mono", style: { color: "var(--text-secondary)", fontSize: "12px" } }, sourceLabel(row.original)),
      ),
  },
  {
    id: "model",
    header: "模型供应商与引擎",
    size: 220,
    cell: ({ row }) =>
      h(
        "span",
        { class: "mono", style: { color: "var(--text-tertiary)", fontSize: "11px" } },
        `${row.original.provider} · ${row.original.model}`,
      ),
  },
  {
    accessorKey: "findingCount",
    header: "检出漏洞",
    size: 80,
    cell: (c) =>
      h(
        "span",
        {
          class: "count-num" + (c.getValue<number>() === 0 ? " zero" : ""),
          style: { fontWeight: 700 },
        },
        String(c.getValue<number>()),
      ),
  },
  {
    accessorKey: "status",
    header: "任务状态",
    size: 110,
    cell: ({row}) => {const q=queueById.value.get(row.original.id);return h('div',{class:'execution-cell'},[
      h(StatusBadge,{status:row.original.status}),
      q?.reason?h('span',{class:'execution-note'},`${q.position?`第 ${q.position} 位 · `:''}${queueReasons[q.reason]??q.reason}`):null,
      q?h('span',{class:'execution-note'},q.status==='pending'?`已等待 ${elapsed(q.queuedAt,overview.value?.serverTime)}`:q.agentId?`${q.agentId} · ${elapsed(q.startedAt,overview.value?.serverTime)}`:''):null
    ])},
  },
  {
    accessorKey: "createdAt",
    header: "触发时间",
    size: 110,
    cell: (c) =>
      h(Tooltip, { content: absoluteTime(c.getValue<string>()) }, () =>
        h("span", { class: "mono", style: { color: "var(--text-secondary)", fontSize: "11px" } }, relativeTime(c.getValue<string>())),
      ),
  },
  {
    id: "ops",
    header: "",
    size: 48,
    cell: ({ row }) => {
      const s = row.original;
      if (!canAdmin.value || !TERMINAL.includes(s.status)) return null;
      return h(Tooltip, { content: "删除任务记录" }, () =>
        h(
          "button",
          {
            class: "op-btn",
            "aria-label": `删除扫描 ${s.scanNo}`,
            onClick: (e: Event) => {
              e.stopPropagation();
              void removeScan(s);
            },
          },
          h(Trash2, { size: 13, "stroke-width": 1.7 }),
        ),
      );
    },
  },
];

onMounted(() => {
  void load();
  timer = window.setInterval(() => void load(), 5000);
});
onUnmounted(() => {
  disposed=true;
  clearTimeout(searchTimer);
  if (timer) window.clearInterval(timer);
});
</script>

<template>
  <div>
    <div v-if="loadError" class="request-error" role="alert"><span>数据加载失败：{{loadError}}</span><Button variant="outline" size="sm" @click="load(true)">重试</Button></div>
    <PageHeader title="扫描任务" description="全部扫描执行与状态">
      <Button v-if="canOperate" @click="router.push({ name: 'new-scan' })">
        <Plus :size="14" />新建扫描
      </Button>
    </PageHeader>

    <QueueCapacity v-if="overview" :overview="overview" />
    <div v-if="queueError" class="request-error" role="alert">队列状态暂不可用：{{queueError}}<Button variant="ghost" size="sm" @click="load(true)">重新加载</Button></div>
    <!-- Toolbar：filter + search + count -->
    <div class="toolbar">
      <div class="filters">
        <Button
          v-for="g in groups"
          :key="g.key"
          size="sm"
          :variant="statusFilter === g.key ? 'secondary' : 'ghost'"
          @click="statusFilter = g.key"
        >
          <span>{{ g.label }}</span>
          <span class="count">{{ g.count }}</span>
        </Button>
      </div>
      <div class="toolbar-right">
        <Input v-model="keyword" class="search" aria-label="搜索扫描任务" placeholder="搜索扫描 / 源码 / 模型…" />
        <Button variant="secondary" size="icon" title="刷新扫描任务" aria-label="刷新扫描任务" :loading="refreshing" @click="load(true)">
          <RefreshCw :size="13" />
        </Button>
      </div>
    </div>

    <DataTable
      :row-key="(scan: ScanTask) => String(scan.id)"
      :loading="loading"
      :unavailable="!!loadError"
      :data="filtered"
      :columns="columns"
      :row-click="(s) => router.push({ name: 'scan-detail', params: { id: s.id } })"
      :empty-title="keyword.trim() || statusFilter ? '没有匹配的扫描任务' : '还没有扫描任务'"
      empty-hint="从项目或本地快照发起新扫描"
    />
    <nav class="toolbar" aria-label="扫描任务分页">
      <span>共 {{ total }} 条 · 第 {{ page }} / {{ Math.max(1, Math.ceil(total / 50)) }} 页</span>
      <div class="toolbar-right">
        <Button variant="outline" size="sm" :disabled="page <= 1 || fetching" @click="page--">上一页</Button>
        <Button variant="outline" size="sm" :disabled="page * 50 >= total || fetching" @click="page++">下一页</Button>
      </div>
    </nav>
    <ResourceCleanupDialog v-model:open="cleanupOpen" kind="scan" :resource-id="cleanupTask" @completed="scanDeleted" />
  </div>
</template>

<style scoped>
:deep(.execution-cell){display:flex;flex-direction:column;gap:var(--space-1);min-width:160px}:deep(.execution-note){font-size:var(--font-size-xs);color:var(--text-secondary);white-space:normal;overflow-wrap:anywhere}

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
.search {
  width: 250px;
}
:deep(.proj-link) {
  background: none;
  border: none;
  padding: 0;
  font: inherit;
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  cursor: pointer;
}
:deep(.proj-link:hover) {
  color: var(--accent-primary);
  text-decoration: underline;
}
:deep(.op-btn) {
  display: inline-grid;
  place-items: center;
  width: 24px;
  height: 24px;
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-tertiary);
  cursor: pointer;
  transition: background-color var(--duration-fast) var(--ease-ui),
    color var(--duration-fast) var(--ease-ui);
}
:deep(.op-btn:hover) {
  background: var(--state-danger-bg);
  color: var(--state-danger);
}

.toolbar{flex-wrap:wrap}.filters{max-width:100%;overflow-x:auto;min-width:0}.toolbar-right{min-width:0}.search{max-width:100%}@media(max-width:760px){.toolbar-right{width:100%;margin-left:0;flex-shrink:1}.search{flex:1;min-width:0}.filters{width:100%}.total{font-size:var(--font-size-xs)}.sec-right{flex-wrap:wrap;min-width:0}.section-title-bar{gap:var(--space-3);flex-wrap:wrap}}
</style>
