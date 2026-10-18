<script setup lang="ts">
import SDKIdentityBinding from "../components/SDKIdentityBinding.vue";
import SDKSessionEvents from "../components/SDKSessionEvents.vue";
import SDKOperations from "../components/SDKOperations.vue";
import SDKValidationAction from "../components/SDKValidationAction.vue";
const sdkOperationRevision=ref(0);
import ResourceCleanupDialog from "../components/ResourceCleanupDialog.vue";
import ExecutionBudget from "../components/ExecutionBudget.vue";
import type {TaskBudgetSnapshot} from "../contracts";
import { useAuth } from "../composables/auth";
const { canOperate, canAdmin } = useAuth();
import {progressView} from '../utils/scan-progress';
import ExploitabilityReview from "../components/ExploitabilityReview.vue";
import { computed, nextTick, onMounted, onUnmounted, ref, shallowRef, shallowReactive, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import FindingEvidenceGraph from '../components/FindingEvidenceGraph.vue';
import { RotateCcw, Download, FileText, FileDown, Trash2, X, ChevronRight, Search } from "lucide-vue-next";
import { api } from "../api";
import type { QueueOverview, Finding, Progress, ScanCompare, ScanAttempt, ScanLogEvent, ScanTask } from "../types";
import ComponentExecutions from '../components/ComponentExecutions.vue';
import {componentBinding} from '../utils/component-scans';
import GitDiffPreview from '../components/GitDiffPreview.vue';
import ExecutionScanInput from "../components/ExecutionScanInput.vue";
import ExecutionResult from "../components/ExecutionResult.vue";
import ExecutionHistory from "../components/ExecutionHistory.vue";
import ExecutionRecovery from "../components/ExecutionRecovery.vue";
import ScanCoverage from "../components/ScanCoverage.vue";
import ScanActivity from "../components/ScanActivity.vue";
import StatusBadge from "../components/StatusBadge.vue";
import Button from "../components/ui/Button.vue";
import Input from "../components/ui/Input.vue";
import Select from "../components/ui/Select.vue";
import Tooltip from "../components/ui/Tooltip.vue";
import DropdownMenu from "../components/ui/DropdownMenu.vue";
import Loading from "../components/ui/Loading.vue";
import CodeViewer from "../components/CodeViewer.vue";
import { message } from "../lib/toast";
import { confirm } from "../components/ui/confirm";
import { relativeTime, absoluteTime, durationBetween } from "../utils/time";

/* ExecutionWorkspace（design/COMPONENTS.md #15）：
   Header（对象+状态+动作）→ 执行进度/活动流 → 主数据面（findings grid）
   → Inspector 常驻右栏（选中 finding 的调查面板） */
const route = useRoute();
const router = useRouter();
const id = Number(route.params.id);
const task = shallowRef<ScanTask | null>(null);
const budget=shallowRef<TaskBudgetSnapshot|null>(null);
const queueOverview=ref<QueueOverview|null>(null);
const queueError=ref("");
const findings = shallowRef<Finding[]>([]);
const progress = shallowRef<Progress | null>(null);
const findLoading = ref(false);
const loadError = ref("");
const findingsError = ref("");
const compareError = ref("");
const actionError = ref("");
const statusBusy = ref(false);
const exporting = ref(false);
const exportingPdf = ref(false);
const compare = shallowRef<ScanCompare | null>(null);
const tab = ref("findings");
const severityFilter = ref("");
const search = ref("");
const selected = shallowRef<Finding | null>(null);
let timer: number | undefined;

const logEpoch = ref(0);
const attempts = shallowRef<ScanAttempt[]>([]);
const recoveryPanel=ref<InstanceType<typeof ExecutionRecovery>|null>(null);
const attemptError = ref("");
const attemptsReady=ref(false);
function requestedAttempt(){const value=String(route.query.attempt??'');return /^[1-9][0-9]*$/.test(value)&&Number.isSafeInteger(Number(value))?value:'current';}
const selectedAttempt = ref(requestedAttempt());
watch(()=>route.query.attempt,()=>{selectedAttempt.value=requestedAttempt();});
const historicalAttempt = computed(() => attempts.value.find(a => String(a.attempt) === selectedAttempt.value));
const unavailableAttempt=computed(()=>selectedAttempt.value!=='current'&&!historicalAttempt.value);
const attemptOptions = computed(() => [
  {value:"current",label:task.value?.status === "pending" ? "当前任务 · 等待领取" : `当前执行${task.value?.attempt ? ` · 第 ${task.value.attempt} 次` : " · 等待领取"}`},
  ...attempts.value.map(a => ({value:String(a.attempt),label:`第 ${a.attempt} 次 · ${absoluteTime(a.startedAt)}`})),
]);
async function loadAttempts() {
 const generation = loadGeneration;
 try { const result = await api.scanAttempts(id); if (disposed || generation !== loadGeneration) return; attempts.value = result; attemptsReady.value=true; attemptError.value=""; }
 catch(e) { if (!disposed && generation === loadGeneration) attemptError.value=(e as Error).message; }
}

const isRunning = computed(() => task.value !== null && ["pending", "claimed", "scanning", "cancel_requested"].includes(task.value.status));

const severityCounts = computed(() => {
  const c = { critical: 0, high: 0, medium: 0, low: 0, info: 0 };
  for (const f of findings.value) {
    const key = f.severity as keyof typeof c;
    if (key in c) c[key]++;
  }
  return c;
});
const duration = computed(() => {
  const t = task.value;
  return t?.startedAt && t.finishedAt ? durationBetween(t.startedAt, t.finishedAt) : "";
});

const filtered = computed(() => {
  const kw = search.value.trim().toLowerCase();
  return findings.value.filter((f) => {
    if (severityFilter.value && f.severity !== severityFilter.value) return false;
    if (!kw) return true;
    return (
      f.title.toLowerCase().includes(kw) ||
      f.filePath.toLowerCase().includes(kw) ||
      (f.cwe ?? []).join(" ").toLowerCase().includes(kw)
    );
  });
});

watch(filtered, rows => { if (selected.value && !rows.some(f => f.id === selected.value!.id)) selected.value = rows[0] ?? null; });

const sevChips = computed(() => [
  { key: "", label: "全部", count: findings.value.length },
  { key: "critical", label: "Critical", count: severityCounts.value.critical },
  { key: "high", label: "High", count: severityCounts.value.high },
  { key: "medium", label: "Medium", count: severityCounts.value.medium },
  { key: "low", label: "Low", count: severityCounts.value.low },
  { key: "info", label: "Info", count: severityCounts.value.info },
]);

const FINDING_STATUS: Record<string, string> = {
  open: "未处理",
  confirmed: "已确认",
  false_positive: "误报",
  fixed: "已修复",
};
function findingStatusLabel(st: string): string {
  return FINDING_STATUS[st] ?? st;
}

function severityColor(sev: string): string {
  switch (sev) {
    case "critical":
      return "var(--severity-critical)";
    case "high":
      return "var(--severity-high)";
    case "medium":
      return "var(--severity-medium)";
    case "low":
      return "var(--severity-low)";
    default:
      return "var(--severity-info)";
  }
}

/* ---- 上游 raw 完整内容读取（防御式，缺字段即静默） ---- */
type RAny = Record<string, unknown>;

function rawObj(f: Finding): RAny | null {
  if (f.raw && typeof f.raw === "object" && !Array.isArray(f.raw)) return f.raw as RAny;
  return null;
}
function rstr(v: unknown): string {
  if (typeof v === "string") return v;
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  return "";
}
function rlist(v: unknown): string[] {
  if (Array.isArray(v)) return v.map(rstr).filter(Boolean);
  if (typeof v === "string" && v) return [v];
  return [];
}
function robj(v: unknown): RAny | null {
  if (v && typeof v === "object" && !Array.isArray(v)) return v as RAny;
  return null;
}

function sevOf(f: Finding): { level?: string; score?: string; scoringSystem?: string } {
  if(f.normalized)return {level:f.normalized.severity,score:f.normalized.severityScore===undefined?undefined:String(f.normalized.severityScore),scoringSystem:f.normalized.scoringSystem};
  const sev = robj(rawObj(f)?.["severity"]);
  return {
    level: sev ? rstr(sev["level"]) : undefined,
    score: sev && sev["score"] !== undefined ? String(sev["score"]) : undefined,
    scoringSystem: sev ? rstr(sev["scoringSystem"]) : undefined,
  };
}
function confOf(f: Finding): { level?: string; rationale?: string } {
  if(f.normalized)return {level:f.normalized.confidence?.level,rationale:f.normalized.confidence?.rationale??undefined};
  const c = robj(rawObj(f)?.["confidence"]);
  return {
    level: c ? rstr(c["level"]) : undefined,
    rationale: c ? rstr(c["rationale"]) : undefined,
  };
}
function apOf(f: Finding): {
  has: boolean;
  summary?: string;
  entrypoint?: string;
  source?: string;
  dataflow?: string;
  sink?: string;
  reachability?: string;
  impact?: string;
  likelihood?: string;
  steps: string[];
  preconditions: string[];
  assumptions: string[];
} {
  if(f.normalized){const n=f.normalized;return {has:!!(n.attackPath||n.entrypoint||n.source||n.sink||n.dataflow||n.attackSteps?.length),summary:n.attackPath,entrypoint:n.entrypoint,source:n.source,dataflow:n.dataflow,sink:n.sink,reachability:n.reachability,impact:n.impact,likelihood:n.likelihood,steps:n.attackSteps??[],preconditions:n.preconditions??[],assumptions:[]};}
  const raw = rawObj(f);
  const ap = raw?.["attackPath"];
  if (typeof ap === "string") return { has: true, dataflow: ap, steps: [], preconditions: [], assumptions: [] };
  const o = robj(ap);
  if (!o) return { has: false, steps: [], preconditions: [], assumptions: [] };
  const dataflow = o["dataflow"] ?? o["data_flow"];
  const reach = o["reachability"];
  const impact = o["impact"];
  const likelihood = o["likelihood"];
  return {
    has: true,
    summary: rstr(o["summary"]),
    entrypoint: rstr(o["entrypoint"]),
    source: rstr(o["source"]),
    dataflow: typeof dataflow === "string" ? dataflow : robj(dataflow) ? rstr(robj(dataflow)?.["summary"]) : "",
    sink: rstr(o["sink"]),
    reachability: typeof reach === "string" ? reach : robj(reach) ? rstr(robj(reach)?.["summary"]) : "",
    impact: typeof impact === "string" ? impact : robj(impact) ? String(robj(impact)?.["rationale"] ?? robj(impact)?.["why"] ?? "") : "",
    likelihood: typeof likelihood === "string" ? likelihood : robj(likelihood) ? String(robj(likelihood)?.["level"] ?? robj(likelihood)?.["rationale"] ?? "") : "",
    steps: rlist(o["steps"]),
    preconditions: rlist(o["preconditions"]),
    assumptions: rlist(o["assumptions"]),
  };
}
function rcOf(f: Finding): string {
  if(f.normalized)return f.normalized.rootCause??"";
  const rc = rawObj(f)?.["rootCause"];
  return typeof rc === "string" ? rc : robj(rc) ? rstr(robj(rc)?.["summary"]) : "";
}
function evOf(f: Finding): Array<{ label?: string; path?: string; startLine?: number; endLine?: number; code?: string; explanation?: string }> {
  if(f.normalized)return f.normalized.codeEvidence??[];
  const list = rawObj(f)?.["codeEvidence"];
  if (!Array.isArray(list)) return [];
  return list
    .map((e) => {
      const o = robj(e);
      return o
        ? {
            label: rstr(o["label"]),
            path: rstr(o["path"]),
            startLine: typeof o["startLine"] === "number" ? o["startLine"] : undefined,
            endLine: typeof o["endLine"] === "number" ? o["endLine"] : undefined,
            code: rstr(o["code"]),
            explanation: rstr(o["explanation"]),
          }
        : null;
    })
    .filter((e): e is NonNullable<typeof e> => e !== null);
}
function locOf(f: Finding): Array<{ role?: string; path?: string; startLine?: number; endLine?: number }> {
  const list = rawObj(f)?.["locations"];
  if (!Array.isArray(list)) return [];
  return list.map((l) => {
    const o = robj(l);
    return {
      role: o ? rstr(o["role"]) : "",
      path: o ? rstr(o["path"]) : "",
      startLine: o && typeof o["startLine"] === "number" ? o["startLine"] : 0,
      endLine: o && typeof o["endLine"] === "number" ? o["endLine"] : undefined,
    };
  });
}
function valOf(f: Finding): string {
  const v = f.normalized?f.normalized.validation:rawObj(f)?.["validation"];
  if (v === undefined || v === null) return "";
  if (typeof v === "string") return v;
  return JSON.stringify(v, null, 2);
}
function rtOf(f: Finding): string[] {
  return f.normalized?f.normalized.remediationTests??[]:rlist(rawObj(f)?.["remediationTests"]);
}
function pcOf(f: Finding): string[] {
  return f.normalized?f.normalized.preventiveControls??[]:rlist(rawObj(f)?.["preventiveControls"]);
}
function proOf(f: Finding): RAny | null {
  return robj(f.normalized?f.normalized.provenance:rawObj(f)?.["provenance"]);
}
function extOf(f: Finding): RAny | null {
  const extensions=robj(f.normalized?f.normalized.extensions:rawObj(f)?.["extensions"]);
  if(!extensions)return null;
  const sdk=Object.fromEntries(Object.entries(extensions).filter(([key])=>!['axiomEvidence','axiomEvidenceUnavailable'].includes(key)));
  return Object.keys(sdk).length?sdk:null;
}
function ruleIdOf(f: Finding): string {
  return f.normalized?f.normalized.ruleId??"":rstr(rawObj(f)?.["ruleId"]);
}
function idOf(f: Finding): string {
  if(f.normalized?.engineFindingId)return f.normalized.engineFindingId;
  if(rawObj(f)?.["findingId"])return rstr(rawObj(f)?.["findingId"]);
  const id = rawObj(f)?.["identity"];
  return typeof id === "string" ? id : robj(id) ? rstr(robj(id)?.["anchor"]) : "";
}
function fpOf(f: Finding): string {
  if(f.normalized)return f.normalized.fingerprint;
  const fp = rawObj(f)?.["fingerprints"];
  const o = robj(fp);
  return o ? rstr(o["primary"]) : "";
}
function kvOf(o: RAny | null): Array<[string, string]> {
  if (!o) return [];
  return Object.entries(o)
    .filter(([, v]) => v !== null && v !== undefined && v !== "")
    .map(([k, v]) => [k, typeof v === "object" ? JSON.stringify(v) : rstr(v)]);
}

const prettyRaw = computed(() => {
  if (!selected.value) return "";
  try {
    return JSON.stringify(selected.value.raw ?? null, null, 2);
  } catch {
    return String(selected.value.raw ?? "");
  }
});

/* 导出菜单 / 状态选项 / raw 折叠 / monaco 语言推断 */
const exportItems = [
  { key: "html", label: "HTML 报告", icon: FileText },
  { key: "pdf", label: "PDF 报告", icon: FileDown },
];
function onExportSelect(key: string): void {
  if (key === "html") void exportReport();
  else void exportReportPdf();
}

const statusOptions = Object.entries(FINDING_STATUS).map(([value, label]) => ({ value, label }));

const rawOpen = ref(false);

const MONACO_LANG: Record<string, string> = {
  php: "php", js: "javascript", mjs: "javascript", jsx: "javascript",
  ts: "typescript", tsx: "typescript", vue: "html", py: "python",
  java: "java", go: "go", rb: "ruby", cs: "csharp",
  c: "c", h: "c", cpp: "cpp", cc: "cpp", cxx: "cpp",
  sql: "sql", xml: "xml", html: "html", htm: "html",
  json: "json", yml: "yaml", yaml: "yaml", sh: "shell", css: "css",
};
function langOf(path: string): string {
  const ext = path.split(".").pop()?.toLowerCase() ?? "";
  return MONACO_LANG[ext] ?? "plaintext";
}

async function downloadBlob(blob: Blob, filename: string): Promise<void> {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}
async function exportReport(): Promise<void> {
  if(unavailableAttempt.value){message.error('所选执行尚不可用，请读取执行记录后再导出报告');return}
  const attempt=historicalAttempt.value?.attempt;
  const filename=`report-${id}${attempt?'-attempt-'+attempt:''}.html`;
  exporting.value = true;
  try {
    await downloadBlob(await api.exportReport(id,attempt),filename);
  } catch (error) {
    message.error((error as Error).message);
  } finally {
    exporting.value = false;
  }
}
async function exportReportPdf(): Promise<void> {
  if(unavailableAttempt.value){message.error('所选执行尚不可用，请读取执行记录后再导出报告');return}
  const attempt=historicalAttempt.value?.attempt;
  const filename=`report-${id}${attempt?'-attempt-'+attempt:''}.pdf`;
  exportingPdf.value = true;
  try {
    await downloadBlob(await api.exportReportPdf(id,attempt),filename);
  } catch (error) {
    message.error((error as Error).message);
  } finally {
    exportingPdf.value = false;
  }
}

let loadBusy = false;
let mutationBusy = false;
let loadGeneration = 0;
let disposed = false;
async function load(): Promise<void> {
  if (loadBusy || mutationBusy || disposed) return;
  loadBusy = true;
  const generation = loadGeneration;
  if (!Number.isSafeInteger(id) || id < 1) { loadError.value = "扫描编号无效，请从扫描任务列表重新打开。"; loadBusy = false; return; }
  try {
    const previousAttempt = task.value?.attempt;
    const previousStatus = task.value?.status;
    const [scan,queue]=await Promise.allSettled([api.getScan(id),api.queueOverview(id)]);
    if (disposed || generation !== loadGeneration) return;
    if(queue.status==='fulfilled'){queueOverview.value=queue.value;queueError.value=''}else{queueOverview.value=null;queueError.value=queue.reason.message}
    if(scan.status==='rejected')throw scan.reason;
    task.value = scan.value;
    if (previousAttempt !== task.value.attempt || previousStatus !== task.value.status) void loadAttempts();
    loadError.value = "";
    progress.value = progressView(task.value.progress,task.value.progressJson);
    if (["completed", "partial", "failed", "canceled", "timeout"].includes(task.value.status)) {
      if (timer) window.clearInterval(timer);
      timer = undefined;
    } else if (!timer) {
      timer = window.setInterval(() => void load(),3000);
    }
  } catch (error) {
    if (!disposed && generation === loadGeneration) loadError.value = (error as Error).message;
  } finally { loadBusy = false; }
}
const focusFindingId = Number(route.query.finding ?? 0);
let focusedOnce = false;
let findingsGeneration=0;
watch([()=>selectedAttempt.value,()=>task.value?.attempt,()=>task.value?.status,()=>historicalAttempt.value?.status],()=>{
  findingsGeneration++;findings.value=[];selected.value=null;compare.value=null;compareError.value='';findLoading.value=false;findingsError.value='';
  if(task.value && !unavailableAttempt.value && (historicalAttempt.value || ["completed","partial","failed","canceled","timeout"].includes(task.value.status)))void loadFindings();
});

async function loadFindings(): Promise<void> {
  const generation = loadGeneration;
  const request=++findingsGeneration;
  findLoading.value = true;
  findingsError.value = "";
  try {
    const historical=historicalAttempt.value;
    const result = historical ? (await api.attemptResult(id,historical.attempt)).findings.map((f,index)=>({...f,id:-(index+1),status:'archived'})) : await api.listFindings(id);
    if (disposed || generation !== loadGeneration || request!==findingsGeneration) return;
    findings.value = (result??[]).map(row=>shallowReactive(row));
    if (!selected.value && !focusedOnce && findings.value.length) selected.value = findings.value[0];
    // 池页跳转直达：自动打开指定 finding 的 Inspector
    if (focusFindingId > 0 && !focusedOnce) {
      const target = findings.value.find((f) => f.id === focusFindingId);
      if (target) {
        focusedOnce = true;
        selected.value = target;
      }
    }
  } catch (error) {
    if (!disposed && generation === loadGeneration && request===findingsGeneration) findingsError.value = `漏洞读取失败：${(error as Error).message}`;
  } finally {
    if(request===findingsGeneration)findLoading.value = false;
  }
  if (!disposed && generation === loadGeneration && request===findingsGeneration && !historicalAttempt.value) void loadCompare();
}
async function loadCompare(): Promise<void> {
  const generation = loadGeneration;
  const selection=selectedAttempt.value;
  try {
    const result = await api.compareScan(id);
    if (disposed || generation !== loadGeneration || selection!==selectedAttempt.value) return;
    compare.value = result;
    compareError.value = "";
  } catch (error) {
    if (!disposed && generation === loadGeneration && selection===selectedAttempt.value) { compare.value = null; compareError.value = `扫描对比暂不可用：${(error as Error).message}`; }
  }
}

const terminal = computed(
  () => task.value !== null && ["completed", "partial", "failed", "canceled", "timeout"].includes(task.value.status),
);
const canCancel = computed(
  () => task.value !== null && ["pending", "claimed", "scanning", "cancel_requested"].includes(task.value.status),
);
const canRetry = computed(() => terminal.value);

const cleanupOpen=ref(false);
function deleteScan():void{cleanupOpen.value=true}
function scanDeleted(){message.success('已删除扫描与产物');void router.push({name:'scans'})}

async function cancelScan(): Promise<void> {
  const ok = await confirm({ title: "取消扫描", body: "确定取消这次扫描？" });
  if (!ok) return;
  if (mutationBusy) return;
  try {
    mutationBusy = true;
    actionError.value = "";
    loadGeneration++;
    task.value = await api.cancelScan(id);
    loadGeneration++;
    message.success("已请求取消，等待 Agent 停止扫描");
  } catch (error) {
    actionError.value = `取消请求未完成：${(error as Error).message}`;
  } finally { mutationBusy = false; loadGeneration++; if (!disposed) { void loadAttempts(); void load(); } }
}
async function retryScan(): Promise<void> {
  if (!mutationBusy) await recoveryPanel.value?.requestRestart();
}
function onRecoveryChanged(next:ScanTask,action:'restore_result'|'restart'|'interrupt_expired'):void {
  loadGeneration++;
  task.value=next;
  selectedAttempt.value='current';
  findings.value=[];findingsError.value='';compareError.value='';
  progress.value=null;compare.value=null;selected.value=null;logEpoch.value++;
  sdkOperationRevision.value++;
  message.success(action==='restore_result'?'已恢复原执行结果，未调用模型':action==='restart'?'已确认重新执行并排队':'已登记执行中断，未启动新扫描');
  if (!timer) timer=window.setInterval(()=>void load(),3000);
  void loadAttempts();void load();
}

async function onStatusChange(row: Finding, value: string): Promise<void> {
  if (!canOperate.value || historicalAttempt.value || statusBusy.value) return;
  statusBusy.value = true;
  actionError.value = "";
  try {
    await api.setFindingStatus(row.id, value);
    row.status = value;
    message.success(`已标记 ${FINDING_STATUS[value] ?? value}`);
  } catch (error) {
    actionError.value = `漏洞处理状态未保存：${(error as Error).message}`;
  } finally { statusBusy.value = false; }
}

function closeFinding(): void {
  const findingId = selected.value?.id; selected.value = null;
  void nextTick(() => document.querySelector<HTMLElement>(`[data-finding-id="${findingId}"]`)?.focus());
}
function onFindingKey(event: KeyboardEvent, index: number): void {
  if (event.target !== event.currentTarget || event.ctrlKey || event.metaKey || event.altKey) return;
  const positions: Record<string, number> = {ArrowDown:index+1,j:index+1,ArrowUp:index-1,k:index-1,Home:0,End:filtered.value.length-1};
  if (!(event.key in positions)) return;
  event.preventDefault();
  const row = filtered.value[Math.max(0,Math.min(filtered.value.length-1,positions[event.key]!))];
  if (row) { selected.value = row; void nextTick(() => document.querySelector<HTMLElement>(`[data-finding-id="${row.id}"]`)?.focus()); }
}
function inspectorShortcut(event: KeyboardEvent): void {
  if (event.key === 'Escape' && selected.value && !document.querySelector('[role="dialog"],[role="listbox"]')) closeFinding();
}

onMounted(() => {
  window.addEventListener('keydown', inspectorShortcut);
  void load();
  timer = window.setInterval(() => void load(), 3000);
});
onUnmounted(() => {
  window.removeEventListener('keydown', inspectorShortcut);
  disposed = true;
  loadGeneration++;
  if (timer) window.clearInterval(timer);
});

function sourceName(t: ScanTask): string {
  const segs = t.sourcePath.split(/[\\/]/).filter(Boolean);
  return segs[segs.length - 1] ?? t.sourcePath;
}
</script>

<template>
  <ResourceCleanupDialog v-model:open="cleanupOpen" kind="scan" :resource-id="id" @completed="scanDeleted" />
  <div v-if="task" class="exec">
    <div v-if="loadError" class="request-error" role="alert"><span>{{loadError}}</span><Button variant="outline" size="sm" @click="load">重试</Button></div>
    <!-- WorkspaceHeader（COMPONENTS #3）：对象 + 状态 + 主动作 -->
    <div class="exec-head">
      <div class="exec-head-main">
        <div class="title-row">
          <button class="crumb-link" @click="router.push({ name: 'scans' })">扫描任务</button>
          <span class="crumb-sep">/</span>
          <h1 class="exec-title mono">{{ task.scanNo }}</h1>
          <StatusBadge :status="task.status" />
        </div>
        <div class="exec-sub">
          <Tooltip :content="task.sourcePath">
            <span class="src-path">{{ sourceName(task) }}</span>
          </Tooltip>
          <span class="sep">·</span>
          <span>{{ task.provider }} · {{ task.model }}</span>
          <span class="sep">·</span>
          <span>{{ task.scanMode }}</span>
          <template v-if="duration">
            <span class="sep">·</span>
            <span class="mono">耗时 {{ duration }}</span>
          </template>
          <span class="sep">·</span>
          <Tooltip :content="absoluteTime(task.finishedAt || task.createdAt)">
            <span>{{ task.finishedAt ? "完成于 " + relativeTime(task.finishedAt) : "创建于 " + relativeTime(task.createdAt) }}</span>
          </Tooltip>
        </div>
      </div>
      <div class="exec-actions">
        <Button v-if="canOperate && canCancel" variant="outline" @click="cancelScan"><X :size="14" />取消</Button>
        <Button v-if="canOperate && canRetry && !task.sdkOperation && !task.componentPlanning && !task.componentScan" :disabled="budget?.accountingVersion===1 && !budget.retryAllowed" variant="secondary" @click="retryScan"><RotateCcw :size="14" />重新执行</Button>
        <DropdownMenu v-if="!task.sdkOperation && !task.componentPlanning && !unavailableAttempt" :items="exportItems" @select="onExportSelect">
          <Button variant="secondary" :loading="exporting || exportingPdf"><Download :size="14" />导出</Button>
        </DropdownMenu>
        <Button v-if="canAdmin && canRetry && !task.componentPlanning" variant="destructive" @click="deleteScan"><Trash2 :size="14" />删除</Button>
      </div>
    </div>

    <div v-if="queueError" class="request-error" role="alert">调度状态暂不可用：{{queueError}}<Button variant="ghost" size="sm" @click="load">重新加载</Button></div>
    <SDKIdentityBinding v-if="task.sdkIdentity" :identity="task.sdkIdentity"/>
    <p v-if="actionError" class="request-error" role="alert">{{actionError}}。当前任务和历史结果已保留，可重试原操作。</p>
    <p v-if="task.sdkOperation" class="deep-policy-summary">{{task.sdkOperation.kind==='validate'?'SDK 独立复核 · 完整源码快照 · 费用未知，不支持费用上限，不能自动重跑':'SDK 原生预检 · 只检查本地输入与配置，不启动模型'}} <RouterLink v-if="task.sdkOperation.origin" :to="'/scans/'+task.sdkOperation.origin.taskId+'?attempt='+task.sdkOperation.origin.attempt">来源执行 #{{task.sdkOperation.origin.taskId}} / {{task.sdkOperation.origin.attempt}}</RouterLink></p>
    <p v-else-if="task.componentPlanning" class="deep-policy-summary">{{task.componentPlanning.kind==='normalize'?'组件计划本地校验 · 不调用模型':'组件划分建议 · 费用未知'}} · <RouterLink :to="'/component-plans/'+task.componentPlanning.planId">返回组件计划</RouterLink></p>
    <SDKOperations v-else-if="task.inputContractVersion && task.sourceSnapshotId && !task.componentScan" :task="task" :revision="sdkOperationRevision"/>
    <ExecutionBudget v-if="task.sdkOperation?.kind!=='preflight'" :limits-unsupported="!!task.sdkOperation && !task.componentPlanning" :scan-id="id" :status="task.status" :attempt="task.attempt" :progress-revision="task.progress?.revision" @snapshot="budget=$event" @select-attempt="selectedAttempt=String($event)" />
    <p v-if="task.maxCostUsd!=null && (!budget || budget.accountingVersion===0)" class="deep-policy-summary">本次扫描的估算费用预算：{{task.maxCostUsd}} USD · 依据引擎估算，供应商实际账单可能不同。</p>
    <p v-if="task.scanMode==='deep'" class="deep-policy-summary">发现时限 {{task.timeoutMinutes}} 分钟 · 汇总宽限最多 10 分钟 · 最多发现 {{task.deepMaxDiscoveryRuns||'SDK 默认'}} 次 · 连续无新增 {{task.deepStopAfterNoNew||'SDK 默认'}} 次后停止 · worker {{task.workers??'SDK 默认'}}</p>
    <ExecutionHistory :task="task" :attempts="attempts" :selected-attempt="selectedAttempt" :overview="queueOverview" @select="selectedAttempt=$event" />
    <ComponentExecutions v-if="task.componentScan" :task="task" :attempt="historicalAttempt?.attempt" :can-operate="canOperate" :budget="budget" @changed="load" @select-attempt="selectedAttempt=String($event)"/>
    <ExecutionRecovery v-if="!task.sdkOperation && !task.componentPlanning" ref="recoveryPanel" :task="task" :can-operate="canOperate" :budget-revision="budget?.revision" @changed="onRecoveryChanged" />
    <!-- 摘要指标带 -->
    <div v-if="!task.sdkOperation && !task.componentPlanning" class="f-ribbon">
      <div class="f-cell">
        <span class="f-tag">{{historicalAttempt?'所选执行检出':'当前任务检出'}}</span>
        <span class="f-val mono">{{ historicalAttempt?.findingCount ?? task.findingCount }}</span>
      </div>
      <div class="f-cell">
        <span class="f-tag">Critical</span>
        <span class="f-val mono" :class="{ 'text-danger': severityCounts.critical > 0 }">{{ findingsError || findLoading ? '—' : severityCounts.critical }}</span>
      </div>
      <div class="f-cell">
        <span class="f-tag">High</span>
        <span class="f-val mono text-high">{{ findingsError || findLoading ? '—' : severityCounts.high }}</span>
      </div>
      <div class="f-cell">
        <span class="f-tag">Medium</span>
        <span class="f-val mono text-med">{{ findingsError || findLoading ? '—' : severityCounts.medium }}</span>
      </div>
      <div class="f-cell">
        <span class="f-tag">Low / Info</span>
        <span class="f-val mono text-low">{{ findingsError || findLoading ? '—' : severityCounts.low + severityCounts.info }}</span>
      </div>
    </div>

    <div class="flex items-center gap-3 mb-3 flex-wrap">
      <span class="text-xs text-muted-foreground">执行记录</span>
      <Select v-model="selectedAttempt" :options="attemptOptions" aria-label="选择执行记录" class="max-w-sm" />
      <span v-if="historicalAttempt" class="text-xs text-muted-foreground">历史日志 · {{historicalAttempt.agentId}}</span>
      <span v-if="attemptError" role="alert" class="text-xs text-destructive">执行记录读取失败 <Button size="sm" variant="ghost" @click="loadAttempts">重试</Button></span>
    </div>
    <p v-if="unavailableAttempt" role="status" class="text-xs text-muted-foreground">{{attemptsReady?`未找到指定的第 ${selectedAttempt} 次执行，请选择可用记录。`:'正在读取请求的历史执行…'}}</p>
    <details v-if="task?.validationMode==='exploitability'" class="validation-policy"><summary>本次启用独立可利用性验证</summary><p>仅源码分析；不运行项目、不发送网络请求。重试沿用本次要求。</p><p style="white-space:pre-wrap;overflow-wrap:anywhere">用户提供的业务背景（待核实）：{{task.businessContext||'未提供'}}</p></details>
      <ScanCoverage v-if="!unavailableAttempt && !task.sdkOperation && !task.componentPlanning" :scan-id="id" :attempt="historicalAttempt?.attempt ?? task.attempt" :status="historicalAttempt?.status ?? task.status" :diagnostic="historicalAttempt ? historicalAttempt.progressDiagnostic : task.progressDiagnostic" />
    <GitDiffPreview v-if="task.gitTarget && task.sourceSnapshotId" :snapshot-id="task.sourceSnapshotId" :source-hash="task.sourceContentHash" :target="task.gitTarget"/>
    <ExecutionScanInput :source-hash="task.sourceContentHash" :source-snapshot-id="task.sourceSnapshotId" :operation="task.sdkOperation" :input="task.sdkOperation?.kind==='validate'?undefined:task.scanInput" :parent-execution="task.parentExecution"/>
    <ExecutionResult :operation-kind="task.componentPlanning ? 'component_'+task.componentPlanning.kind : task.sdkOperation?.kind" v-if="!unavailableAttempt" :scan-id="id" :attempt="historicalAttempt?.attempt ?? task.attempt" :status="historicalAttempt?.status ?? task.status" :diagnostic="historicalAttempt ? historicalAttempt.progressDiagnostic : task.progressDiagnostic" />
    <ScanActivity :operation-kind="task.componentPlanning ? 'component_'+task.componentPlanning.kind : task.sdkOperation?.kind" v-if="!unavailableAttempt" :key="`${id}-${logEpoch}-${selectedAttempt}-${task.attempt}`" :scan-id="id" :attempt="historicalAttempt?.attempt ?? (task.status==='pending' ? undefined : task.attempt)" :running="isRunning && (!historicalAttempt || historicalAttempt.attempt===task.attempt)" :status="historicalAttempt?.status ?? task.status" :diagnostic="historicalAttempt ? historicalAttempt.progressDiagnostic : task.progressDiagnostic" :progress="historicalAttempt ? progressView(historicalAttempt.progress,historicalAttempt.progressJson) : progress" />

    <SDKSessionEvents v-if="isRunning && task.attempt>0 && !task.sdkOperation && !task.componentPlanning && !historicalAttempt" :scan-id="id" :attempt="task.attempt"/>
    <div v-if="!unavailableAttempt && !task.sdkOperation && !task.componentPlanning" class="split" :class="{ 'inspector-open': selected }">
      <!-- PrimaryDataSurface：findings grid -->
      <div class="surface pane">
        <div class="toolbar">
          <div class="filters">
            <Button
              v-for="c in sevChips"
              :key="c.key"
              size="sm"
              :variant="severityFilter === c.key ? 'secondary' : 'ghost'"
              @click="severityFilter = c.key"
            >
              <span>{{ c.label }}</span>
              <span class="count">{{ c.count }}</span>
            </Button>
          </div>
          <div class="search-wrap">
            <Search :size="13" class="search-ico" />
            <Input v-model="search" class="search" aria-label="搜索当前扫描漏洞" placeholder="搜索漏洞 / 文件 / CWE…" />
          </div>
        </div>

        <p v-if="historicalAttempt" class="text-xs text-muted-foreground">第 {{historicalAttempt.attempt}} 次执行的原始发现 · 只读；不代表当前人工处置状态。</p>
        <div v-if="findingsError" class="request-error" role="alert"><span>{{findingsError}}</span><Button variant="outline" size="sm" @click="loadFindings">重新读取漏洞</Button></div>
        <Loading :visible="findLoading">
          <div class="grid-head">
            <span class="col-sev">严重度</span>
            <span class="col-title">漏洞</span>
            <span class="col-loc">位置</span>
            <span class="col-state">状态</span>
          </div>
          <template v-if="filtered.length">
            <div
              v-for="f in filtered"
              :key="f.id"
              :data-finding-id="f.id"
              class="grid-row row-hover"
              :class="{ selected: selected?.id === f.id }"
              role="button" tabindex="0" :aria-pressed="selected?.id === f.id"
              @keydown="onFindingKey($event,filtered.indexOf(f))"
              @keydown.enter="selected = f" @keydown.space.prevent="selected = f"
              @click="selected = f"
            >
              <span class="col-sev">
                <span class="sev-pill" :class="f.severity.toLowerCase()">{{ f.severity }}</span>
              </span>
              <span class="col-title">
                <span class="title-text">{{ f.title }}</span>
                <span class="mono cwe">{{ f.cwe?.join(", ") }}<template v-if="f.confidence !== null"> · 置信 {{ Math.round(f.confidence * 100) }}%</template></span>
              </span>
              <span class="col-loc mono">{{ f.filePath }}{{ f.startLine ? ":" + f.startLine : "" }}</span>
              <span class="col-state" :class="f.status">{{ historicalAttempt?'历史原始发现':findingStatusLabel(f.status) }}</span>
            </div>
          </template>
          <div v-else-if="!findLoading && !findingsError" class="empty-state">
            <p class="empty-title">{{ findings.length ? "没有匹配的漏洞" : "暂无漏洞" }}</p>
            <p class="empty-hint">扫描完成后漏洞按指纹聚合在此处。</p>
          </div>
        </Loading>

        <!-- 扫描信息 & 对比：Inspector 打开时收进主面底部 -->


        <div v-if="compareError" class="request-error" role="alert"><span>{{compareError}}</span><Button variant="ghost" size="sm" @click="loadCompare">重新读取对比</Button></div>
        <details v-if="compare && compare.previousScanId" class="compare">
          <summary class="compare-head">
            <span>与上次扫描对比（#{{ compare.previousScanId }}）</span>
            <span class="compare-sums">
              <span class="sum add">＋{{ compare.added.length }} 新增</span>
              <span v-if="compare.removed.length" class="sum rm">－{{ compare.removed.length }} 不再出现</span>
              <span class="sum">={{ compare.unchanged }} 保持</span>
            </span>
          </summary>
          <ul v-if="compare.added.length" class="compare-list">
            <li v-for="f in compare.added" :key="f.id" class="compare-item">
              <span class="sev-mini" :style="{ color: severityColor(f.severity) }">{{ f.severity }}</span>
              <span class="compare-title">{{ f.title }}</span>
              <span class="mono path">{{ f.filePath }}</span>
            </li>
          </ul>
          <ul v-if="compare.removed.length" class="compare-list">
            <li v-for="f in compare.removed" :key="f.id" class="compare-item rm">
              <span class="compare-title">{{ f.title }}</span> <span class="mono path">{{ f.filePath }}</span>
            </li>
          </ul>
        </details>
      </div>

      <!-- Inspector（COMPONENTS #7）：常驻右栏，radius 0，divider 分 section -->
      <aside v-if="selected" class="inspector pane" aria-label="漏洞证据">
        <div class="insp-head">
          <div class="insp-sev-row">
            <span class="sev-pill" :class="selected.severity.toLowerCase()">{{ selected.severity.toUpperCase() }}</span>
            <span v-if="sevOf(selected).score" class="mono insp-score">
              {{ sevOf(selected).scoringSystem }} {{ sevOf(selected).score }}
            </span>
            <Button variant="ghost" size="iconSm" class="insp-close-btn" aria-label="关闭漏洞证据" @click="closeFinding"><X :size="14" :stroke-width="1.7" /></Button>
          </div>
          <h2 class="insp-title">{{ selected.title }}</h2>
          <SDKValidationAction v-if="terminal && !task.sdkOperation && !task.componentPlanning && !historicalAttempt" :scan-id="id" :attempt="task.attempt" :finding-id="selected.id" @created="sdkOperationRevision++"/>
          <div class="insp-meta mono">
            <span>{{ selected.cwe?.join(", ") }}</span>
            <span v-if="confOf(selected).level">置信 {{ confOf(selected).level }}</span>
            <span v-if="ruleIdOf(selected)" class="muted">rule: {{ ruleIdOf(selected) }}</span>
          </div>
        </div>

        <div class="insp-body">
          <p class="inference-note">以下为扫描分析结果；模型置信度不等同于独立复现验证。</p>
          <FindingEvidenceGraph :scan-id="id" :attempt="historicalAttempt?.attempt ?? task.attempt" :fingerprint="selected.fingerprint" :git-target="task.gitTarget" :source-hash="task.sourceContentHash"/>
          <div v-if="confOf(selected).rationale" class="insp-section">
            <div class="section-label">置信度依据</div>
            <p class="insp-text">{{ confOf(selected).rationale }}</p>
          </div>

          <div class="insp-section">
            <div class="section-label">描述</div>
            <p class="insp-text">{{ selected.description || "—" }}</p>
          </div>

          <!-- AttackPath（COMPONENTS #28）：序列表达 -->
          <template v-if="apOf(selected).has">
            <div class="insp-section">
              <div class="section-label">攻击路径</div>
              <p v-if="apOf(selected).summary" class="insp-text">{{ apOf(selected).summary }}</p>
              <div class="ap-chain mono">
                <template v-if="apOf(selected).entrypoint">
                  <span class="ap-node">{{ apOf(selected).entrypoint }}</span>
                  <span class="ap-arrow">→</span>
                </template>
                <span v-if="apOf(selected).source" class="ap-node muted">{{ apOf(selected).source }}</span>
                <template v-if="apOf(selected).dataflow">
                  <span v-if="apOf(selected).source || apOf(selected).entrypoint" class="ap-arrow">→</span>
                  <span class="ap-node">{{ apOf(selected).dataflow }}</span>
                </template>
                <template v-if="apOf(selected).sink">
                  <span class="ap-arrow">→</span>
                  <span class="ap-node sink">{{ apOf(selected).sink }}</span>
                </template>
              </div>
              <div v-if="apOf(selected).reachability" class="insp-text">可达性：{{ apOf(selected).reachability }}</div>
              <div v-if="apOf(selected).impact" class="insp-text">影响：{{ apOf(selected).impact }}</div>
              <div v-if="apOf(selected).likelihood" class="insp-text">可能性：{{ apOf(selected).likelihood }}</div>
              <ol v-if="apOf(selected).steps?.length" class="insp-list">
                <li v-for="(s, i) in apOf(selected).steps" :key="i">{{ s }}</li>
              </ol>
              <ul v-if="apOf(selected).preconditions?.length" class="insp-list">
                <li v-for="(p, i) in apOf(selected).preconditions" :key="i">前置：{{ p }}</li>
              </ul>
              <ul v-if="apOf(selected).assumptions?.length" class="insp-list">
                <li v-for="(a, i) in apOf(selected).assumptions" :key="i">假设：{{ a }}</li>
              </ul>
            </div>
          </template>

          <div v-if="rcOf(selected)" class="insp-section">
            <div class="section-label">根因</div>
            <p class="insp-text">{{ rcOf(selected) }}</p>
          </div>

          <!-- CodeEvidenceViewer（COMPONENTS #14） -->
          <template v-if="evOf(selected).length">
            <div class="insp-section">
              <div class="section-label">代码证据</div>
              <div v-for="(e, i) in evOf(selected)" :key="i" class="ev">
                <div v-if="e.label" class="ev-label">{{ e.label }}</div>
                <div class="ev-loc mono">{{ e.path }}:{{ e.startLine }}{{ e.endLine && e.endLine !== e.startLine ? "-" + e.endLine : "" }}</div>
                <CodeViewer :code="e.code ?? ''" :language="langOf(e.path ?? '')" :start-line="e.startLine || 1" />
                <div v-if="e.explanation" class="ev-exp">{{ e.explanation }}</div>
              </div>
            </div>
          </template>

          <div v-if="locOf(selected).length > 1" class="insp-section">
            <div class="section-label">位置</div>
            <div class="mono insp-loc">
              <div v-for="(l, i) in locOf(selected)" :key="i">
                {{ l.role ? l.role + " · " : "" }}{{ l.path }}:{{ l.startLine }}
              </div>
            </div>
          </div>

          <section v-if="componentBinding(selected.normalized?.extensions?.axiomComponent)" class="insp-section"><div class="section-label">组件证据绑定</div><p class="insp-text">组件 {{componentBinding(selected.normalized?.extensions?.axiomComponent)!.componentIndex+1}} · 冻结计划 v{{componentBinding(selected.normalized?.extensions?.axiomComponent)!.version}}</p><RouterLink :to="'/component-plans/'+componentBinding(selected.normalized?.extensions?.axiomComponent)!.planId">查看来源计划</RouterLink><details><summary>查看执行、源码与原始结果摘要</summary><pre class="ev-code">{{JSON.stringify(componentBinding(selected.normalized?.extensions?.axiomComponent),null,2)}}</pre></details></section>
          <ExploitabilityReview v-if="task?.validationMode==='exploitability'" :validation="selected.normalized?selected.normalized.validation:rawObj(selected)?.validation" />
          <template v-if="valOf(selected)">
            <div class="insp-section">
              <div class="section-label">验证</div>
              <pre class="ev-code">{{ valOf(selected) }}</pre>
            </div>
          </template>

          <div class="insp-section">
            <div class="section-label">修复建议</div>
            <p class="insp-text">{{ selected.remediation || "—" }}</p>
            <ol v-if="rtOf(selected).length" class="insp-list">
              <li v-for="(t, i) in rtOf(selected)" :key="i">{{ t }}</li>
            </ol>
            <ul v-if="pcOf(selected).length" class="insp-list">
              <li v-for="(c, i) in pcOf(selected)" :key="i">预防：{{ c }}</li>
            </ul>
          </div>

          <template v-if="proOf(selected)">
            <div class="insp-section">
              <div class="section-label">溯源</div>
              <div class="mono insp-kv">
                <div v-for="[k, v] in kvOf(proOf(selected))" :key="k">
                  <span class="k">{{ k }}:</span> {{ v }}
                </div>
              </div>
            </div>
          </template>
          <div v-if="idOf(selected) || fpOf(selected)" class="mono insp-kv muted">
            <div v-if="idOf(selected)">SDK Finding ID: {{ idOf(selected) }}</div>
            <div v-if="selected.normalized?.engineOccurrenceId">SDK Occurrence ID: {{ selected.normalized.engineOccurrenceId }}</div>
            <div v-if="fpOf(selected)">fingerprint: {{ fpOf(selected) }}</div>
          </div>
          <template v-if="extOf(selected)">
            <div class="insp-section">
              <div class="section-label">扩展字段</div>
              <div class="mono insp-kv">
                <div v-for="[k, v] in kvOf(extOf(selected))" :key="k">
                  <span class="k">{{ k }}:</span> {{ v }}
                </div>
              </div>
            </div>
          </template>

          <!-- Raw 数据入口（AGENTS #36） -->
          <button type="button" class="raw-toggle" :aria-expanded="rawOpen" aria-controls="raw-finding-content" @click="rawOpen = !rawOpen">
            <ChevronRight :size="13" class="raw-chev" :class="{ open: rawOpen }" />
            <span class="section-label" style="margin: 0">原始 JSON（上游完整对象）</span>
          </button>
          <div v-if="rawOpen" id="raw-finding-content" role="region" aria-label="原始漏洞 JSON"><CodeViewer :code="prettyRaw" language="json" :max-height="400" /></div>
        </div>

        <!-- Inspector Actions -->
        <div class="insp-foot">
          <span class="section-label" style="margin: 0">标记状态</span>
          <span v-if="historicalAttempt">历史原始发现 · 只读</span>
          <span v-else-if="!canOperate">{{FINDING_STATUS[selected.status || 'open'] || selected.status}} · 只读</span>
          <Select v-else
            aria-label="漏洞处理状态"
            :disabled="statusBusy"
            :model-value="selected.status || 'open'"
            :options="statusOptions"
            class="w-40"
            @change="(v: string) => onStatusChange(selected!, v)"
          />
        </div>
      </aside>
    </div>
  </div>
  <div v-else-if="loadError" class="request-error" role="alert"><span>扫描详情加载失败：{{loadError}}</span><Button variant="outline" @click="load">重试</Button></div>
  <Loading v-else text="正在加载扫描详情…"/>
</template>

<style scoped>

.deep-policy-summary{font-size:var(--font-size-xs);color:var(--text-tertiary);line-height:var(--line-height-body);margin:0 0 var(--space-4);overflow-wrap:anywhere}
/* WorkspaceHeader */
.exec-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: var(--space-5);
  flex-wrap: wrap;
  margin-bottom: var(--space-5); /* 与 PageHeader 一致：无下划线 */
}
.exec-head-main {
  flex: 1;
  min-width: 0;
}
.title-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}
.crumb-link {
  color: var(--text-tertiary);
  font-size: var(--font-size-sm);
  cursor: pointer;
}
.crumb-link:hover {
  color: var(--text-primary);
}
.crumb-sep {
  color: var(--text-disabled);
}
.exec-title {
  margin: 0;
  font-size: var(--font-size-title); /* 与 PageHeader 同字号 */
  font-weight: var(--font-weight-semibold);
  letter-spacing: -0.01em;
  color: var(--text-primary);
}
.exec-sub {
  margin-top: var(--space-1-5);
  font-size: var(--font-size-sm);
  color: var(--text-tertiary);
  display: flex;
  gap: var(--space-2);
  flex-wrap: wrap;
  align-items: baseline;
}
.sep {
  opacity: 0.6;
}
.exec-actions {
  display: flex;
  gap: var(--space-2);
  flex-shrink: 0;
  align-self: flex-start;
}
.src-path {
  cursor: default;
  text-decoration: underline;
  text-decoration-color: var(--border-default);
  text-underline-offset: 3px;
}

/* 摘要指标带 */
.f-ribbon {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  background: var(--surface-primary);
  border: 1px solid var(--border-default);
  margin-bottom: var(--space-4);
  overflow: hidden;
}
.f-cell {
  padding: var(--space-2-5) var(--space-3);
  border-right: 1px solid var(--border-default);
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.f-cell:last-child {
  border-right: none;
}
.f-tag {
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.06em;
  color: var(--text-tertiary);
}
.f-val {
  font-size: 18px;
  font-weight: 700;
  color: var(--text-primary);
  line-height: 1;
}
.text-danger { color: var(--severity-critical); }
.text-high { color: var(--severity-high); }
.text-med { color: var(--severity-medium); }
.text-low { color: var(--severity-low); }

/* 活动流（ActivityStream,COMPONENTS #18） */
.live {
  margin-bottom: var(--space-4);
  border: 1px solid var(--border-default);
  background: var(--surface-primary);
}
.live-progress {
  display: flex;
  align-items: baseline;
  gap: var(--space-3);
  flex-wrap: wrap;
  padding: var(--space-2) var(--space-3);
  border-bottom: 1px solid var(--border-default);
  background: var(--surface-secondary);
}
.phase {
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  color: var(--execution-running);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.live-meta {
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  font-variant-numeric: tabular-nums;
}
.live-activity {
  font-size: var(--font-size-xs);
  color: var(--text-tertiary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
  min-width: 0;
}
.sandbox-warn {
  color: var(--state-warning);
  font-weight: var(--font-weight-semibold);
}
.activity-label {
  font-weight: var(--font-weight-semibold);
  color: var(--text-secondary);
}
.live-head {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  padding: var(--space-1-5) var(--space-3);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-medium);
  color: var(--text-secondary);
  border-bottom: 1px solid var(--border-subtle);
}
.live-count {
  font-size: var(--font-size-2xs);
  color: var(--text-tertiary);
}
.live-log {
  max-height: 200px;
  overflow-y: auto;
  padding: var(--space-2) var(--space-3);
  font-size: var(--font-size-xs);
  line-height: 1.8;
  background: var(--surface-secondary);
  font-family: var(--font-mono);
}
.log-line {
  display: flex;
  gap: var(--space-2-5);
  align-items: baseline;
  color: var(--text-secondary);
}
.log-t {
  color: var(--text-tertiary);
  flex-shrink: 0;
  font-variant-numeric: tabular-nums;
}
.log-k {
  color: var(--text-tertiary);
  min-width: 64px;
  flex-shrink: 0;
  font-size: var(--font-size-2xs);
  letter-spacing: 0.04em;
}
.log-m {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.log-line.warn .log-m {
  color: var(--state-warning);
}
.log-line.err .log-m {
  color: var(--state-danger);
}
.log-empty {
  padding: var(--space-3) 0;
  text-align: center;
  color: var(--text-tertiary);
}

/* Split:主面 + Inspector */
.split {
  display: grid;
  grid-template-columns: 1fr;
  gap: 0;
  min-width: 0;
}
.split.inspector-open {
  grid-template-columns: minmax(0, 1fr) var(--inspector-width-md);
  gap: var(--space-4);
}

/* 主数据面 */
.surface {
  overflow: hidden;
}
.surface .toolbar {
  padding: var(--space-2-5) var(--space-3);
  border-bottom: 1px solid var(--border-default);
  margin-bottom: 0;
}
.toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-height: var(--height-toolbar);
  flex-wrap: wrap;
}
.filters {
  display: inline-flex;
  align-items: center;
  background: var(--surface-secondary);
  border: 1px solid var(--border-default);
  border-radius: var(--radius-sm);
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
  border-radius: var(--radius-xs); /* 与 Findings/ScanList 的 count 徽章一致 */
  background: var(--surface-tertiary);
  color: var(--text-tertiary);
  font-weight: 500;
}
.search-wrap {
  margin-left: auto;
  position: relative;
  width: 240px;
}
.search-ico {
  position: absolute;
  left: 9px;
  top: 50%;
  transform: translateY(-50%);
  color: var(--text-tertiary);
  pointer-events: none;
  z-index: 1;
}
.search {
  padding-left: 26px;
}

.grid-head,
.grid-row {
  display: grid;
  grid-template-columns: 90px minmax(240px, 1.2fr) minmax(160px, 1fr) 110px;
  gap: var(--space-3);
  align-items: center;
}
.grid-head {
  font-size: var(--font-size-2xs);
  font-weight: 600;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--text-tertiary);
  border-bottom: 1px solid var(--border-default);
  padding: 0 var(--space-3);
  height: 32px;
  background: var(--surface-secondary);
}
.grid-row {
  min-height: var(--height-table-row);
  padding: var(--space-2) var(--space-3);
  border-bottom: 1px solid var(--border-subtle);
  cursor: pointer;
}
.grid-row:last-child {
  border-bottom: none;
}
.grid-row.selected {
  background: var(--surface-selected);
}
.sev-mark {
  font-size: 8px;
  margin-right: 2px;
}
.sev-text {
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-semibold);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.col-title {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.title-text {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  color: var(--text-primary);
  line-height: var(--line-height-ui);
}
.cwe {
  font-size: var(--font-size-2xs);
  color: var(--text-tertiary);
}
.col-loc {
  font-size: var(--font-size-xs);
  color: var(--text-tertiary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.col-state {
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
}
.col-state.confirmed {
  color: var(--state-warning);
}
.col-state.false_positive {
  color: var(--text-tertiary);
  text-decoration: line-through;
}
.col-state.fixed {
  color: var(--state-success);
}

/* 错误 / 对比 */
.error-box {
  margin-top: var(--space-4);
  padding: var(--space-2-5) var(--space-3);
  border: 1px solid var(--state-danger);
  background: var(--state-danger-bg);
  font-size: var(--font-size-xs);
  color: var(--state-danger);
  display: flex;
  gap: var(--space-2-5);
  word-break: break-all;
}
.compare {
  margin-top: var(--space-5);
  padding-top: var(--space-3);
  border-top: 1px solid var(--border-default);
}
.compare-head {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: var(--space-4);
  flex-wrap: wrap;
  font-weight: var(--font-weight-semibold);
  font-size: var(--font-size-sm);
}
.compare-sums {
  display: flex;
  gap: var(--space-3);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-medium);
}
.sum {
  color: var(--text-secondary);
}
.sum.add {
  color: var(--state-danger);
}
.sum.rm {
  color: var(--text-tertiary);
}
.compare-list {
  margin-top: var(--space-3);
  display: flex;
  flex-direction: column;
  gap: var(--space-1-5);
  padding-top: var(--space-2-5);
  border-top: 1px solid var(--border-subtle);
}
.compare-item {
  font-size: var(--font-size-sm);
  color: var(--text-primary);
  display: flex;
  gap: var(--space-2);
  align-items: baseline;
}
.compare-item.rm {
  color: var(--text-tertiary);
}
.sev-mini {
  font-size: var(--font-size-2xs);
  text-transform: uppercase;
  font-weight: var(--font-weight-semibold);
}
.path {
  font-size: var(--font-size-xs);
  color: var(--text-tertiary);
}

/* Inspector */
.inspector {
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.insp-head {
  padding: var(--space-4);
  border-bottom: 1px solid var(--border-subtle);
}
.insp-sev-row {
  display: flex;
  align-items: center;
  gap: var(--space-2-5);
}
.insp-score {
  font-size: var(--font-size-2xs);
  color: var(--text-tertiary);
}
.insp-close-btn {
  margin-left: auto;
}
.insp-title {
  margin: var(--space-2) 0 var(--space-1-5);
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
  line-height: var(--line-height-ui);
}
.insp-meta {
  display: flex;
  gap: var(--space-3);
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  flex-wrap: wrap;
}
.insp-meta .muted {
  color: var(--text-tertiary);
}
.insp-body {
  flex: 1;
  overflow-y: auto;
  padding: var(--space-4);
}
.insp-section {
  padding-bottom: var(--space-3);
  margin-bottom: var(--space-3);
  border-bottom: 1px solid var(--border-subtle);
}
.insp-text {
  margin: 0;
  font-size: var(--font-size-sm);
  line-height: var(--line-height-body);
  color: var(--text-secondary);
}
.insp-list {
  margin: var(--space-2) 0 0;
  padding-left: var(--space-5);
  font-size: var(--font-size-sm);
  line-height: var(--line-height-body);
  color: var(--text-secondary);
}
.insp-loc {
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  line-height: 1.8;
}
.insp-kv {
  font-size: var(--font-size-xs);
  line-height: 1.8;
  color: var(--text-secondary);
  word-break: break-all;
}
.insp-kv .k {
  color: var(--text-tertiary);
}
.insp-kv.muted {
  color: var(--text-tertiary);
}
/* CodeEvidence：mono 块，浅底无阴影 */
.ev {
  margin-bottom: var(--space-3);
}
.ev-label {
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-medium);
  color: var(--text-secondary);
  margin-bottom: var(--space-0-5);
}
.ev-loc {
  font-size: var(--font-size-2xs);
  color: var(--text-tertiary);
  margin-bottom: var(--space-1-5);
}
.ev-code {
  margin: 0;
  padding: var(--space-2-5) var(--space-3);
  background: var(--surface-secondary);
  border: 1px solid var(--border-default);

  border-radius: var(--radius-xs);
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  line-height: 1.6;
  color: var(--text-primary);
  overflow-x: auto;
  white-space: pre-wrap;
  word-break: break-all;
}
.ev-exp {
  margin-top: var(--space-1-5);
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
}
.raw-toggle {
  display: flex;
  align-items: center;
  gap: var(--space-1-5);
  padding-top: var(--space-2-5);
  border-top: 1px solid var(--border-subtle);
  cursor: pointer;
  user-select: none;
}
.raw-toggle:hover .section-label {
  color: var(--text-primary);
}
.raw-chev {
  color: var(--text-tertiary);
  transition: transform var(--duration-fast) var(--ease-ui);
}
.raw-chev.open {
  transform: rotate(90deg);
}
/* AttackPath */
.ap-chain {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--space-2);
  font-size: var(--font-size-xs);
  margin-bottom: var(--space-2);
}
.ap-node {
  padding: 1px var(--space-1-5);
  border-radius: var(--radius-xs);
  background: var(--surface-secondary);
  border: 1px solid var(--border-subtle);
  max-width: 100%;
  word-break: break-all;
}
.ap-node.sink {
  background: var(--state-danger-bg);
  border-color: var(--state-danger);
  color: var(--state-danger);
}
.ap-arrow {
  color: var(--text-tertiary);
}
.insp-foot {
  padding: var(--space-3) var(--space-4);
  border-top: 1px solid var(--border-subtle);
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

@media (max-width: 1366px) {
  .split.inspector-open .col-loc {
    display: none;
  }
  .split.inspector-open .grid-head,
  .split.inspector-open .grid-row {
    grid-template-columns: 80px minmax(180px, 1fr) 90px;
  }
}

@media (max-width: 1200px) {
  .split.inspector-open {
    grid-template-columns: 1fr;
  }
}

.exec-head{align-items:flex-start;margin-bottom:var(--space-6)}.exec-title{font-family:var(--font-ui);font-size:var(--font-size-title);letter-spacing:-.02em}.exec-sub{line-height:1.8}.f-ribbon{border-radius:var(--radius-md);overflow:hidden}.split.inspector-open{grid-template-columns:320px minmax(0,1fr);gap:0;border:1px solid var(--border-default);border-radius:var(--radius-lg);overflow:hidden;background:var(--surface-primary)}.split.inspector-open>.surface{border:0;border-right:1px solid var(--border-default);border-radius:0}.split.inspector-open>.inspector{border:0;border-radius:0;min-width:0;max-height:none;position:static}.surface{border-radius:var(--radius-lg)}.split.inspector-open .grid-head{display:none}.split.inspector-open .grid-row{grid-template-columns:1fr auto;gap:var(--space-2);padding:var(--space-4);min-height:110px}.split.inspector-open .col-title{grid-row:2;grid-column:1/-1}.split.inspector-open .col-loc{display:block;grid-row:3;grid-column:1/-1;white-space:normal;overflow-wrap:anywhere}.split.inspector-open .col-state{grid-row:1;grid-column:2}.split.inspector-open .col-sev{grid-row:1;grid-column:1}.split.inspector-open .toolbar{display:flex;flex-direction:column;align-items:stretch;padding:var(--space-3)}.split.inspector-open .filters{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));width:100%;max-width:100%;overflow:visible;gap:var(--space-1);border:0;background:transparent;padding:0}.split.inspector-open .filters :deep(button){font-size:var(--font-size-2xs);padding:0 var(--space-1-5);flex-shrink:0}.split.inspector-open .filters .count{padding:0;background:transparent}.split.inspector-open .search-wrap{width:100%;margin:0}.insp-head{padding:var(--space-6);border-bottom:1px solid var(--border-default)}.insp-title{font-size:var(--font-size-xl);line-height:1.5;margin-top:var(--space-3)}.insp-body{padding:var(--space-6);overflow:visible}.insp-section{padding-bottom:var(--space-5);margin-bottom:var(--space-4)}.insp-text{line-height:1.85;overflow-wrap:anywhere}.inference-note{font-size:var(--font-size-xs);color:var(--text-tertiary);margin:0 0 var(--space-5);line-height:1.8}.insp-foot{padding:var(--space-4) var(--space-6)}.compare{padding:var(--space-4)}.compare-item{overflow-wrap:anywhere}.grid-row{min-height:58px}.grid-row:focus-visible{outline-offset:-2px}.live{border-radius:var(--radius-md);overflow:hidden}.filters{max-width:100%;flex-wrap:wrap;min-width:0}.title-row{flex-wrap:wrap}.exec-actions{flex-wrap:wrap}
@media(max-width:1000px){.split.inspector-open{grid-template-columns:260px minmax(0,1fr)}.insp-head,.insp-body{padding:var(--space-4)}.split:not(.inspector-open) .grid-head,.split:not(.inspector-open) .grid-row{grid-template-columns:75px minmax(0,1fr) 80px}.split:not(.inspector-open) .col-loc{display:none}}
@media(max-width:760px){.split.inspector-open{grid-template-columns:1fr}.split.inspector-open>.surface{border:0;border-bottom:1px solid var(--border-default)}.split.inspector-open .grid-row{min-height:0}.split.inspector-open .col-loc{grid-row:3}.split.inspector-open .toolbar{padding:var(--space-3)}.f-ribbon{flex-wrap:wrap}.f-cell{min-width:90px;flex:1}.exec-title{font-size:var(--font-size-xl);overflow-wrap:anywhere}.exec-sub{overflow-wrap:anywhere}.insp-title{font-size:var(--font-size-lg)}.search-wrap{width:100%;margin:0}.grid-head,.grid-row{min-width:0}.insp-foot{padding:var(--space-4)}.filters{width:100%}}
@media(max-width:760px){
 .exec-head{flex-direction:column;align-items:stretch;gap:var(--space-3)}
 .exec-head-main{width:100%}.exec-title{flex-basis:100%;order:3;min-width:0}.title-row{gap:var(--space-2)}
 .exec-actions{width:100%;justify-content:flex-start}.split.inspector-open{grid-template-columns:minmax(0,1fr)}
 .f-cell{min-width:0}.exec{min-width:0;max-width:100%}
 .toolbar{flex-wrap:wrap}.grid-row{overflow-wrap:anywhere}
}

.split.inspector-open .filters :deep(button){width:100%;min-width:0;font-size:var(--font-size-xs);padding:0 var(--space-1);height:32px}
.split.inspector-open .filters :deep(.btn-inner){width:100%;justify-content:space-between;gap:var(--space-1)}
.compare-head{cursor:pointer;gap:var(--space-2)}.compare-sums{flex-wrap:wrap;gap:var(--space-2);font-weight:400}.compare-list{padding-left:0;list-style:none}
.compare-item{display:grid;grid-template-columns:auto minmax(0,1fr);gap:var(--space-1) var(--space-2);padding:var(--space-2) 0}
.compare-item .sev-mini{white-space:nowrap;font-size:var(--font-size-xs)}.compare-title{min-width:0;overflow-wrap:anywhere}
.compare-item .path{grid-column:1/-1;color:var(--text-tertiary);overflow-wrap:anywhere}.compare-item.rm .compare-title{grid-column:1/-1}
.insp-sev-row{flex-wrap:wrap}.insp-head,.insp-body{padding:var(--space-5)}.insp-section{padding-bottom:var(--space-4);margin-bottom:var(--space-4)}
</style>
