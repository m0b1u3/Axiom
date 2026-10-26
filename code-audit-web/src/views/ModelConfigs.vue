<script setup lang="ts">
import { useAuth } from "../composables/auth";
const { canAdmin } = useAuth();
import { computed, h, onMounted, reactive, ref } from "vue";
import { Plus, Trash2, Pencil, Plug, SlidersHorizontal, ChevronDown, Eye, EyeOff, Check, Cpu, Search, Lock } from "lucide-vue-next";
import type { ColumnDef } from "@tanstack/vue-table";
import { api, type ModelConfig, type ModelConfigPayload, type ProviderPreset } from "../api";
import PageHeader from "../components/PageHeader.vue";
import DataTable from "../components/DataTable.vue";
import Button from "../components/ui/Button.vue";
import Input from "../components/ui/Input.vue";
import Select from "../components/ui/Select.vue";
import Dialog from "../components/ui/Dialog.vue";
import Switch from "../components/ui/Switch.vue";
import DropdownMenu from "../components/ui/DropdownMenu.vue";
import { message } from "../lib/toast";
import { confirm } from "../components/ui/confirm";
import iconDeepseek from "../assets/providers/deepseek.svg";
import iconKimi from "../assets/providers/kimi.svg";
import iconQwen from "../assets/providers/qwen.svg";
import iconZhipu from "../assets/providers/zhipu.svg";
import iconOpenai from "../assets/providers/openai.svg";
withDefaults(defineProps<{embedded?:boolean}>(),{embedded:false});

/* 品牌图标（cc-switch 同款 SVG）；custom/未知 → lucide Plug */
const ICONS: Record<string, string> = {
  deepseek: iconDeepseek,
  kimi: iconKimi,
  qwen: iconQwen,
  zhipu: iconZhipu,
  openai: iconOpenai,
};
function iconOf(p: ProviderPreset | undefined): string {
  return (p && ICONS[p.icon]) || "";
}

const items = ref<ModelConfig[]>([]);
const presets = ref<ProviderPreset[]>([]);
const loading = ref(false);
const keyword = ref("");
const enabledFilter = ref("all");
const enabledOptions = [{label:"全部状态",value:"all"},{label:"工作模型",value:"enabled"},{label:"未启用",value:"disabled"}];
const showKey = ref(false);
const loadError = ref("");
const presetsError = ref("");
const formError = ref("");
const filtered = computed(() => items.value.filter(x => (enabledFilter.value === "all" || x.enabled === (enabledFilter.value === "enabled")) && (x.name+" "+x.provider+" "+x.model).toLowerCase().includes(keyword.value.trim().toLowerCase())));
const dialogOpen = ref(false);
const editing = ref(false);
const editingId = ref(0);
const saving = ref(false);
const enablingId = ref(0);

const reasoningOptions=[{value:"sdk-default",label:"沿用 SDK 默认"},...["none","minimal","low","medium","high","xhigh"].map(value=>({value,label:value}))];
 const form = reactive({
 reasoningEffort:"sdk-default",
 sdkIdentityOnly:false,
  name: "",
  remark: "",
  provider: "",
  websiteUrl: "",
  apiKey: "",
  baseUrl: "",
  model: "",
  wireApi: "",
});
const editingKeyHint = ref("");
const fetchedModels = ref<string[]>([]);
const fetchingModels = ref(false);
const probing = ref(false);
const probeResult = ref<{ ok: boolean; msg: string } | null>(null);

const presetOf = computed(() => presets.value.find((p) => p.key === form.provider));
const presetLabel = (key: string): string => presets.value.find((p) => p.key === key)?.label ?? key;

/* 下拉只展示点「获取模型列表」之后的在线结果，不把预设提示混进去 */
const fetchedModelItems = computed(() => fetchedModels.value.map((m) => ({ key: m, label: m })));

/* 「测试连接」：编辑走已存配置探测；新建用表单值拉一次模型列表作连通性探针 */
async function probeConnection(): Promise<void> {
  if (probing.value) return;
  if (form.sdkIdentityOnly) {
    probeResult.value = { ok: false, msg: "此模型使用授权账户。请在“模型与授权”的“授权账户”中查询账户状态。" };
    return;
  }
  probing.value = true;
  probeResult.value = null;
  try {
    if (editing.value) {
      const r = await api.testModelConfig(editingId.value);
      probeResult.value = r.ok ? { ok: true, msg: r.message || "接口连接成功；SDK 工具和完整复核能力尚未验证。" } : { ok: false, msg: r.error || "连接失败" };
    } else {
      if (!form.provider) {
        probeResult.value = { ok: false, msg: "请先选择供应商" };
        return;
      }
      if (!form.apiKey.trim()) {
        probeResult.value = { ok: false, msg: "请先填写 API Key" };
        return;
      }
      const r = await api.fetchProviderModels({
        provider: form.provider,
        baseUrl: form.baseUrl.trim(),
        apiKey: form.apiKey.trim(),
      });
      probeResult.value = r.ok
        ? { ok: true, msg: `已获取 ${r.models?.length ?? 0} 个模型；SDK 工具和完整复核能力尚未验证。` }
        : { ok: false, msg: r.error || "连接失败" };
    }
  } catch (error) {
    probeResult.value = { ok: false, msg: (error as Error).message };
  } finally {
    probing.value = false;
  }
}

/* 点预设卡片：切换供应商时覆盖仍是「上一档预填」的字段；用户改过的名称/型号保留 */
function pickPreset(key: string): void {
  const prev = presets.value.find((x) => x.key === form.provider);
  const next = presets.value.find((x) => x.key === key);
  const nameIsPreset = !form.name.trim() || form.name.trim() === (prev?.label ?? "");
  form.provider = key;
  fetchedModels.value = [];
  probeResult.value = null;
  if (editing.value) return;
  form.baseUrl = next?.baseUrl ?? "";
  form.websiteUrl = next?.website ?? "";
  form.wireApi = next?.wireApi ?? "";
  if (nameIsPreset) form.name = next?.label ?? "";
}

/* 「获取模型列表」：服务端代理 GET {modelsUrl}，用表单里的 key（编辑时可复用已存 key） */
async function fetchModelList(): Promise<void> {
  if (fetchingModels.value) return;
  if (!form.provider) {
    message.warning("请先选择供应商");
    return;
  }
  if (!editing.value && !form.apiKey.trim()) {
    message.warning("请先填写 API Key");
    return;
  }
  fetchingModels.value = true;
  try {
    const r = await api.fetchProviderModels({
      provider: form.provider,
      baseUrl: form.baseUrl.trim(),
      ...(form.apiKey.trim() ? { apiKey: form.apiKey.trim() } : {}),
      ...(editing.value ? { modelConfigId: editingId.value } : {}),
    });
    if (r.ok && r.models) {
      fetchedModels.value = r.models;
      message.success(`获取到 ${r.models.length} 个模型，点「获取列表」下拉选择`);
    } else {
      message.warning(r.error || "获取模型列表失败");
    }
  } catch (error) {
    message.warning((error as Error).message);
  } finally {
    fetchingModels.value = false;
  }
}

let loadAgain = false;
async function load(): Promise<void> {
  if (loading.value) { loadAgain = true; return; }
  loading.value = true;
  try {
    items.value = await api.listModelConfigs();
    loadError.value = "";
  } catch (error) {
    loadError.value = (error as Error).message;
  } finally {
    loading.value = false;
    if (loadAgain) { loadAgain = false; void load(); }
  }
}

async function loadPresets(): Promise<void> {
  presetsError.value = "";
  try {
    presets.value = await api.listModelProviders();
  } catch (error) {
    presetsError.value = `模型供应商读取失败：${(error as Error).message}`;
  }
}

function openCreate(): void {
  formError.value = "";
  showKey.value = false;
  editing.value = false;
  editingId.value = 0;
  editingKeyHint.value = "";
  Object.assign(form, { sdkIdentityOnly:false,name: "", remark: "", provider: "", websiteUrl: "", apiKey: "", baseUrl: "", model: "", wireApi: "", reasoningEffort:"sdk-default" });
  fetchedModels.value = [];
  probeResult.value = null;
  dialogOpen.value = true;
}

function openEdit(row: ModelConfig): void {
  formError.value = "";
  showKey.value = false;
  editing.value = true;
  editingId.value = row.id;
  editingKeyHint.value = row.apiKeyHint ?? "";
 form.sdkIdentityOnly=!!row.sdkIdentityOnly;
  Object.assign(form, {
    name: row.name,
    remark: row.remark ?? "",
    provider: row.provider,
    websiteUrl: row.websiteUrl ?? "",
    apiKey: "",
    baseUrl: row.baseUrl,
    model: row.model,
    wireApi: row.wireApi ?? "",
 reasoningEffort:row.reasoningEffort??"sdk-default",
  });
  fetchedModels.value = [];
  probeResult.value = null;
  dialogOpen.value = true;
}

async function save(): Promise<void> {
  if (!canAdmin.value || saving.value) return;
  formError.value = "";
  if (!form.provider) {
    formError.value = "请选择供应商。";
    return;
  }
  if (!form.name.trim() || !form.model.trim() || (!editing.value && !form.apiKey && !form.sdkIdentityOnly)) {
    formError.value = "名称、型号必填；新建需提供 API Key 或使用授权账户。";
    return;
  }
  if (form.provider !== "openai" && !form.baseUrl.trim()) {
    formError.value = "请填写 Base URL。";
    return;
  }
  saving.value = true;
  try {
    const payload: ModelConfigPayload = {
 ...(!editing.value&&form.sdkIdentityOnly?{sdkIdentityOnly:true}:{}),
      name: form.name.trim(),
      provider: form.provider,
      baseUrl: form.baseUrl.trim(),
      model: form.model.trim(),
      remark: form.remark.trim(),
      websiteUrl: form.websiteUrl.trim(),
      wireApi: "responses",
 reasoningEffort:form.reasoningEffort==="sdk-default"?"":form.reasoningEffort,
      ...(form.apiKey ? { apiKey: form.apiKey } : {}),
    };
    if (editing.value) {
      await api.updateModelConfig(editingId.value, payload);
    } else {
      await api.createModelConfig(payload);
    }
    dialogOpen.value = false;
    await load();
  } catch (error) {
    formError.value = `模型配置保存失败：${(error as Error).message}`;
  } finally {
    saving.value = false;
  }
}

/* 启用开关：开 = 设为工作模型（独占）；关 = 停用默认模型 */
async function toggleEnable(row: ModelConfig, enabled: boolean): Promise<void> {
  if (!canAdmin.value || enablingId.value || row.enabled === enabled) return;
  enablingId.value = row.id;
  try {
    await api.enableModelConfig(row.id, enabled);
    message.success(enabled ? `已启用「${row.name}」，新建扫描默认使用它` : `已停用「${row.name}」，新建扫描需手动选择模型`);
    await load();
  } catch (error) {
    message.error((error as Error).message);
  } finally {
    enablingId.value = 0;
  }
}

async function remove(row: ModelConfig): Promise<void> {
  const ok = await confirm({
    title: "删除模型配置",
    body: row.enabled ? `「${row.name}」当前处于启用状态，删除后新建扫描将没有默认模型。确定删除？` : `确定删除「${row.name}」？`,
  });
  if (!ok) return;
  try {
    await api.deleteModelConfig(row.id);
    await load();
  } catch (error) {
    message.error((error as Error).message);
  }
}

const columns: ColumnDef<ModelConfig>[] = [
  {
    accessorKey: "name",
    header: "名称",
    cell: ({ row }) => {
      const r=row.original, preset=presets.value.find(p=>p.key===r.provider), icon=iconOf(preset);
      return h("div", {class:"model-identity"}, [h("span",{class:"model-logo"},icon?h("img",{src:icon,alt:""}):h(Plug,{size:20})),h("span",{class:"model-identity-copy"},[h("strong",r.name),h("small",r.remark||presetLabel(r.provider))])]);
    },
  },
  {
    accessorKey: "model",
    header: "模型",
    cell: (c) =>
      h("div",{class:"model-identity-copy"},[h("span",{class:"mono"},c.getValue<string>()),h("small",`推理强度 · ${c.row.original.reasoningEffort??"SDK 默认"}`)]),
  },
  { accessorKey:"updatedAt", header:"更新时间", size:150, cell:c=>h("span",{class:"model-time"}, new Date(c.getValue<string>()).toLocaleString("zh-CN",{hour12:false})) },
  {
    id: "actions",
    header: () => h("span", { style: { display: "block", textAlign: "right" } }, "操作"),
    size: 210,
    cell: ({ row }) => {
      const r = row.original;
      if (!canAdmin.value) return h("span", {class: "text-xs text-muted-foreground"}, "仅管理员可编辑");
      return h("span", { style: { display: "flex", gap: "4px", justifyContent: "flex-end", alignItems: "center" } }, [
        h(
          "span",
          { title: "工作模型开关：新建扫描默认使用启用的配置（独占，启用一条会停用其他）" },
          [
            h(Switch, {
              checked: r.enabled,
              size: "sm",
              "aria-label": `设为工作模型 ${r.name}`,
              disabled: enablingId.value !== 0,
              onChange: (v: boolean) => void toggleEnable(r, v),
            }),
          ],
        ),
        h(
          Button,
          { variant: "ghost", size: "sm", title: "查看 / 编辑模型配置", onClick: () => openEdit(r) },
          () => [h(Pencil, { size: 12, "stroke-width": 1.8 }), h("span", "编辑")],
        ),
        h(
          Button,
          { variant: "ghost", size: "sm", class: "row-danger", title: "删除模型配置", onClick: () => void remove(r) },
          () => [h(Trash2, { size: 12, "stroke-width": 1.8 }), h("span", "删除")],
        ),
      ]);
    },
  },
];

onMounted(() => {
  void load();
  void loadPresets();
});
</script>

<template>
  <div>
    <header v-if="embedded" class="model-section-header"><h3>模型配置</h3><Button v-if="canAdmin" @click="openCreate"><Plus :size="14" :stroke-width="2"/>添加模型</Button></header>
    <PageHeader v-else title="模型配置" description="配置模型供应商与分析引擎，为代码审计和漏洞研判选择合适的模型。">
      <Button v-if="canAdmin" @click="openCreate">
        <Plus :size="14" :stroke-width="2" />添加模型
      </Button>
    </PageHeader>

    <div class="model-summary"><span>{{items.length}} 个模型配置</span><span>{{items.filter(x=>x.enabled).length}} 个工作模型</span><small>启用一个工作模型，作为新建扫描的默认配置</small></div>
    <div v-if="loadError" class="request-error" role="alert"><span>{{loadError}}</span><Button variant="outline" size="sm" @click="load">重试</Button></div>
    <div v-if="presetsError" class="request-error" role="alert"><span>{{presetsError}}</span><Button variant="outline" size="sm" @click="loadPresets">重新读取供应商</Button></div>
    <div class="model-toolbar"><Search :size="14"/><Input v-model="keyword" placeholder="搜索模型名称、供应商或型号…" aria-label="搜索模型配置"/><Select v-model="enabledFilter" :options="enabledOptions" aria-label="筛选模型状态" class="model-status-filter"/></div>
    <DataTable
      :loading="loading"
      :unavailable="!!loadError"
      :data="filtered"
      :columns="columns"
      :empty-title="items.length ? '没有匹配的模型' : '还没有模型配置'"
      :empty-hint="items.length ? '调整搜索词或状态筛选，查看其他配置。' : '添加供应商与模型型号，填写 API Key，或使用授权账户提供凭据。'"
    />

    <Dialog
      v-model:open="dialogOpen"
      :title="editing ? '编辑模型配置' : '添加模型'"
      description="选择模型供应商，并填写连接与模型配置。"
      width="760px"
    >
      <div class="dlg">
        <p v-if="formError" class="request-error full" role="alert">{{formError}}</p>
        <div v-if="!editing" class="provider-label">模型供应商</div>
        <div v-if="!editing" class="providers">
          <button
            v-for="p in presets"
            :key="p.key"
            type="button"
            class="provider"
            :class="{ on: form.provider === p.key }"
            :aria-pressed="form.provider === p.key"
            @click="pickPreset(p.key)"
          >
            <img v-if="iconOf(p)" :src="iconOf(p)" alt="" />
            <SlidersHorizontal v-else :size="24" :stroke-width="1.4" />
            <span>{{ p.label }}</span><Check v-if="form.provider === p.key" class="selected-check" :size="11"/>
          </button>
        </div>
        <div v-else class="edit-line">
          <img v-if="iconOf(presetOf)" :src="iconOf(presetOf)" alt="" />
          <SlidersHorizontal v-else :size="18" :stroke-width="1.6" />
          <span>{{ presetLabel(form.provider) }}</span>
        </div>

        <label class="field">
          <span class="lab">名称</span>
          <Input v-model="form.name" placeholder="给这条配置起个名字" />
          <span class="hint">用于在应用中标识该模型配置。</span>
        </label>

        <label v-if="!editing&&form.provider==='openai'" class="field full"><span><input type="checkbox" v-model="form.sdkIdentityOnly" @change="form.apiKey=''"/> 扫描时使用授权账户提供凭据</span><span class="hint">模型只保存型号和接口地址；创建扫描时选择已登录的授权账户。</span></label>
        <label v-if="!form.sdkIdentityOnly||editing" class="field">
          <span class="lab">API Key <button type="button" class="key-toggle" :aria-label="showKey?'隐藏 API Key':'显示 API Key'" @click="showKey=!showKey"><EyeOff v-if="showKey" :size="14"/><Eye v-else :size="14"/></button></span>
          <div class="inbox">
            <Input
              v-model="form.apiKey"
              :type="showKey?'text':'password'"
              :placeholder="editing && editingKeyHint ? `已保存 ${editingKeyHint}` : 'sk-…'"
              autocomplete="new-password"
            />
            <a v-if="presetOf?.keyUrl" class="inbox-btn" :href="presetOf.keyUrl" target="_blank" rel="noopener">获取</a>
          </div>
          <span class="hint">{{editing ? "留空保留已保存的凭据。" : "填写供应商提供的 API Key，提交后由服务端保存。"}}</span>
        </label>

        <label class="field full">
          <span class="lab">接口地址</span>
          <div class="inbox">
            <Input
              v-model="form.baseUrl"
              placeholder="https://api.example.com/v1"
              :disabled="form.provider === 'openai'"
            />
            <button v-if="!form.sdkIdentityOnly" type="button" class="inbox-btn" :disabled="probing" @click="probeConnection">
              {{ probing ? "测试中…" : editing ? "测试已存配置" : "测试连接" }}
            </button>
          </div>
          <span v-if="probeResult" class="hint" :role="probeResult.ok ? 'status' : 'alert'" :class="{ ok: probeResult.ok, err: !probeResult.ok }">{{ probeResult.msg }}</span>
          <span v-else-if="form.sdkIdentityOnly" class="hint">登录与账户状态在“模型与授权”的“授权账户”页签管理。</span>
          <span v-else class="hint">填写模型服务的接口地址。</span>
        </label>

        <label class="field">
          <span class="lab">模型</span>
          <div class="field-row">
            <Input v-model="form.model" placeholder="模型 ID" />
            <DropdownMenu
              v-if="fetchedModels.length"
              :items="fetchedModelItems"
              @select="(k: string) => (form.model = k)"
            >
              <Button variant="outline" :title="`已获取 ${fetchedModels.length} 个型号，点击选择`">
                获取列表<ChevronDown :size="13" :stroke-width="2" class="caret" />
              </Button>
            </DropdownMenu>
            <Button v-else variant="outline" :loading="fetchingModels" @click="fetchModelList">获取列表</Button>
          </div>
          <span v-if="presetOf?.modelExamples" class="hint">例如：{{ presetOf.modelExamples }} 等。</span>
          <span v-else class="hint">填写服务商提供的模型 ID。</span>
        </label>

        <label class="field full" for="model-reasoning-effort"><span class="lab">推理强度</span><Select id="model-reasoning-effort" v-model="form.reasoningEffort" :options="reasoningOptions" :disabled="saving" placeholder="沿用 SDK 默认" aria-label="推理强度"/><span class="hint">沿用默认时不覆盖 SDK 配置。供应商和模型须支持所选强度；连接测试不验证其生效情况。</span></label>
 <div class="split full">
          <label class="field">
            <span class="lab">官网</span>
            <Input v-model="form.websiteUrl" placeholder="https://" />
            <span class="hint">该服务商的官方网站。</span>
          </label>
          <label class="field">
            <span class="lab">协议</span>
            <span>Responses</span>
            <span v-if="form.wireApi && form.wireApi !== 'responses'" class="hint">此配置使用已移除的 Chat 协议。请填写支持 Responses 的接口地址后保存。</span>
            <span v-else class="hint">模型接口须支持 Responses。</span>
          </label>
        </div>

        <label class="field full">
          <span class="lab">备注</span>
          <Input v-model="form.remark" placeholder="可选" />
          <span class="hint">添加备注信息，便于后续管理。</span>
        </label>
      </div>

      <template #footer>
        <Button variant="secondary" @click="dialogOpen = false">取消</Button>
        <Button :loading="saving" @click="save">{{ editing ? "保存" : "添加" }}</Button>
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.model-section-header{display:flex;justify-content:space-between;align-items:center;gap:var(--space-3);margin-bottom:var(--space-4)}.model-section-header h3{margin:0;font-size:var(--font-size-md);font-weight:var(--font-weight-medium)}
.dlg {
  display: grid;
  grid-template-columns: minmax(0,1fr) minmax(0,1fr);
  gap: var(--space-5);
}
/* 供应商选择器：6 列等大卡片，等宽等高、一行排开；选中 = 蓝色细框 + 浅蓝底 */
.providers {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: 8px;
}
.provider {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  min-width: 0;
  height: 74px;
  padding: 8px 4px;
  border: 1px solid var(--border-default);
  border-radius: var(--radius-md);
  background: var(--surface-primary);
  color: var(--text-secondary);
  font: inherit;
  font-size: 12px;
  line-height: 1.2;
  cursor: pointer;
}
.provider img {
  width: 24px;
  height: 24px;
}
.provider span {
  max-width: 100%;
  font-size: 12px;
  line-height: 1.2;
  text-align: center;
  overflow: visible;
  white-space: normal;
  word-break: keep-all;
}
.provider:hover {
  border-color: var(--border-strong);
  color: var(--text-primary);
}
.provider.on {
  border-color: var(--accent-primary);
  color: var(--accent-primary);
  background: color-mix(in srgb, var(--accent-primary) 6%, var(--surface-primary));
}
.edit-line {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--text-primary);
}
.edit-line img {
  width: 18px;
  height: 18px;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}
.lab {
  font-size: var(--font-size-sm);
  font-weight: 500;
  color: var(--text-primary);
}
.field-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.field-row :deep(input) {
  flex: 1;
  min-width: 0;
}
.field-row :deep(button) {
  flex: none;
}
.caret {
  color: var(--text-tertiary);
}
/* 输入框内右侧的文字按钮（获取 / 测试连接），mockup 同款蓝色链接样式 */
.inbox {
  position: relative;
}
.inbox :deep(input) {
  padding-right: 76px;
}
.inbox-btn {
  position: absolute;
  right: 10px;
  top: 50%;
  transform: translateY(-50%);
  border: 0;
  padding: 0;
  background: none;
  font: inherit;
  font-size: 12px;
  font-weight: 500;
  color: var(--accent-primary);
  cursor: pointer;
  text-decoration: none;
  white-space: nowrap;
}
.inbox-btn:hover {
  text-decoration: underline;
}
.inbox-btn:disabled {
  opacity: 0.5;
  cursor: default;
  text-decoration: none;
}
.split {
  display: grid;
  grid-template-columns: minmax(0, 1.15fr) minmax(0, 0.85fr);
  gap: 16px;
}
.hint {
  font-size: 12px;
  color: var(--text-tertiary);
  line-height: 1.4;
}
.hint.ok {
  color: var(--state-success);
}
.hint.err {
  color: var(--state-danger);
}
:deep(.row-danger) {
  color: var(--state-danger);
}
.full,.providers,.edit-line,.provider-label{grid-column:1/-1}.provider-label{font-size:var(--font-size-xs);font-weight:500;margin-bottom:calc(-1 * var(--space-3))}.provider{position:relative}.provider .selected-check{position:absolute;top:var(--space-1);right:var(--space-1);background:var(--accent-primary);color:white;border-radius:50%;padding:1px;box-sizing:content-box}.hint{font-size:var(--font-size-xs)}.lab{display:flex;align-items:center;justify-content:space-between}.key-toggle{display:inline-flex;padding:var(--space-0-5);color:var(--text-tertiary)}.model-summary{display:flex;gap:var(--space-5);align-items:center;font-size:var(--font-size-xs);color:var(--text-secondary);margin:var(--space-1) 0 var(--space-5)}.model-summary small{margin-left:auto;font-size:var(--font-size-2xs);color:var(--text-tertiary)}.model-toolbar{display:flex;align-items:center;gap:var(--space-2);max-width:560px;margin-bottom:var(--space-4);color:var(--text-tertiary)}
:deep(.model-identity){display:flex;align-items:center;gap:var(--space-3)}:deep(.model-logo){display:grid;place-items:center;width:34px;height:34px;border:1px solid var(--border-default);border-radius:var(--radius-md);background:white;flex:none}:deep(.model-logo img){width:24px;height:24px}:deep(.model-identity-copy strong){display:block;font-size:var(--font-size-sm);font-weight:500}:deep(.model-identity-copy small){display:block;font-size:var(--font-size-xs);color:var(--text-tertiary);margin-top:var(--space-1)}:deep(.model-time){font-size:var(--font-size-xs);color:var(--text-tertiary)}:deep(.dt tbody td){height:76px}.provider img{object-fit:contain}:global(.dark .provider img[src*="openai"]),:global(.dark .provider img[src*="kimi"]){filter:invert(1)}
.model-status-filter{min-width:120px}.model-toolbar :deep(input){min-width:0}
@media(max-width:650px){.dlg{grid-template-columns:minmax(0,1fr)}.providers{grid-template-columns:repeat(3,minmax(0,1fr))}.model-summary{flex-wrap:wrap}.model-summary small{margin-left:0}.split{grid-template-columns:1fr}.field-row{flex-wrap:wrap}.field-row :deep(input){flex-basis:60%}}
</style>
