<script setup lang="ts">
import { useAuth } from "../composables/auth";
const { canOperate, canAdmin } = useAuth();
import { h, computed, watch, onMounted, onUnmounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Plus, Upload, Trash2, ScanLine } from "lucide-vue-next";
import type { ColumnDef } from "@tanstack/vue-table";
import { api, type SnapshotInfo } from "../api";
import type { Project, ScanTask } from "../types";
import PageHeader from "../components/PageHeader.vue";
import StatusBadge from "../components/StatusBadge.vue";
import DataTable from "../components/DataTable.vue";
import Button from "../components/ui/Button.vue";
import Select from "../components/ui/Select.vue";
import Input from "../components/ui/Input.vue";
import CatalogPagination from "../components/CatalogPagination.vue";
import {useCatalog} from "../composables/catalog";
import Tooltip from "../components/ui/Tooltip.vue";
import { message } from "../lib/toast";
import ResourceCleanupDialog from "../components/ResourceCleanupDialog.vue";
import { relativeTime, absoluteTime } from "../utils/time";
import { shortHash } from "../utils/format";

/* 项目详情：项目 → 代码快照（资产）→ 扫描记录 的归属链 */
const route = useRoute();
const router = useRouter();
const id = Number(route.params.id);

const project = ref<Project | null>(null);
const {result:snapshotResult,search:snapshotSearch,loading:snapshotLoading,error:snapshotError,sorting:snapshotSorting,load:reloadSnapshots,sort:sortSnapshots,page:changeSnapshotPage} = useCatalog<SnapshotInfo>(query=>api.snapshotPage({...query,projectId:id}), [], 'id','desc');
const snapshots=computed(()=>snapshotResult.value?.items ?? []);
const scans = ref<ScanTask[]>([]);
const loading = ref(false);
const loadError = ref("");
const uploading = ref(false);
const fileInput = ref<HTMLInputElement | null>(null);
const orphans = ref<SnapshotInfo[]>([]);
const attachId = ref("");
const attaching = ref(false);
const orphanSearch=ref(''), orphanLoading=ref(false), orphanError=ref('');
const orphanPage=ref(1),orphanTotal=ref<number>();
let orphanGeneration=0, orphanTimer:ReturnType<typeof setTimeout> | undefined;
async function loadOrphans(page=orphanPage.value) {
  if(!canOperate.value)return;
  const gen=++orphanGeneration;orphanLoading.value=true;orphanError.value='';
  try {
    const [list,pinned]=await Promise.all([api.snapshotPage({projectId:0,q:orphanSearch.value,page}),attachId.value ? api.snapshotPage({projectId:0,id:Number(attachId.value)}) : Promise.resolve(null)]);
    if(gen!==orphanGeneration)return;
    orphans.value=list.items;orphanPage.value=list.page;orphanTotal.value=list.total;
    const selected=pinned?.items[0];
    if(selected && !orphans.value.some(s=>s.id===selected.id))orphans.value=[selected,...orphans.value];
    if(attachId.value && !selected)attachId.value='';
  }catch(error){if(gen===orphanGeneration)orphanError.value=(error as Error).message;}
  finally{if(gen===orphanGeneration)orphanLoading.value=false;}
}
watch(orphanSearch,()=>{orphanGeneration++;orphanLoading.value=true;clearTimeout(orphanTimer);orphanTimer=setTimeout(()=>void loadOrphans(1),250)});
let timer: number | undefined;

let loadAgain = false;
async function load(): Promise<void> {
  if (loading.value) { loadAgain = true; return; }
  loading.value = true;
  loadError.value = "";
  try {
    const [p, scs] = await Promise.all([
      api.getProject(id),
      api.listScans(id),
    ]);
    project.value = p;
    scans.value = scs;
  } catch (error) {
    loadError.value = (error as Error).message;
  } finally {
    loading.value = false;
    if (loadAgain) { loadAgain = false; void load(); }
  }
}

function pickFile(): void {
  fileInput.value?.click();
}

async function onFilePicked(event: Event): Promise<void> {
  if (!canOperate.value || uploading.value) return;
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
    const snap = await api.uploadSnapshot(file, id);
    message.success(`已上传 ${snap.originalFilename}（${snap.fileCount} 个文件）`);
    await Promise.all([load(),reloadSnapshots(),loadOrphans()]);
  } catch (error) {
    message.error((error as Error).message);
  } finally {
    uploading.value = false;
    input.value = "";
  }
}

function scanSnapshot(snap: SnapshotInfo): void {
  void router.push({ name: "new-scan", query: { projectId: String(id), snapshotId: String(snap.id) } });
}

const cleanupOpen = ref(false), cleanupSnapshot = ref(0);
function removeSnapshot(snap: SnapshotInfo): void { cleanupSnapshot.value = snap.id; cleanupOpen.value = true; }
async function snapshotDeleted(): Promise<void> { message.success("已删除代码快照与目录"); await Promise.all([load(),reloadSnapshots(),loadOrphans()]); }

async function attach(): Promise<void> {
  if (!canOperate.value || attaching.value || !attachId.value || orphanLoading.value || !!orphanError.value) return;
  attaching.value = true;
  try {
    await api.assignSnapshotProject(Number(attachId.value), id);
    message.success("已关联到本项目，其历史扫描一并归入");
    attachId.value = "";
    await Promise.all([load(),reloadSnapshots(),loadOrphans()]);
  } catch (error) {
    message.error((error as Error).message);
  } finally {
    attaching.value = false;
  }
}

function formatSize(bytes: number): string {
  if (bytes >= 1 << 20) return (bytes / (1 << 20)).toFixed(1) + " MB";
  if (bytes >= 1 << 10) return (bytes / (1 << 10)).toFixed(1) + " KB";
  return bytes + " B";
}

const snapColumns: ColumnDef<SnapshotInfo>[] = [
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
    size: 64,
    cell: ({ row }) => {
      const s = row.original;
      if (!canOperate.value) return null;
      return h("span", { style: { display: "flex", gap: "2px", justifyContent: "flex-end" } }, [
        h(Tooltip, { content: "用此快照新建扫描" }, () =>
          h(Button, { variant: "ghost", size: "iconSm", "aria-label": `扫描快照 ${s.originalFilename}`, onClick: () => scanSnapshot(s) }, () => h(ScanLine, { size: 13, "stroke-width": 1.7 })),
        ),
        h(Tooltip, { content: s.scanCount > 0 ? "有扫描引用，不可删除" : "删除快照" }, () =>
          h(
            Button,
            { variant: "ghost", size: "iconSm", class: "row-danger", "aria-label": `删除快照 ${s.originalFilename}`, disabled: !canAdmin.value || s.scanCount > 0, onClick: () => void removeSnapshot(s) },
            () => h(Trash2, { size: 13, "stroke-width": 1.7 }),
          ),
        ),
      ]);
    },
  },
];

const scanColumns: ColumnDef<ScanTask>[] = [
  {
    accessorKey: "scanNo",
    header: "扫描编号",
    size: 190,
    cell: (c) => h("span", { class: "mono", style: { fontWeight: 600, color: "var(--text-primary)" } }, c.getValue<string>()),
  },
  {
    id: "model",
    header: "模型",
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
    size: 90,
    cell: (c) => h("span", { class: "count-num" + (c.getValue<number>() === 0 ? " zero" : "") }, String(c.getValue<number>())),
  },
  {
    accessorKey: "status",
    header: "任务状态",
    size: 110,
    cell: (c) => h(StatusBadge, { status: c.getValue<string>() }),
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
];

onMounted(() => {
  void load(); void loadOrphans();
  timer = window.setInterval(() => void load(), 5000);
});
onUnmounted(() => {
  orphanGeneration++;clearTimeout(orphanTimer);
  if (timer) window.clearInterval(timer);
});
</script>

<template>
  <div>
    <div v-if="loadError" class="request-error" role="alert"><span>数据加载失败：{{loadError}}</span><Button variant="outline" size="sm" @click="load">重试</Button></div>
    <PageHeader :title="project?.name ?? '项目'" description="代码资产与审计执行记录">
      <Button v-if="canOperate && project" variant="secondary" :loading="uploading" @click="pickFile">
        <Upload :size="14" />上传代码包
      </Button>
      <Button v-if="canOperate && project" @click="router.push({ name: 'new-scan', query: { projectId: String(id) } })">
        <Plus :size="14" />新建扫描
      </Button>
    </PageHeader>
    <input ref="fileInput" type="file" accept=".zip,.tar.gz,.tgz" style="display: none" @change="onFilePicked" />

    <p v-if="loading && !project" role="status">正在读取项目与代码快照…</p>
    <template v-if="project">
    <!-- 代码快照（项目资产） -->
    <div class="section-title-bar">
      <span class="sec-label">代码快照</span>
      <span class="sec-right">
        <span class="sec-count mono">{{ snapshotResult?.total ?? '—' }} 个</span>
      </span>
    </div>
    <Input v-model="snapshotSearch" aria-label="搜索项目代码快照" placeholder="搜索本项目的全部快照" maxlength="256" />
    <div v-if="snapshotError" class="request-error" role="alert">项目快照读取失败：{{snapshotError}}<Button variant="outline" size="sm" @click="reloadSnapshots">重读项目快照</Button></div>
    <DataTable
      :loading="snapshotLoading"
      :unavailable="!!snapshotError"
      manual-sorting :sorting="snapshotSorting" @update:sorting="sortSnapshots"
      :data="snapshots"
      :columns="snapColumns"
      empty-title="还没有代码快照"
      empty-hint="上传源码包生成不可变快照，扫描任务基于快照执行。"
    >
      <template #empty>
        <Button v-if="canOperate" variant="link" size="sm" style="margin-top: 8px" @click="pickFile">上传代码包 →</Button>
      </template>
    </DataTable>
    <CatalogPagination label="项目快照分页" :page="snapshotResult?.page" :page-size="snapshotResult?.pageSize" :total="snapshotResult?.total" :loading="snapshotLoading" :unavailable="!!snapshotError" @change="changeSnapshotPage" />
    <section v-if="canOperate" class="snapshot-attach" aria-label="关联代码快照">
      <h3>关联已有快照</h3>
      <p class="upload-hint">选择尚未归属项目的快照，其历史扫描会一并归入。</p>
      <Input v-model="orphanSearch" aria-label="搜索未归属快照" placeholder="搜索全部未归属快照" maxlength="256" />
      <div v-if="orphanError" class="request-error" role="alert">未归属快照读取失败：{{orphanError}}<Button variant="outline" size="sm" @click="loadOrphans()">重读未归属快照</Button></div>
      <div class="attach-controls">
        <Select v-model="attachId" aria-label="待关联快照" :options="orphans.map(s=>({value:String(s.id),label:`#${s.id}  ${s.originalFilename} · ${s.fileCount} 文件`}))" clearable :disabled="orphanLoading || !!orphanError" placeholder="选择未归属快照…" />
        <Button size="sm" variant="secondary" :disabled="!attachId || orphanLoading || !!orphanError" :loading="attaching" @click="attach">关联</Button>
      </div>
      <CatalogPagination label="未归属快照分页" :page="orphanTotal === undefined ? undefined : orphanPage" :total="orphanTotal" :loading="orphanLoading" :unavailable="!!orphanError" @change="loadOrphans" />
    </section>

    <!-- 项目扫描记录 -->
    <div class="section-title-bar" style="margin-top: var(--space-6)">
      <span class="sec-label">扫描记录</span>
      <span class="sec-count mono">{{ scans.length }} 次</span>
    </div>
    <DataTable
      :loading="loading"
      :unavailable="!!loadError"
      :data="scans"
      :columns="scanColumns"
      :row-click="(s) => router.push({ name: 'scan-detail', params: { id: s.id } })"
      empty-title="还没有扫描记录"
      empty-hint="选择一个代码快照发起扫描。"
    />
    </template>
    <ResourceCleanupDialog v-model:open="cleanupOpen" kind="snapshot" :resource-id="cleanupSnapshot" @completed="snapshotDeleted" />
  </div>
</template>

<style scoped>
.snapshot-attach{border-top:1px solid var(--border-default);padding-block:var(--space-4);max-width:var(--content-max-width)}.snapshot-attach h3{font-size:var(--font-size-sm);font-weight:var(--font-weight-medium)}.attach-controls{display:flex;align-items:center;gap:var(--space-2);margin-top:var(--space-2)}.attach-controls>:first-child{flex:1;min-width:0}
.section-title-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: var(--space-2);
}
.sec-label {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
}
.sec-count {
  font-size: 11px;
  color: var(--text-tertiary);
}
.sec-right {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.toolbar{flex-wrap:wrap}.filters{max-width:100%;overflow-x:auto;min-width:0}.toolbar-right{min-width:0}.search{max-width:100%}@media(max-width:760px){.toolbar-right{width:100%;margin-left:0;flex-shrink:1}.search{flex:1;min-width:0}.filters{width:100%}.total{font-size:var(--font-size-xs)}.sec-right{flex-wrap:wrap;min-width:0}.section-title-bar{gap:var(--space-3);flex-wrap:wrap}}
:deep(.row-danger) {
  color: var(--state-danger);
}
</style>
