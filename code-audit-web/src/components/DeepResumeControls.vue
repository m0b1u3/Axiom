<script setup lang="ts">
import {shallowRef,ref,watch,onBeforeUnmount} from 'vue';
import type {ScanTask} from '../types';
import type {DeepResumeSnapshot} from '../contracts';
import {api,ApiError} from '../api';
import {deepResumeView,deepResumeAction} from '../utils/deep-resume';
import {absoluteTime} from '../utils/time';
import Button from './ui/Button.vue';import Dialog from './ui/Dialog.vue';
const props=defineProps<{task:ScanTask;canOperate:boolean;budgetRevision?:number}>();const emit=defineEmits<{changed:[task:ScanTask]}>();
const snapshot=shallowRef<DeepResumeSnapshot|null>(null),confirmation=shallowRef<DeepResumeSnapshot|null>(null),loading=ref(false),busy=ref(false),open=ref(false),ack=ref(false),error=ref(''),actionError=ref('');let epoch=0,disposed=false,timer:ReturnType<typeof setTimeout>|undefined;
function poll(){clearTimeout(timer);if(!disposed&&['queued','checking','ready','pending'].includes(snapshot.value?.state??''))timer=setTimeout(()=>void load(),3000)}
async function load(){const run=++epoch,id=props.task.id,attempt=props.task.attempt;loading.value=true;try{const v=deepResumeView(await api.deepResume(id),id,attempt,props.task.status,props.task.sourceContentHash??'');if(disposed||run!==epoch)return;if(!v)throw new Error('续扫合同或执行身份不匹配，操作已停用；历史仍可读取。');snapshot.value=v;error.value='';}catch(e){if(!disposed&&run===epoch){snapshot.value=null;error.value=e instanceof ApiError&&e.status===404?'当前 Server 尚未提供 Deep 续扫；请同步更新配套组件，历史报告仍可读取。':(e as Error).message;}}finally{if(!disposed&&run===epoch){loading.value=false;poll()}}}
watch(()=>[props.task.id,props.task.attempt,props.task.status,props.budgetRevision],()=>{epoch++;snapshot.value=null;confirmation.value=null;open.value=false;ack.value=false;actionError.value='';void load()},{immediate:true});onBeforeUnmount(()=>{disposed=true;epoch++;clearTimeout(timer)});
async function check(){if(!props.canOperate||busy.value||!snapshot.value?.canCheck)return;busy.value=true;error.value='';const run=epoch,id=props.task.id,execution=snapshot.value.execution;try{const v=deepResumeView(await api.checkDeepResume(id,execution),id,props.task.attempt,props.task.status,props.task.sourceContentHash??'');if(!disposed&&run===epoch&&id===props.task.id){if(!v)throw new Error('恢复检查响应不匹配');snapshot.value=v;poll()}}catch(e){if(!disposed)error.value='恢复检查未完成：'+(e as Error).message;}finally{if(!disposed)busy.value=false}}
function confirm(){if(!props.canOperate||!snapshot.value?.canResume||busy.value)return;confirmation.value=snapshot.value;ack.value=false;open.value=true;error.value='';actionError.value=''}
async function resume(){if(!props.canOperate||!ack.value||busy.value||!confirmation.value)return;const action=deepResumeAction(confirmation.value);if(!action||action.execution.taskId!==String(props.task.id)||action.execution.attempt!==props.task.attempt)return;const run=epoch,id=props.task.id;busy.value=true;try{const next=await api.resumeDeep(id,action);if(!disposed&&run===epoch&&id===props.task.id){open.value=false;emit('changed',next)}}catch(e){if(!disposed){actionError.value='断点续扫未启动：'+(e as Error).message;confirmation.value=null;ack.value=false;void load()}}finally{if(!disposed)busy.value=false}}
</script>
<template>
 <section class="deep-resume" aria-label="Deep 原会话续扫">
  <p v-if="loading&&!snapshot" role="status">正在读取 Deep 恢复状态…</p>
  <template v-if="snapshot">
   <strong>SDK 0.5.0 · Deep 原会话续扫</strong><p role="status" aria-live="polite">{{snapshot.reason}}</p>
   <dl v-if="snapshot.inspection" class="resume-binding"><div><dt>最近检查</dt><dd>{{absoluteTime(snapshot.inspection.checkedAt)}}</dd></div><div><dt>原 Agent</dt><dd>{{snapshot.inspection.agentId}}</dd></div><div v-if="snapshot.inspection.scanId"><dt>原 Scan ID</dt><dd><code>{{snapshot.inspection.scanId}}</code></dd></div><div v-if="snapshot.inspection.threadId"><dt>原会话</dt><dd><code>{{snapshot.inspection.threadId}}</code></dd></div><div><dt>已观察累计估算</dt><dd>{{snapshot.inspection.baseline.estimatedUsd===null?'未知，不按零费用处理':new Intl.NumberFormat(undefined,{style:'currency',currency:'USD',maximumFractionDigits:6}).format(snapshot.inspection.baseline.estimatedUsd)}}</dd></div></dl>
   <p v-if="snapshot.continuation">本次复用第 {{snapshot.continuation.inspection.origin.attempt}} 次执行的 SDK 会话与检查点，接续第 {{snapshot.continuation.previous.attempt}} 次执行。原生结果包含累计用量；任务费用只登记本次新增估算，原未知费用仍保留。</p>
   <div v-if="canOperate" class="resume-actions"><Button variant="outline" size="sm" :disabled="busy||loading||!snapshot.canCheck||['queued','checking','pending','running'].includes(snapshot.state)" :loading="busy" @click="check">检查续扫条件</Button><Button v-if="snapshot.canResume" size="sm" :disabled="busy||loading" @click="confirm">断点续扫…</Button><Button variant="ghost" size="sm" :disabled="busy||loading" @click="load">刷新恢复状态</Button></div>
   <details><summary>Deep 恢复状态原始 JSON</summary><pre>{{JSON.stringify(snapshot,null,2)}}</pre></details>
  </template>
  <p v-if="error" role="alert" class="resume-error">{{error}} <Button variant="ghost" size="sm" :disabled="loading||busy" @click="load">重新读取</Button></p>
  <Dialog v-model:open="open" title="确认 Deep 断点续扫" description="复用原 Scan ID、Codex 会话和检查点，仅原 Agent 可执行。">
   <div class="resume-confirm"><p>原执行记录与中间证据会保留，续扫创建新的平台执行记录。完成的 Worker 是否复用由 SDK 检查点决定，不保证完全没有重复分析。</p><p>SDK 保留原 Deep 发现阶段的起始时间，续扫不会重置该阶段的计时。</p><p>续扫仍会调用模型并产生新增费用。任务总预算按剩余额度下发，保留单次上限；原未知费用保持未知，不能按零计费。</p><p v-if="confirmation?.inspection">原执行第 {{confirmation.inspection.origin.attempt}} 次 · 原 Agent {{confirmation.inspection.agentId}} · 原 Scan ID <code>{{confirmation.inspection.scanId}}</code></p><label><input v-model="ack" type="checkbox" :disabled="busy">我确认复用原会话，并知悉续扫可能产生新增费用</label><p v-if="actionError" role="alert" class="resume-error">{{actionError}} 请返回并重新核验。</p></div>
   <template #footer><Button variant="outline" :disabled="busy" @click="open=false">返回</Button><Button :disabled="!canOperate||!ack||busy||!confirmation" :loading="busy" @click="resume">确认断点续扫</Button></template>
  </Dialog>
 </section>
</template>
<style scoped>
.deep-resume{min-width:0}.deep-resume p{margin-block:var(--space-2);line-height:var(--line-height-body);overflow-wrap:anywhere}.deep-resume strong{font-weight:500;color:var(--text-primary)}.resume-actions{display:flex;flex-wrap:wrap;gap:var(--space-2);margin-block:var(--space-2)}.resume-binding{margin-block:var(--space-3)}.resume-binding>div{display:grid;grid-template-columns:minmax(96px,120px) minmax(0,1fr);gap:var(--space-2);padding-block:var(--space-1)}dd{margin:0;min-width:0;overflow-wrap:anywhere}dt{color:var(--text-tertiary)}code,pre{font-family:var(--font-mono);overflow-wrap:anywhere;white-space:pre-wrap}pre{max-height:280px;overflow:auto;background:var(--surface-secondary);padding:var(--space-3)}summary{cursor:pointer;margin-block:var(--space-2)}.resume-error{color:var(--state-danger)}.resume-confirm{font-size:var(--font-size-sm);color:var(--text-secondary)}.resume-confirm label{display:flex;gap:var(--space-2);align-items:flex-start;margin-top:var(--space-4);line-height:var(--line-height-body);color:var(--text-primary)}input{accent-color:var(--accent-primary);flex:none}input:focus-visible{outline:2px solid var(--border-focus);outline-offset:2px}@media(max-width:640px){.resume-binding>div{grid-template-columns:1fr;gap:var(--space-1)}}
</style>
