<script setup lang="ts">
import {ref,shallowRef,watch,onBeforeUnmount,nextTick} from 'vue';
import type {ScanTask} from '../types';
import type {ScanRecoverySnapshot,ScanRecoveryAction} from '../contracts';
import {api,ApiError} from '../api';
import {recoveryView,recoveryAction} from '../utils/scan-recovery';
import {absoluteTime} from '../utils/time';
import Button from './ui/Button.vue';
import Dialog from './ui/Dialog.vue';

const props=defineProps<{task:ScanTask;canOperate:boolean;budgetRevision?:number}>();
const emit=defineEmits<{changed:[task:ScanTask,action:ScanRecoveryAction['action']]}>();
const snapshot=shallowRef<ScanRecoverySnapshot|null>(null),confirmation=shallowRef<ScanRecoverySnapshot|null>(null);
const expanded=ref(['failed','partial','canceled','timeout'].includes(props.task.status));
const loading=ref(false),busy=ref(false),error=ref(''),actionError=ref(''),restartOpen=ref(false),acknowledged=ref(false);
let epoch=0,disposed=false;
onBeforeUnmount(()=>{disposed=true;epoch++});

async function load():Promise<ScanRecoverySnapshot|null>{
 const run=++epoch,{id,attempt,status}=props.task;loading.value=true;error.value='';
 try {
  const value=recoveryView(await api.scanRecovery(id),id,attempt,status);
  if(disposed||run!==epoch)return null;
  if(!value)throw new Error('Server 返回的恢复合同或执行身份不匹配，操作已停用；历史结果仍可读取。');
  snapshot.value=value;return value;
 } catch(e){if(!disposed&&run===epoch){snapshot.value=null;error.value=e instanceof ApiError&&e.status===404?'当前 Server 尚未提供扫描恢复入口，请升级配套 Server；历史任务与报告仍可读取。':(e as Error).message;}return null;}
 finally{if(!disposed&&run===epoch)loading.value=false;}
}
watch(()=>[props.task.id,props.task.attempt,props.task.status,props.budgetRevision],()=>{snapshot.value=null;if(expanded.value||restartOpen.value)void load();});
watch(expanded,open=>{if(open&&!loading.value)void load();},{immediate:true});

async function requestRestart(){
 if(!props.canOperate||busy.value)return;
 expanded.value=true;
 const value=await load();
 if(!value||!value.restart.allowed)return;
 confirmation.value=value;acknowledged.value=false;actionError.value='';restartOpen.value=true;
}
defineExpose({requestRestart});

async function act(action:ScanRecoveryAction['action']){
 if(!props.canOperate||busy.value||(action==='restart'&&!acknowledged.value))return;
 const observed=action==='restart'?confirmation.value:snapshot.value;
 const request=observed?recoveryAction(observed,action):null;
 if(!request)return;
 busy.value=true;actionError.value='';
 try{
  const next=await api.recoverScan(props.task.id,request);
  if(disposed)return;
  restartOpen.value=false;confirmation.value=null;emit('changed',next,action);
 }catch(e){
  if(!disposed)actionError.value=`${action==='restart'?'重新执行':action==='restore_result'?'恢复结果入库':'登记中断'}未完成：${(e as Error).message}`;
 }finally{if(!disposed){busy.value=false;await nextTick();if(!disposed&&!restartOpen.value)void load();}}
}
</script>

<template>
 <section class="execution-recovery" aria-label="扫描中断与恢复">
  <button type="button" class="recovery-heading" :aria-expanded="expanded" @click="expanded=!expanded">扫描中断与恢复 <span>{{expanded?'收起':'查看恢复方式'}}</span></button>
  <template v-if="expanded">
   <p class="recovery-note">关闭或刷新浏览器不会停止后台扫描。断点续扫会复用原 SDK 会话与已完成工作；重新执行会启动新的会话。</p>
   <p v-if="loading" role="status">正在核验当前执行与恢复证据…</p>
   <div v-if="error" class="recovery-error" role="alert">{{error}} <Button variant="ghost" size="sm" :disabled="loading" @click="load">重新核验</Button></div>
   <template v-if="snapshot">
    <dl class="recovery-properties">
     <div><dt>断点续扫</dt><dd><strong>SDK 0.1.25 不支持</strong><p>{{snapshot.checkpointResume.reason}}</p></dd></div>
     <div><dt>原执行结果</dt><dd><p>{{snapshot.committedResult.reason}}</p><Button v-if="canOperate && snapshot.committedResult.state==='recoverable'" variant="outline" size="sm" :disabled="busy||loading" :loading="busy" @click="act('restore_result')">恢复结果入库</Button><details v-if="snapshot.committedResult.sha256"><summary>提交结果 SHA-256</summary><code>{{snapshot.committedResult.sha256}}</code></details></dd></div>
     <div v-if="snapshot.interruption.allowed"><dt>执行失联</dt><dd><p>{{snapshot.interruption.reason}}</p><Button v-if="canOperate" variant="outline" size="sm" :disabled="busy||loading" @click="act('interrupt_expired')">登记租约过期中断</Button></dd></div>
     <div><dt>重新执行</dt><dd><p>{{snapshot.restart.reason}}</p><Button v-if="canOperate" variant="outline" size="sm" :disabled="busy||loading||!snapshot.restart.allowed" @click="requestRestart">重新执行…</Button></dd></div>
    </dl>
    <p v-if="snapshot.recovery" class="recovery-note">结果已恢复入库 · 原第 {{snapshot.execution.attempt}} 次执行 · {{absoluteTime(snapshot.recovery.recoveredAt)}} · 操作者 {{snapshot.recovery.recoveredBy}}。未启动新扫描；原中断状态与错误已保留在恢复记录中。</p>
    <details class="recovery-raw"><summary>恢复状态原始 JSON</summary><pre>{{JSON.stringify(snapshot,null,2)}}</pre></details>
   </template>
   <p v-if="!canOperate" class="recovery-note">当前账户可读取恢复证据；执行恢复与重新执行需要操作权限。</p>
   <p v-if="actionError&&!restartOpen" role="alert" class="recovery-error">{{actionError}}</p>
  </template>
  <Dialog v-model:open="restartOpen" title="确认重新执行扫描" :description="task.inputContractVersion?'此操作会启动新的 SDK 会话，沿用冻结的源码、范围与模型配置。':'此操作会启动新的 SDK 会话，沿用历史任务的源码路径与配置规则。'">
   <div class="restart-confirmation">
    <p>重新执行可能重复分析并再次产生费用。已有历史、报告与证据保留；设置了任务总预算时，新尝试按剩余额度下发，单次上限保留原有设置。</p>
    <p v-if="confirmation?.committedResult.state==='recoverable'">当前有完整提交结果，可返回页面选择「恢复结果入库」，该操作无需调用模型。</p>
    <label class="restart-ack"><input v-model="acknowledged" type="checkbox" :disabled="busy">我已知悉这会重新调用模型并可能重复产生费用</label>
    <p v-if="actionError" role="alert" class="recovery-error">{{actionError}}</p>
   </div>
   <template #footer><Button variant="outline" :disabled="busy" @click="restartOpen=false">返回</Button><Button :disabled="!acknowledged||busy" :loading="busy" @click="act('restart')">确认重新执行</Button></template>
  </Dialog>
 </section>
</template>

<style scoped>
.execution-recovery{min-width:0;border-block:1px solid var(--border-default);margin-block:var(--space-4);font-size:var(--font-size-xs);color:var(--text-secondary)}
.recovery-heading{display:flex;align-items:center;gap:var(--space-3);width:100%;padding:var(--space-2) 0;text-align:left;color:var(--text-primary);font-weight:500;background:none;border:0;cursor:pointer}.recovery-heading span{font-weight:400;color:var(--text-secondary)}.recovery-heading:focus-visible{outline:2px solid var(--border-focus);outline-offset:2px}
.recovery-note,.recovery-properties p,.restart-confirmation p{line-height:var(--line-height-body);margin:var(--space-2) 0;overflow-wrap:anywhere}
.recovery-properties{margin:var(--space-3) 0}.recovery-properties>div{display:grid;grid-template-columns:minmax(80px,100px) minmax(0,1fr);gap:var(--space-3);padding-block:var(--space-2);border-top:1px solid var(--border-subtle)}dt{color:var(--text-primary);font-weight:500}dd{margin:0;min-width:0}dd p:first-child{margin-top:0}strong{font-weight:500;color:var(--text-primary)}summary{cursor:pointer}.recovery-properties details,.recovery-raw{margin:var(--space-2) 0}code,pre{font-family:var(--font-mono);overflow-wrap:anywhere;white-space:pre-wrap}pre{background:var(--surface-secondary);padding:var(--space-3);max-height:280px;overflow:auto}.recovery-error{color:var(--state-danger);line-height:var(--line-height-body);margin:var(--space-2) 0;overflow-wrap:anywhere}.restart-confirmation{font-size:var(--font-size-sm);color:var(--text-secondary)}.restart-ack{display:flex;align-items:flex-start;gap:var(--space-2);line-height:var(--line-height-body);color:var(--text-primary);margin-top:var(--space-4)}input{accent-color:var(--accent-primary);flex:none}input:focus-visible{outline:2px solid var(--border-focus);outline-offset:2px}
@media(max-width:640px){.recovery-properties>div{grid-template-columns:1fr;gap:var(--space-1)}}
</style>
