<script setup lang="ts">
import ResourceCleanupDialog from "../components/ResourceCleanupDialog.vue";
import { useAuth } from "../composables/auth";
const { canOperate, canAdmin } = useAuth();
import { computed, h, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { Upload, Trash2, ScanLine } from "lucide-vue-next";
import type { ColumnDef } from "@tanstack/vue-table";
import { api, type SnapshotInfo } from "../api";
import type { Project } from "../types";
import PageHeader from "../components/PageHeader.vue";
import DataTable from "../components/DataTable.vue";
import Button from "../components/ui/Button.vue";
import Input from "../components/ui/Input.vue";
import Tooltip from "../components/ui/Tooltip.vue";
import Select from "../components/ui/Select.vue";
import CatalogPagination from "../components/CatalogPagination.vue";
import {useCatalog} from "../composables/catalog";
import { message } from "../lib/toast";

import { relativeTime, absoluteTime } from "../utils/time";
import { shortHash } from "../utils/format";

/* 资产 = 全部代码快照的统一管理（跨项目视角） */
const router = useRouter();
const {result, loading, error:loadError, search:keyword, filters, sorting, load, filter, sort, page} = useCatalog<SnapshotInfo>(api.snapshotPage, ['projectId','usage'], 'id','desc');
const projects = ref<Project[]>([]), projectError = ref('');
const uploading = ref(false);
const fileInput = ref<HTMLInputElement | null>(null);
const projectName = computed(() => {
  const names=new Map(projects.value.map(p=>[p.id,p.name]));
  return (id:number) => id>0 ? (names.get(id) ?? '#' + id) : '';
});
const projectOptions = computed(() => [{value:'0',label:'未归属项目'}, ...projects.value.map(p=>({value:String(p.id),label:p.name}))]);
const usageOptions = [{value:'referenced',label:'有扫描引用'},{value:'unused',label:'无扫描引用'}];
async function loadProjects() {
  projectError.value='';
  try { projects.value=await api.listProjects(); } catch(error) { projectError.value=(error as Error).message; }
}

function pickFile(): void {
  fileInput.value?.click();
}

async function onFilePicked(event: Event): Promise<void> {
  if (uploading.value) return;
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  if (!file) return;
  if (!/\.(zip|tar\.gz|tgz)$/i.test(file.name)) {
    message.warning("仅支持 .zip / .tar.gz / .tgz");
    input.value = "";
    return;
  }
  uploading.value = true;
  try {
    const snap = await api.uploadSnapshot(file);
    message.success(`已上传 ${snap.originalFilename}（${snap.fileCount} 个文件）`);
    await load();
  } catch (error) {
    message.error((error as Error).message);
  } finally {
    uploading.value = false;
    input.value = "";
  }
}

const cleanupOpen=ref(false),cleanupSnapshot=ref(0);
function removeSnapshot(snap:SnapshotInfo){cleanupSnapshot.value=snap.id;cleanupOpen.value=true}
async function snapshotDeleted(){message.success('已删除代码快照与目录');await load()}

function formatSize(bytes: number): string {
  if (bytes >= 1 << 20) return (bytes / (1 << 20)).toFixed(1) + " MB";
  if (bytes >= 1 << 10) return (bytes / (1 << 10)).toFixed(1) + " KB";
  return bytes + " B";
}

const columns: ColumnDef<SnapshotInfo>[] = [
  {
    accessorKey: "originalFilename",
    header: "归档",
    cell: ({ row }) =>
      h("span", { style: { display: "flex", flexDirection: "column", gap: "1px", minWidth: 0 } }, [
        h("span", { class: "dt-cell-text", title: row.original.originalFilename, style: { fontWeight: 500, color: "var(--text-primary)" } }, row.original.originalFilename),
        h(Tooltip, { content: row.original.snapshotPath }, () =>
          h(
            "span",
            { class: "mono", style: { fontSize: "10px", color: "var(--text-tertiary)" } },
            row.original.snapshotPath.split(/[\\/]/).slice(-2).join("/"),
          ),
        ),
      ]),
  },
  {
    id: "project",
    header: "项目",
    size: 130,
    cell: ({ row }) => {
      const s = row.original;
      if (s.projectId <= 0) return h("span", { style: { color: "var(--text-tertiary)", fontSize: "11px" } }, "未归属");
      return h(
        "button",
        {
          class: "proj-link",
          onClick: () => void router.push({ name: "project-detail", params: { id: s.projectId } }),
        },
        projectName.value(s.projectId),
      );
    },
  },
  {
    accessorKey: "archiveSha256",
    header: "内容哈希",
    size: 100,
    cell: (c) => h("span", { class: "mono", style: { color: "var(--text-tertiary)", fontSize: "11px" } }, shortHash(c.getValue<string>())),
  },
  {
    accessorKey: "fileCount",
    header: "文件数",
    size: 70,
    cell: (c) => h("span", { class: "count-num" }, String(c.getValue<number>())),
  },
  {
    accessorKey: "totalSize",
    header: "大小",
    size: 90,
    cell: (c) => h("span", { class: "mono", style: { color: "var(--text-tertiary)", fontSize: "11px" } }, formatSize(c.getValue<number>())),
  },
  {
    accessorKey: "scanCount",
    header: "扫描次数",
    size: 80,
    cell: (c) => h("span", { class: "count-num" + (c.getValue<number>() === 0 ? " zero" : "") }, String(c.getValue<number>())),
  },
  {
    accessorKey: "createdAt",
    header: "上传时间",
    size: 110,
    cell: (c) =>
      h(Tooltip, { content: absoluteTime(c.getValue<string>()) }, () =>
        h("span", { style: { color: "var(--text-secondary)", fontSize: "12px" } }, relativeTime(c.getValue<string>())),
      ),
  },
  {
    id: "ops",
    header: "",
    size: 120,
    cell: ({ row }) => {
      const s = row.original;
      if (!canOperate.value) return h("button",{class:"proj-link",onClick:()=>void router.push("/component-plans?snapshotId="+s.id)},"组件计划");
      return h("span", { style: { display: "flex", gap: "2px", justifyContent: "flex-end" } }, [
 h("button",{class:"proj-link",onClick:()=>void router.push("/component-plans?snapshotId="+s.id),"aria-label":"管理组件计划 "+s.originalFilename},"组件计划"),
        h(Tooltip, { content: "用此快照新建扫描" }, () =>
          h(
            "button",
            {
              class: "op-btn",
              "aria-label": `扫描快照 ${s.originalFilename}`,
              onClick: () =>
                void router.push({
                  name: "new-scan",
                  query: { snapshotId: String(s.id), projectId: s.projectId > 0 ? String(s.projectId) : undefined },
                }),
            },
            h(ScanLine, { size: 13, "stroke-width": 1.7 }),
          ),
        ),
        h(Tooltip, { content: s.scanCount > 0 ? "有扫描引用，不可删除" : "删除快照" }, () =>
          h(
            "button",
            {
              class: "op-btn",
              "aria-label": `删除快照 ${s.originalFilename}`,
              disabled: !canAdmin.value || s.scanCount > 0,
              onClick: () => void removeSnapshot(s),
            },
            h(Trash2, { size: 13, "stroke-width": 1.7 }),
          ),
        ),
      ]);
    },
  },
];

onMounted(loadProjects);
</script>

<template>
  <ResourceCleanupDialog v-model:open="cleanupOpen" kind="snapshot" :resource-id="cleanupSnapshot" @completed="snapshotDeleted" />
  <div>
    <div v-if="loadError" class="request-error" role="alert"><span>数据加载失败：{{loadError}}</span><Button variant="outline" size="sm" @click="load">重试</Button></div>
    <div v-if="projectError" class="request-error" role="alert">项目筛选读取失败：{{projectError}}<Button variant="outline" size="sm" @click="loadProjects">重读项目筛选</Button></div>
    <PageHeader title="资产" description="全部代码快照的统一管理（跨项目）">
      <Button v-if="canOperate" variant="secondary" :loading="uploading" @click="pickFile">
        <Upload :size="14" />上传代码包
      </Button>
    </PageHeader>
    <input ref="fileInput" type="file" accept=".zip,.tar.gz,.tgz" style="display: none" @change="onFilePicked" />

    <div class="toolbar">
      <Select :model-value="filters.projectId" :options="projectOptions" clearable placeholder="全部项目" aria-label="资产所属项目" class="project-select" @change="filter('projectId',$event)" />
      <Select :model-value="filters.usage" :options="usageOptions" clearable placeholder="全部引用状态" aria-label="快照引用状态" class="usage-select" @change="filter('usage',$event)" />
      <Input v-model="keyword" class="search" aria-label="搜索代码快照" placeholder="搜索文件名 / 哈希 / 项目 / 路径…" maxlength="256" />
      <span class="total mono">{{ result?.total ?? '—' }} 个快照</span>
    </div>


    <DataTable
      :loading="loading"
      :unavailable="!!loadError"
      :data="result?.items ?? []"
      manual-sorting :sorting="sorting" @update:sorting="sort"
      :columns="columns"
      :empty-title="keyword.trim() || filters.projectId || filters.usage ? '没有匹配的快照' : '还没有代码快照'"
      empty-hint="上传源码包生成不可变快照，扫描任务基于快照执行。"
    >
      <template #empty>
        <Button v-if="canOperate" variant="link" size="sm" style="margin-top: 8px" @click="pickFile">上传代码包 →</Button>
      </template>
    </DataTable>
    <CatalogPagination label="资产分页" :page="result?.page" :page-size="result?.pageSize" :total="result?.total" :loading="loading" :unavailable="!!loadError" @change="page" />
  </div>
</template>

<style scoped>
.project-select{width:180px}.usage-select{width:160px}
.toolbar {
  flex-wrap:wrap;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  margin-bottom: var(--space-3);
  min-height: var(--height-toolbar);
}
.search {
  width: 280px;
}
.total {
  font-size: 11px;
  color: var(--text-tertiary);
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
:deep(.op-btn:hover:not(:disabled)) {
  background: var(--surface-hover);
  color: var(--text-primary);
}
:deep(.op-btn:disabled) {
  opacity: 0.35;
  cursor: not-allowed;
}

.toolbar{flex-wrap:wrap}.filters{max-width:100%;overflow-x:auto;min-width:0}.toolbar-right{min-width:0}.search{max-width:100%}@media(max-width:760px){.toolbar-right{width:100%;margin-left:0;flex-shrink:1}.search{flex:1;min-width:0}.filters{width:100%}.total{font-size:var(--font-size-xs)}.sec-right{flex-wrap:wrap;min-width:0}.section-title-bar{gap:var(--space-3);flex-wrap:wrap}}
</style>
