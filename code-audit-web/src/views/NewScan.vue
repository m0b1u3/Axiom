<script setup lang="ts">
import { useAuth } from "../composables/auth";
const { canOperate } = useAuth();
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Upload } from "lucide-vue-next";
import { api, type ModelConfig, type SnapshotInfo } from "../api";
import type { Project } from "../types";
import PageHeader from "../components/PageHeader.vue";
import Button from "../components/ui/Button.vue";
import Input from "../components/ui/Input.vue";
import Select from "../components/ui/Select.vue";
import { message } from "../lib/toast";
import { shortHash } from "../utils/format";
import {scanInput,scopePaths,optionalBudget} from "../utils/scan-input";

/* 创建扫描 = 表单页：项目 → 快照 → 范围 → 模型与参数 */
const router = useRouter();
const route = useRoute();
const submitting = ref(false);
const form = reactive({
  sourcePath: "",
  scanMode: "standard",
  timeoutMinutes: 60,
  deepMaxDiscoveryRuns: "",
  deepStopAfterNoNew: "",
  maxCostUsd:"",
  maxTotalCostUsd:"",
  workers:"",
});
const projects = ref<Project[]>([]);
const selectedProjectId = ref<number | null>(null);
const snapshots = ref<SnapshotInfo[]>([]);
const selectedSnapshotId = ref("");
const modelConfigs = ref<ModelConfig[]>([]);
const selectedModelConfigId = ref("");
const includeText = ref("");
const selectedFocus = ref<string[]>([]);
const validationMode = ref('default'), businessContext = ref('');
const inputError=ref("");
const budgetError=computed(()=>{try{optionalBudget(form.maxCostUsd);return "";}catch(error){return (error as Error).message;}});
watch([()=>form.maxTotalCostUsd,()=>form.maxCostUsd,()=>form.workers,()=>form.scanMode,()=>form.timeoutMinutes,includeText,selectedSnapshotId,selectedModelConfigId],()=>{inputError.value="";});
watch(()=>form.scanMode,mode=>{if(mode==='deep')validationMode.value='default'});

const FOCUS_OPTIONS = [
  { key: "authz", label: "认证鉴权 / 越权" },
  { key: "injection", label: "注入" },
  { key: "ssrf", label: "SSRF / 内网" },
  { key: "secrets", label: "密钥泄露" },
  { key: "tenant", label: "多租户隔离" },
  { key: "upload", label: "上传 / 路径穿越" },
] as const;

function toggleFocus(key: string): void {
  const i = selectedFocus.value.indexOf(key);
  if (i === -1) selectedFocus.value = [...selectedFocus.value, key];
  else selectedFocus.value = selectedFocus.value.filter((k) => k !== key);
}

const fileInput = ref<HTMLInputElement | null>(null);
const uploading = ref(false);
const lastSnapshot = ref<SnapshotInfo | null>(null);

const selConfig = computed(() =>
  modelConfigs.value.find((c) => String(c.id) === selectedModelConfigId.value) ?? null,
);

const selProject = computed(() =>
  projects.value.find((p) => p.id === selectedProjectId.value) ?? null,
);

const projectOptions = computed(() =>
  projects.value.map((p) => ({
    value: String(p.id),
    label: `${p.name}  ·  ${p.snapshotCount} 快照 · ${p.scanCount} 次扫描`,
  })),
);

const snapshotOptions = computed(() =>
  snapshots.value.filter(s => selectedProjectId.value === null || s.projectId === selectedProjectId.value || !s.projectId || String(s.id) === selectedSnapshotId.value).map((s) => ({
    value: String(s.id),
    label: `#${s.id}  ${s.originalFilename}  ·  ${s.fileCount} 文件 · ${shortHash(s.archiveSha256)}`,
  })),
);

const modelOptions = computed(() =>
  modelConfigs.value.map((c) => ({
    value: String(c.id),
    label: `${c.name}  ·  ${c.provider} / ${c.model}${c.enabled ? "（已启用）" : ""}`,
  })),
);

const includePaths = computed(() => scopePaths(includeText.value));

async function onFilePicked(event: Event): Promise<void> {
  if (uploading.value) return;
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  if (!file) return;
  if (!/\.(zip|tar\.gz|tgz)$/i.test(file.name)) {
    message.warning("仅支持 .zip / .tar.gz / .tgz");
    if (fileInput.value) fileInput.value.value = "";
    return;
  }
  uploading.value = true;
  try {
    const snap = await api.uploadSnapshot(file, selectedProjectId.value ?? undefined);
    lastSnapshot.value = snap;
    selectedSnapshotId.value = String(snap.id);
    form.sourcePath = snap.snapshotPath;
    if (!snapshots.value.some((s) => s.id === snap.id)) {
      snapshots.value = [snap, ...snapshots.value];
    }
    message.success(`快照已生成：${snap.fileCount} 个文件`);
  } catch (error) {
    message.error((error as Error).message);
  } finally {
    uploading.value = false;
    if (fileInput.value) fileInput.value.value = "";
  }
}

function onPickSnapshot(v: string): void {
  if (!v) { form.sourcePath = ""; lastSnapshot.value = null; return; }
  const snap = snapshots.value.find((item) => String(item.id) === v);
  if (snap) {
    form.sourcePath = snap.snapshotPath;
    lastSnapshot.value = snap;
    if (snap.projectId > 0 && selectedProjectId.value === null) {
      selectedProjectId.value = snap.projectId;
    }
  }
}

function onProjectChange(v: string): void {
  // Project classifies this scan; it must not discard the selected immutable asset.
  selectedProjectId.value = v ? Number(v) : null;
}

async function submit(): Promise<void> {
  if (!canOperate.value || submitting.value || uploading.value) return;
  if (!selectedModelConfigId.value) {
    message.warning("请选择模型配置（没有则先到「模型配置」新建）");
    return;
  }
  if (!selectedSnapshotId.value && !form.sourcePath.trim()) {
    message.warning("请上传快照或选择已有快照");
    return;
  }
  if(form.scanMode==='deep' && [form.deepMaxDiscoveryRuns,form.deepStopAfterNoNew].some(v=>v!==''&&(!Number.isInteger(Number(v))||Number(v)<1||Number(v)>1000))){message.warning('深度扫描次数须为 1–1000 的整数，或留空使用 SDK 默认值');return;}
  submitting.value = true;
  inputError.value="";
  try {
    const payload=scanInput({snapshotId:Number(selectedSnapshotId.value),modelConfigId:Number(selectedModelConfigId.value),projectId:selectedProjectId.value,
      mode:form.scanMode,timeoutMinutes:form.timeoutMinutes,budget:form.maxCostUsd,totalBudget:form.maxTotalCostUsd,workers:form.workers,includeText:includeText.value,focus:selectedFocus.value,
      validationMode:validationMode.value as "default"|"exploitability",businessContext:businessContext.value,deepMaxDiscoveryRuns:form.deepMaxDiscoveryRuns,deepStopAfterNoNew:form.deepStopAfterNoNew});
    const task = await api.createScan(payload);
    message.success(`已创建扫描 #${task.id}，等待 Agent 领取`);
    void router.push({ name: "scan-detail", params: { id: task.id } });
  } catch (error) {
    inputError.value=(error as Error).message;
    message.error((error as Error).message);
  } finally {
    submitting.value = false;
  }
}

onMounted(async () => {
  const qProject = Number(route.query.projectId);
  if (qProject > 0) selectedProjectId.value = qProject;
  try {
    projects.value = await api.listProjects();
  } catch {
    projects.value = [];
  }
  try {
    snapshots.value = await api.listSnapshots();
  } catch {
    snapshots.value = [];
  }
  const qSnap = Number(route.query.snapshotId);
  if (qSnap > 0 && snapshots.value.some((s) => s.id === qSnap)) {
    selectedSnapshotId.value = String(qSnap);
    const snap = snapshots.value.find((s) => s.id === qSnap);
    if (snap) { form.sourcePath = snap.snapshotPath; lastSnapshot.value = snap; if (!selectedProjectId.value && snap.projectId > 0) selectedProjectId.value = snap.projectId; }
  }
  try {
    modelConfigs.value = await api.listModelConfigs();
    // 默认选中「已启用」的工作模型；无启用时仅在唯一配置下兜底
    const enabled = modelConfigs.value.find((c) => c.enabled);
    if (enabled) {
      selectedModelConfigId.value = String(enabled.id);
    } else if (modelConfigs.value.length === 1) {
      selectedModelConfigId.value = String(modelConfigs.value[0]!.id);
    }
  } catch {
    modelConfigs.value = [];
  }
});
</script>

<template>
  <div class="newscan-col">
    <PageHeader title="新建扫描" description="配置源码、扫描方式与审计要求" />

    <p v-if="!canOperate" class="request-error" role="status">当前账户为只读用户，无法创建扫描。请联系管理员调整角色。</p>
    <div v-else class="form-wrap">
<section class="scan-section"><header><h2>代码来源</h2><p>选择本次审计的源码。</p></header><div class="section-body">      <div class="source-field">
      <div class="field-label">归属项目（可选）</div>
      <Select aria-label="归属项目"
        :model-value="selectedProjectId === null ? '' : String(selectedProjectId)"
        :options="projectOptions"
        clearable
        placeholder="默认跟随资产归属，可指定本次扫描的项目"
        @change="onProjectChange"
      />
      </div>
      <div class="source-field snapshot-field">
      <div class="field-label">代码快照<span v-if="selectedSnapshotId" class="source-selected"> · 已选定源码</span></div>
      <Select aria-label="代码快照"
        v-model="selectedSnapshotId"
        :options="snapshotOptions"
        clearable
        :placeholder="snapshotOptions.length ? '选择已有快照，或上传新的源码包' : '暂无可选快照，请上传源码包'"
        @change="onPickSnapshot"
      />
      </div>

      <p v-if="selectedSnapshotId" class="upload-hint">修改归属项目会保留当前源码，仅改变本次扫描归属，不移动原资产。</p>
      <div class="upload-row">
        <div>
          <div class="upload-title">上传新的源码包</div>
          <div class="upload-hint">
            支持 .zip / .tar.gz / .tgz，同内容自动去重<span v-if="selProject">，归入「{{ selProject.name }}」</span>
          </div>
        </div>
        <!-- 文案固定：Button 自带 loading 遮罩 spinner，不换字不换宽 -->
        <Button variant="secondary" :loading="uploading" @click="fileInput?.click()">
          <Upload :size="14" />选择文件上传
        </Button>
        <input ref="fileInput" type="file" accept=".zip,.tar.gz,.tgz,.gz" style="display: none" @change="onFilePicked" />
      </div>

      <div v-if="lastSnapshot" class="snap-info mono">
        <div class="snap-line">
          <span>文件 {{ lastSnapshot.fileCount }}</span>
          <span>总大小 {{ lastSnapshot.totalSize }} B</span>
          <span>SHA {{ shortHash(lastSnapshot.archiveSha256) }}</span>
        </div>
        <div class="snap-path">{{ lastSnapshot.snapshotPath }}</div>
      </div>

</div></section>
<section class="scan-section"><header><h2>扫描配置</h2><p>选择模型和执行方式。</p></header><div class="section-body"><label class="field-label">模型配置</label>
      <Select
        v-if="modelConfigs.length"
        v-model="selectedModelConfigId"
        :options="modelOptions"
        placeholder="选择扫描使用的模型配置"
        class="wide-reset"
      />
      <div v-else class="config-hint">
        还没有模型配置，
        <router-link to="/settings/model-configs" class="tlink">到「模型配置」新建</router-link>

      </div>
      <div v-if="selConfig" class="config-info mono">
        <span>{{ selConfig.provider }} / {{ selConfig.model }}</span>
        <span class="muted">{{ selConfig.baseUrl || "（OpenAI 官方）" }}</span>
      </div>

      <div class="params">
        <div>
          <div class="field-label">扫描模式</div>
          <Select
            v-model="form.scanMode"
            :options="[
              { value: 'standard', label: '标准扫描' },
              { value: 'deep', label: '深度扫描' },
            ]"
          />
        </div>
        <div>
          <label class="field-label" for="scan-timeout">{{form.scanMode==='deep'?'发现时限（分钟）':'超时（分钟）'}}</label>
          <Input
            id="scan-timeout"
            min="1" max="600" step="1"
            :model-value="String(form.timeoutMinutes)"
            type="number"
            style="width: 140px"
            @update:model-value="form.timeoutMinutes = Math.min(600, Math.max(1, Number($event) || 60))"
          />
        </div>
      </div>

      <div class="budget-field">
        <label class="field-label" for="scan-budget">单次估算上限（USD，可选）</label>
        <Input id="scan-budget" v-model="form.maxCostUsd" type="number" min="0" step="any" inputmode="decimal" placeholder="留空不设置费用预算" :aria-invalid="budgetError ? true : undefined" aria-describedby="budget-help budget-error" />
        <p id="budget-help" class="upload-hint">依据引擎估算费用控制扫描；不是供应商账单的硬上限。第三方模型可能没有可用的费用估算。</p>
        <label class="field-label" for="scan-total-budget">任务总预算（USD，可选）</label><Input id="scan-total-budget" v-model="form.maxTotalCostUsd" type="number" min="0" step="0.000001" inputmode="decimal" placeholder="覆盖本任务的全部重试" aria-describedby="total-budget-help" /><p id="total-budget-help" class="upload-hint">每次执行按剩余额度下发，最多六位小数；费用未知时阻止累计预算重试。留空保留单次上限语义。</p>
        <p v-if="budgetError" id="budget-error" class="field-error" role="alert">{{budgetError}}</p>
      </div>

      <details v-if="form.scanMode==='deep'" class="deep-options"><summary>深度扫描停止条件（可选）</summary>
        <p>达到发现时限后，引擎停止未完成的发现任务并汇总已完成结果；另留最多 10 分钟用于收尾。</p>
        <div class="params">
          <div><label class="field-label" for="deep-max-runs">最多发现次数</label><Input id="deep-max-runs" v-model="form.deepMaxDiscoveryRuns" type="number" min="1" max="1000" step="1" placeholder="留空使用 SDK 默认值"/></div>
          <div><label class="field-label" for="deep-stop-new">连续无新增后停止（次）</label><Input id="deep-stop-new" v-model="form.deepStopAfterNoNew" type="number" min="1" max="1000" step="1" placeholder="留空使用 SDK 默认值"/></div>
          <div><label class="field-label" for="deep-workers">Worker 数量（可选）</label><Input id="deep-workers" v-model="form.workers" type="number" min="1" max="64" step="1" placeholder="留空使用引擎默认值"/></div>
        </div>
        <p>每次表示一次独立审查，不是文件数量。满足任一停止条件即可进入收尾；限时结束不代表完整覆盖。</p>
      </details>
</div></section>
<section class="scan-section"><header><h2>审计要求</h2><p>限定范围，明确审计重点。</p></header><div class="section-body"><label class="field-label" for="scan-include">扫描范围（可选）</label>      <textarea
        id="scan-include" v-model="includeText"
        rows="3" class="scope-input" aria-describedby="scope-help"
        placeholder="每行一个相对路径，留空扫描全部"
      ></textarea>
      <div v-if="includePaths.length" class="include-hint">
        <span class="mono">{{ includePaths.length }}</span> 个路径子集 · 只扫描这些目录/文件
      </div>
      <p id="scope-help" class="upload-hint">每行一个目录或文件；文件名中的逗号会原样保留。</p>

<div class="field-label focus-label">发现重点（可选）</div>      <div class="focus-row">
        <Button
          v-for="opt in FOCUS_OPTIONS"
          :key="opt.key"
          :aria-pressed="selectedFocus.includes(opt.key)"
          size="sm"
          :variant="selectedFocus.includes(opt.key) ? 'secondary' : 'ghost'"
          @click="toggleFocus(opt.key)"
        >
          {{ opt.label }}
        </Button>
      </div>
      <div class="upload-hint" style="margin-top: 4px;">
        可多选；不选则进行通用审计。
      </div>

      <fieldset class="validation-options">
        <legend>可利用性验证</legend>
        <label><input v-model="validationMode" type="radio" value="default" name="validation-mode" />默认审计<span>沿用 SDK 自带验证流程。</span></label>
        <label :class="{unavailable:form.scanMode==='deep'}"><input v-model="validationMode" type="radio" value="exploitability" name="validation-mode" :disabled="form.scanMode==='deep'" />加强可利用性验证<span>发现候选后独立验证输入可控性、权限前提和阻断措施。</span></label>
        <p class="upload-hint">{{form.scanMode==='deep'?'深度扫描不支持独立验证，请在上方切换为标准扫描。':'加强验证会增加本次扫描的耗时与模型用量；只分析源码，不运行项目或发送攻击请求。'}}</p>
        <div v-if="validationMode==='exploitability'" class="business-context">
          <label for="business-context">业务环境与攻击者权限（可选）</label>
          <textarea id="business-context" v-model="businessContext" maxlength="4000" rows="4" placeholder="例如：普通用户可注册；配置仅管理员可改；重点考虑未登录攻击者。" aria-describedby="context-help"></textarea>
          <p id="context-help" class="upload-hint">作为用户提供的背景保存，与源码已确认事实区分。{{businessContext.length}} / 4000</p>
        </div>
      </fieldset>
</div></section>
      <div class="submit-row">
        <Button :loading="submitting" :disabled="uploading" @click="submit">创建扫描</Button>
        <span class="submit-note">扫描将在后台执行，你可以在任务详情中查看进度、日志和分析结果。</span>
      </div>
      <p v-if="inputError" class="field-error" role="alert">{{inputError}}</p>
    </div>
  </div>
</template>

<style scoped>
.newscan-col{max-width:1040px}
.scan-section{display:grid;grid-template-columns:160px minmax(0,1fr);gap:var(--space-6);padding:var(--space-6) 0;border-top:1px solid var(--border-default)}
.scan-section header h2{font-size:var(--font-size-md);font-weight:600;margin:0 0 var(--space-2)}
.scan-section header p,.upload-hint,.config-info,.snap-info,.include-hint,.submit-note{font-size:var(--font-size-xs);color:var(--text-secondary);line-height:var(--line-height-body)}
.scan-section header p{margin:0}.section-body{min-width:0}.field-label{display:block;font-size:var(--font-size-sm);font-weight:500;margin-bottom:var(--space-2)}
.source-selected{font-size:var(--font-size-xs);font-weight:400;color:var(--text-secondary)}
.source-field+.source-field{margin-top:var(--space-4)}
.budget-field{margin-top:var(--space-4)}
.field-error{font-size:var(--font-size-sm);color:var(--state-danger);margin:var(--space-2) 0 0;line-height:var(--line-height-body)}
.scope-input{width:100%;box-sizing:border-box;padding:var(--space-3);border:1px solid var(--border-control);border-radius:var(--radius-md);background:var(--surface-primary);color:var(--text-primary);font:var(--font-size-md)/var(--line-height-body) var(--font-mono);resize:vertical}
.upload-row{display:flex;justify-content:space-between;align-items:center;gap:var(--space-3);margin-top:var(--space-3)}
.upload-title{font-size:var(--font-size-xs);font-weight:500;margin-bottom:var(--space-1)}
.upload-hint{margin:var(--space-2) 0 0}.upload-row .upload-hint{margin:0}.upload-row>div{min-width:0}.upload-row>button{flex-shrink:0}
.snap-info{margin-top:var(--space-3)}.snap-line{display:flex;gap:var(--space-3);flex-wrap:wrap}.snap-path{display:none}
.params{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:var(--space-4);margin-top:var(--space-4)}
.config-info{margin-top:var(--space-2);display:flex;gap:var(--space-2);flex-wrap:wrap;overflow-wrap:anywhere}.config-info>span+span{display:none}
.config-hint{font-size:var(--font-size-sm);color:var(--text-secondary)}.tlink{color:var(--accent-primary)}
.focus-label{margin-top:var(--space-5)}.focus-row{display:flex;gap:var(--space-2);flex-wrap:wrap}
.focus-row :deep(button){border:1px solid var(--border-default)}.focus-row :deep(button[aria-pressed="true"]){border-color:var(--accent-primary);color:var(--accent-primary)}
.validation-options{border:0;padding:0;margin:var(--space-5) 0 0;min-width:0}
.validation-options legend{font-size:var(--font-size-sm);font-weight:500;padding:0;margin-bottom:var(--space-2)}
.validation-options>label{display:grid;grid-template-columns:16px minmax(0,1fr);column-gap:var(--space-2);align-items:center;font-size:var(--font-size-sm);margin:var(--space-3) 0;cursor:pointer}
.validation-options>label span{grid-column:2;color:var(--text-secondary);font-size:var(--font-size-xs);margin-top:var(--space-1)}
.validation-options input{margin:0;accent-color:var(--accent-primary)}.unavailable{opacity:.6}
.business-context{margin-top:var(--space-4);display:flex;flex-direction:column;gap:var(--space-2);font-size:var(--font-size-sm)}
.business-context textarea{width:100%;box-sizing:border-box;padding:var(--space-3);border:1px solid var(--border-default);border-radius:var(--radius-md);background:var(--surface-primary);color:var(--text-primary);font:inherit;resize:vertical}
.business-context textarea:focus-visible{outline:2px solid var(--accent-primary);outline-offset:2px}
.deep-options{margin-top:var(--space-4);font-size:var(--font-size-xs);color:var(--text-secondary)}summary{cursor:pointer}.deep-options p{line-height:var(--line-height-body)}
.submit-row{border-top:1px solid var(--border-default);padding:var(--space-5) 0;display:flex;gap:var(--space-3);align-items:center}.submit-note{max-width:60ch}
.section-body :deep(input[type="number"]){appearance:textfield;-moz-appearance:textfield}.section-body :deep(input::-webkit-inner-spin-button),.section-body :deep(input::-webkit-outer-spin-button){-webkit-appearance:none;margin:0}
@media(max-width:800px){.scan-section{grid-template-columns:1fr;gap:var(--space-4)}.scan-section header p{display:none}}
@media(max-width:480px){.params{grid-template-columns:1fr}.upload-row{align-items:flex-start;flex-direction:column}.submit-row{align-items:flex-start;flex-direction:column}}
</style>
