<script setup lang="ts">
import ResourceCleanupDialog from "../components/ResourceCleanupDialog.vue";
import { useAuth } from "../composables/auth";
const { canOperate, canAdmin } = useAuth();
import { computed, h, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { Upload, Trash2, ScanLine, Search } from "lucide-vue-next";
import type { ColumnDef } from "@tanstack/vue-table";
import { api, type SnapshotInfo } from "../api";
import type { Project } from "../types";
import PageHeader from "../components/PageHeader.vue";
import DataTable from "../components/DataTable.vue";
import Button from "../components/ui/Button.vue";
import Input from "../components/ui/Input.vue";
import Tooltip from "../components/ui/Tooltip.vue";
import { message } from "../lib/toast";

import { relativeTime, absoluteTime } from "../utils/time";
import { shortHash } from "../utils/format";

/* 资产 = 全部代码快照的统一管理（跨项目视角） */
const router = useRouter();
const snapshots = ref<SnapshotInfo[]>([]);
const projects = ref<Project[]>([]);
const loading = ref(false);
const loadError = ref("");
const uploading = ref(false);
const keyword = ref("");
const fileInput = ref<HTMLInputElement | null>(null);

const projectName = computed(() => {
  const m = new Map(projects.value.map((p) => [p.id, p.name]));
  return (id: number) => (id > 0 ? (m.get(id) ?? "—") : "");
});

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase();
  if (!kw) return snapshots.value;
  return snapshots.value.filter(
    (s) =>
      s.originalFilename.toLowerCase().includes(kw) ||
      s.archiveSha256.toLowerCase().includes(kw) ||
      projectName.value(s.projectId).toLowerCase().includes(kw),
  );
});

async function load(): Promise<void> {
  if (loading.value) return;
  loading.value = true;
  loadError.value = "";
  try {
    const [snaps, projs] = await Promise.all([api.listSnapshots(), api.listProjects()]);
    snapshots.value = snaps;
    projects.value = projs;
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
    size: 64,
    cell: ({ row }) => {
      const s = row.original;
      if (!canOperate.value) return null;
      return h("span", { style: { display: "flex", gap: "2px", justifyContent: "flex-end" } }, [
        h(Tooltip, { content: "用此快照新建扫描" }, () =>
          h(
            "button",
            {
              class: "op-btn",
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

onMounted(load);
</script>

<template>
  <ResourceCleanupDialog v-model:open="cleanupOpen" kind="snapshot" :resource-id="cleanupSnapshot" @completed="snapshotDeleted" />
  <div>
    <div v-if="loadError" class="request-error" role="alert"><span>数据加载失败：{{loadError}}</span><Button variant="outline" size="sm" @click="load">重试</Button></div>
    <PageHeader title="资产" description="全部代码快照的统一管理（跨项目）">
      <Button v-if="canOperate" variant="secondary" :loading="uploading" @click="pickFile">
        <Upload :size="14" />上传代码包
      </Button>
    </PageHeader>
    <input ref="fileInput" type="file" accept=".zip,.tar.gz,.tgz" style="display: none" @change="onFilePicked" />

    <div class="toolbar">
      <Input v-model="keyword" class="search" placeholder="搜索文件名 / 哈希 / 项目…" />
      <span class="total mono">{{ filtered.length }} 个快照</span>
    </div>

    <DataTable
      :loading="loading"
      :data="filtered"
      :columns="columns"
      :empty-title="snapshots.length ? '没有匹配的快照' : '还没有代码快照'"
      empty-hint="上传源码包生成不可变快照，扫描任务基于快照执行。"
    >
      <template #empty>
        <Button v-if="canOperate" variant="link" size="sm" style="margin-top: 8px" @click="pickFile">上传代码包 →</Button>
      </template>
    </DataTable>
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
