<script setup lang="ts">
import { useAuth } from "../composables/auth";
const { canOperate, canAdmin } = useAuth();
import { h, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { Plus, Trash2 } from "lucide-vue-next";
import type { ColumnDef } from "@tanstack/vue-table";
import { api } from "../api";
import type { Project } from "../types";
import PageHeader from "../components/PageHeader.vue";
import DataTable from "../components/DataTable.vue";
import Button from "../components/ui/Button.vue";
import Input from "../components/ui/Input.vue";
import Dialog from "../components/ui/Dialog.vue";
import Tooltip from "../components/ui/Tooltip.vue";
import { message } from "../lib/toast";
import { confirm } from "../components/ui/confirm";
import { relativeTime, absoluteTime } from "../utils/time";

const router = useRouter();
const projects = ref<Project[]>([]);
const loading = ref(false);
const loadError = ref("");
const dialogOpen = ref(false);
const name = ref("");
const creating = ref(false);

async function load(): Promise<void> {
  if (loading.value) return;
  loading.value = true;
  loadError.value = "";
  try {
    projects.value = await api.listProjects();
  } catch (error) {
    loadError.value = (error as Error).message;
  } finally {
    loading.value = false;
  }
}

async function create(): Promise<void> {
  if (creating.value) return;
  if (!name.value.trim()) return;
  creating.value = true;
  try {
    await api.createProject(name.value.trim());
    name.value = "";
    dialogOpen.value = false;
    await load();
  } catch (error) {
    message.error((error as Error).message);
  } finally {
    creating.value = false;
  }
}

async function remove(row: Project): Promise<void> {
  const ok = await confirm({
    title: "删除项目",
    body: `确定删除项目「${row.name}」？其代码快照与扫描记录会保留，仅解除归属。`,
  });
  if (!ok) return;
  try {
    await api.deleteProject(row.id);
    message.success("已删除");
    await load();
  } catch (error) {
    message.error((error as Error).message);
  }
}

const columns: ColumnDef<Project>[] = [
  {
    accessorKey: "name",
    header: "项目",
    cell: (c) => h("span", { style: { fontWeight: 500, color: "var(--text-primary)" } }, c.getValue<string>()),
  },
  {
    accessorKey: "snapshotCount",
    header: "代码快照",
    size: 100,
    cell: (c) => h("span", { class: "count-num" + (c.getValue<number>() === 0 ? " zero" : "") }, String(c.getValue<number>())),
  },
  {
    accessorKey: "scanCount",
    header: "扫描次数",
    size: 100,
    cell: (c) => h("span", { class: "count-num" + (c.getValue<number>() === 0 ? " zero" : "") }, String(c.getValue<number>())),
  },
  {
    accessorKey: "createdAt",
    header: "创建时间",
    size: 140,
    cell: (c) =>
      h(Tooltip, { content: absoluteTime(c.getValue<string>()) }, () =>
        h("span", { style: { color: "var(--text-secondary)", fontSize: "12px" } }, relativeTime(c.getValue<string>())),
      ),
  },
  {
    id: "ops",
    header: "",
    size: 48,
    cell: ({ row }) =>
      canAdmin.value ? h(Tooltip, { content: "删除项目" }, () =>
        h(
          "button",
          {
            class: "op-btn",
              "aria-label": "删除记录",
            onClick: (e: Event) => {
              e.stopPropagation();
              void remove(row.original);
            },
          },
          h(Trash2, { size: 13, "stroke-width": 1.7 }),
        ),
      ) : null,
  },
];

onMounted(load);
</script>

<template>
  <div>
    <div v-if="loadError" class="request-error" role="alert"><span>数据加载失败：{{loadError}}</span><Button variant="outline" size="sm" @click="load">重试</Button></div>
    <PageHeader title="项目" description="组织与浏览被审计的代码资产">
      <Button v-if="canOperate" @click="dialogOpen = true">
        <Plus :size="14" />新建项目
      </Button>
    </PageHeader>

    <DataTable
      :loading="loading"
      :data="projects"
      :columns="columns"
      :row-click="(p) => router.push({ name: 'project-detail', params: { id: p.id } })"
      empty-title="还没有项目"
      empty-hint="项目用于组织被审计的代码资产。"
    />

    <Dialog v-model:open="dialogOpen" title="新建项目">
      <Input v-model="name" placeholder="项目名称" @enter="create" />
      <template #footer>
        <Button variant="secondary" @click="dialogOpen = false">取消</Button>
        <Button :loading="creating" @click="create">创建</Button>
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
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
