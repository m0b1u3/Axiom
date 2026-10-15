<script setup lang="ts">
import { useAuth } from "../composables/auth";
const { canOperate, canAdmin } = useAuth();
import { h, onMounted, onUnmounted, ref } from "vue";
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
import Tooltip from "../components/ui/Tooltip.vue";
import { message } from "../lib/toast";
import { confirm } from "../components/ui/confirm";
import { relativeTime, absoluteTime } from "../utils/time";
import { shortHash } from "../utils/format";

/* 项目详情：项目 → 代码快照（资产）→ 扫描记录 的归属链 */
const route = useRoute();
const router = useRouter();
const id = Number(route.params.id);

const project = ref<Project | null>(null);
const snapshots = ref<SnapshotInfo[]>([]);
const scans = ref<ScanTask[]>([]);
const loading = ref(false);
const loadError = ref("");
const uploading = ref(false);
const fileInput = ref<HTMLInputElement | null>(null);
const orphans = ref<SnapshotInfo[]>([]);
const attachId = ref("");
const attaching = ref(false);
let timer: number | undefined;

async function load(): Promise<void> {
  if (loading.value) return;
  loading.value = true;
  loadError.value = "";
  try {
    const [p, snaps, scs] = await Promise.all([
      api.getProject(id),
      api.listSnapshots(id),
      api.listScans(id),
    ]);
    project.value = p;
    snapshots.value = snaps;
    scans.value = scs;
    // 未归属任何项目的快照可关联进来（含其历史扫描）
    orphans.value = (await api.listSnapshots()).filter((s) => !s.projectId);
    if (attachId.value && !orphans.value.some((s) => String(s.id) === attachId.value)) {
      attachId.value = "";
    }
  } catch (error) {
    loadError.value = (error as Error).message;
  } finally {
    loading.value = false;
  }
}

function pickFile(): void {
  fileInput.value?.click();
}

async function onFilePicked(event: Event): Promise<void> {
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
    await load();
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

async function removeSnapshot(snap: SnapshotInfo): Promise<void> {
  const ok = await confirm({
    title: "删除代码快照",
    body: `确定删除快照「${snap.originalFilename}」？解压目录将一并清除。`,
  });
  if (!ok) return;
  try {
    await api.deleteSnapshot(snap.id);
    message.success("已删除");
    await load();
  } catch (error) {
    message.error((error as Error).message);
  }
}

async function attach(): Promise<void> {
  if (!attachId.value) return;
  attaching.value = true;
  try {
    await api.assignSnapshotProject(Number(attachId.value), id);
    message.success("已关联到本项目，其历史扫描一并归入");
    attachId.value = "";
    await load();
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
        h("span", { style: { fontWeight: 500, color: "var(--text-primary)" } }, row.original.originalFilename),
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
          h(Button, { variant: "ghost", size: "iconSm", onClick: () => scanSnapshot(s) }, () => h(ScanLine, { size: 13, "stroke-width": 1.7 })),
        ),
        h(Tooltip, { content: s.scanCount > 0 ? "有扫描引用，不可删除" : "删除快照" }, () =>
          h(
            Button,
            { variant: "ghost", size: "iconSm", class: "row-danger", disabled: !canAdmin.value || s.scanCount > 0, onClick: () => void removeSnapshot(s) },
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
  void load();
  timer = window.setInterval(() => void load(), 5000);
});
onUnmounted(() => {
  if (timer) window.clearInterval(timer);
});
</script>

<template>
  <div>
    <div v-if="loadError" class="request-error" role="alert"><span>数据加载失败：{{loadError}}</span><Button variant="outline" size="sm" @click="load">重试</Button></div>
    <PageHeader :title="project?.name ?? '项目'" description="代码资产与审计执行记录">
      <Button v-if="canOperate" variant="secondary" :loading="uploading" @click="pickFile">
        <Upload :size="14" />上传代码包
      </Button>
      <Button v-if="canOperate" @click="router.push({ name: 'new-scan', query: { projectId: String(id) } })">
        <Plus :size="14" />新建扫描
      </Button>
    </PageHeader>
    <input ref="fileInput" type="file" accept=".zip,.tar.gz,.tgz" style="display: none" @change="onFilePicked" />

    <!-- 代码快照（项目资产） -->
    <div class="section-title-bar">
      <span class="sec-label">代码快照</span>
      <span class="sec-right">
        <template v-if="orphans.length">
          <Select
            v-model="attachId"
            :options="orphans.map((s) => ({ value: String(s.id), label: `#${s.id}  ${s.originalFilename} · ${s.fileCount} 文件` }))"
            placeholder="关联已有快照…"
            style="width: 260px"
          />
          <Button size="sm" variant="secondary" :disabled="!canOperate || !attachId" :loading="attaching" @click="attach">
            关联
          </Button>
        </template>
        <span class="sec-count mono">{{ snapshots.length }} 个</span>
      </span>
    </div>
    <DataTable
      :loading="loading"
      :data="snapshots"
      :columns="snapColumns"
      empty-title="还没有代码快照"
      empty-hint="上传源码包生成不可变快照，扫描任务基于快照执行。"
    >
      <template #empty>
        <Button v-if="canOperate" variant="link" size="sm" style="margin-top: 8px" @click="pickFile">上传代码包 →</Button>
      </template>
    </DataTable>

    <!-- 项目扫描记录 -->
    <div class="section-title-bar" style="margin-top: var(--space-6)">
      <span class="sec-label">扫描记录</span>
      <span class="sec-count mono">{{ scans.length }} 次</span>
    </div>
    <DataTable
      :loading="loading"
      :data="scans"
      :columns="scanColumns"
      :row-click="(s) => router.push({ name: 'scan-detail', params: { id: s.id } })"
      empty-title="还没有扫描记录"
      empty-hint="选择一个代码快照发起扫描。"
    />
  </div>
</template>

<style scoped>
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
