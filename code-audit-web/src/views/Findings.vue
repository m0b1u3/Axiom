<script setup lang="ts">
import { computed, h, onMounted, ref } from "vue";
import { useRouter, useRoute } from "vue-router";
import InterimFindingCatalog from "../components/InterimFindingCatalog.vue";
import type { ColumnDef } from "@tanstack/vue-table";
import { api } from "../api";
import type { FindingSummary } from "../types";
import PageHeader from "../components/PageHeader.vue";
import DataTable from "../components/DataTable.vue";
import Input from "../components/ui/Input.vue";
import Select from "../components/ui/Select.vue";
import Button from "../components/ui/Button.vue";
import Tooltip from "../components/ui/Tooltip.vue";
import CatalogPagination from "../components/CatalogPagination.vue";
import {useCatalog} from "../composables/catalog";
import type {Project} from "../types";
import { relativeTime, absoluteTime } from "../utils/time";

/* 漏洞池：跨扫描按指纹去重的全局风险视图 */
const router = useRouter(), route = useRoute();
const interimView = computed(() => route.query.view === 'interim');
function setView(interim:boolean){void router.push({query:{...route.query,view:interim?'interim':undefined}});}
function viewKey(event:KeyboardEvent){if(['ArrowLeft','ArrowRight','Home','End'].includes(event.key)){event.preventDefault();const next=event.key==='Home'?false:event.key==='End'?true:!interimView.value;setView(next);document.getElementById(next?'interim-tab':'final-findings-tab')?.focus();}}
const {result, loading, error:loadError, search, filters, sorting, load, filter, sort, page, change} = useCatalog<FindingSummary>(api.findingPage, ['severity','status','projectId','sourceSnapshotId','sourcePath'], 'severity', 'asc');
const projects = ref<Project[]>([]), projectError = ref('');
async function loadProjects() {
  projectError.value='';
  try { projects.value=await api.listProjects(); } catch(error) { projectError.value=(error as Error).message; }
}
const projectOptions = computed(() => [{value:'0',label:'未归属项目'}, ...projects.value.map(p=>({value:String(p.id),label:p.name}))]);
const sevChips = computed(() => [
  {key:'',label:'全部'}, {key:'critical',label:'Critical'}, {key:'high',label:'High'},
  {key:'medium',label:'Medium'}, {key:'low',label:'Low'}, {key:'info',label:'Info'},
].map(chip=>({...chip,count:result.value?.counts?.[chip.key] ?? '—'})));

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
          { class: "dt-cell-text", title: row.original.title, style: { fontWeight: 500, color: "var(--text-primary)", lineHeight: 1.4 } },
          row.original.title,
        ),
        (row.original.raw as {archive?:{assessment?:string}}|null)?.archive?.assessment==='sdk_reportable' ? h('small',{style:{color:'var(--text-secondary)'}},'SDK 可报告 · 未复现') : null,
        row.original.cwe?.length
          ? h("span", { class: "mono", style: { fontSize: "10px", color: "var(--text-tertiary)" } }, row.original.cwe.join(", "))
          : null,
      ]),
  },
  {
    accessorKey: "filePath",
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

onMounted(loadProjects);
</script>

<template>
  <div>
    <div v-if="loadError" class="request-error" role="alert"><span>数据加载失败：{{loadError}}</span><Button variant="outline" size="sm" @click="load">重试</Button></div>
    <div v-if="projectError" class="request-error" role="alert">项目筛选读取失败：{{projectError}}<Button variant="outline" size="sm" @click="loadProjects">重读项目筛选</Button></div>
    <PageHeader title="漏洞" description="最终归档漏洞与扫描中间发现，分开展示并保留证据。" />
    <div class="finding-views" role="tablist" aria-label="漏洞结果类型" @keydown="viewKey">
      <button id="final-findings-tab" type="button" role="tab" :aria-selected="!interimView" :tabindex="interimView?-1:0" aria-controls="final-findings-panel" @click="setView(false)">已归档漏洞</button>
      <button id="interim-tab" type="button" role="tab" :aria-selected="interimView" :tabindex="interimView?0:-1" aria-controls="global-interim-panel" @click="setView(true)">中间发现</button>
    </div>
    <section id="final-findings-panel" role="tabpanel" aria-labelledby="final-findings-tab" v-show="!interimView">

    <div class="toolbar">
      <div class="filters">
        <Button
          v-for="c in sevChips"
          :key="c.key"
          size="sm"
          :variant="filters.severity === c.key ? 'secondary' : 'ghost'"
          @click="filter('severity', c.key)"
        >
          <span>{{ c.label }}</span>
          <span class="count">{{ c.count }}</span>
        </Button>
      </div>
      <div class="toolbar-right">
        <Select :model-value="filters.projectId" :options="projectOptions" placeholder="全部项目" clearable class="project-select" aria-label="漏洞所属项目" @change="filter('projectId',$event)" />
        <Select
          :model-value="filters.status"
          @change="filter('status',$event)"
          :options="statusOptions"
          placeholder="全部状态"
          clearable
          class="status-select"
          aria-label="漏洞状态"
        />
        <Input v-model="search" class="search" aria-label="搜索漏洞" placeholder="搜索漏洞 / 文件 / CWE / 指纹…" maxlength="256" />
      </div>
    </div>

    <div v-if="filters.sourceSnapshotId || filters.sourcePath" class="scope-filter" role="status">
      当前来源：{{filters.sourceSnapshotId ? '快照 #' + filters.sourceSnapshotId : filters.sourcePath}}
      <Button variant="ghost" size="sm" @click="change({sourceSnapshotId:undefined,sourcePath:undefined,page:undefined})">清除来源筛选</Button>
    </div>
    <DataTable
      :loading="loading"
      :unavailable="!!loadError"
      :data="result?.items ?? []"
      manual-sorting :sorting="sorting" @update:sorting="sort"
      :columns="columns"
      :row-click="gotTo"
      empty-title="暂无匹配漏洞"
      empty-hint="这里显示已归档漏洞。失败或超时的候选与证据，请在“中间发现”查看。"
    />
    <CatalogPagination label="漏洞分页" :page="result?.page" :page-size="result?.pageSize" :total="result?.total" :loading="loading" :unavailable="!!loadError" @change="page" />
    </section>
    <section id="global-interim-panel" :role="interimView?'tabpanel':undefined" :aria-labelledby="interimView?'interim-tab':undefined"><InterimFindingCatalog :active="interimView" :projects="projects" @open="setView(true)"/></section>
  </div>
</template>

<style scoped>
.finding-views{display:flex;gap:var(--space-4);border-bottom:1px solid var(--border-default);margin-bottom:var(--space-3)}.finding-views button{font-size:var(--font-size-sm);padding-block:var(--space-2);color:var(--text-secondary);border-bottom:2px solid transparent}.finding-views button[aria-selected="true"]{color:var(--accent-primary);border-bottom-color:var(--accent-primary);font-weight:var(--font-weight-medium)}.finding-views button:focus-visible{outline:2px solid var(--border-focus);outline-offset:2px}
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
  flex-wrap: wrap;
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
.project-select{width:160px}.scope-filter{display:flex;align-items:center;gap:var(--space-2);font-size:var(--font-size-xs);margin-bottom:var(--space-2);overflow-wrap:anywhere}
.toolbar-right {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  margin-left: auto;
  flex-wrap: wrap;
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

.toolbar{flex-wrap:wrap}.filters{max-width:100%;overflow-x:auto;min-width:0}.toolbar-right{min-width:0}.search{max-width:100%}@media(max-width:760px){.toolbar-right{width:100%;margin-left:0;flex-shrink:1}.toolbar-right .search{flex:1 0 100%;width:100%;min-width:0}.filters{width:100%}.total{font-size:var(--font-size-xs)}.sec-right{flex-wrap:wrap;min-width:0}.section-title-bar{gap:var(--space-3);flex-wrap:wrap}}
</style>
