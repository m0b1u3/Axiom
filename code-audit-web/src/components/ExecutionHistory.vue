<script setup lang="ts">
import {computed,ref,shallowRef,watch} from 'vue';
import type {ScanTask,ScanAttempt,AttemptResult,QueueOverview} from '../types';
import {api} from '../api';
import {elapsed,queueReasons,failureHelp} from '../utils/execution';
import {absoluteTime} from '../utils/time';
import StatusBadge from './StatusBadge.vue';
import ExecutionCompatibility from './ExecutionCompatibility.vue';
import Button from './ui/Button.vue';
import Select from './ui/Select.vue';
const props=defineProps<{task:ScanTask;attempts:ScanAttempt[];selectedAttempt:string;overview:QueueOverview|null}>();
const emit=defineEmits<{select:[value:string]}>();
const currentQueue=computed(()=>props.overview?.tasks.find(t=>t.taskId===props.task.id));
const selected=computed(()=>props.attempts.find(a=>a.attempt===Number(props.selectedAttempt)));
const fault=computed(()=>failureHelp(selected.value?.errorCode??props.task.errorCode,selected.value?.status??props.task.status));
const rawError=computed(()=>selected.value?.errorMessage??props.task.errorMessage);
const result=shallowRef<AttemptResult|null>(null),baseline=shallowRef<AttemptResult|null>(null),baselineId=ref('');
const busy=ref(false),error=ref('');let epoch=0;
const baselineOptions=computed(()=>[{value:'',label:'选择对比执行'},...props.attempts.filter(a=>a.resultAvailable&&String(a.attempt)!==props.selectedAttempt).map(a=>({value:String(a.attempt),label:`第 ${a.attempt} 次 · ${absoluteTime(a.startedAt)}`}))]);
const difference=computed(()=>{
 if(!result.value?.available||!baseline.value?.available)return null;
 const a=new Set(result.value.findings.map(f=>f.fingerprint)),b=new Set(baseline.value.findings.map(f=>f.fingerprint));
 return {added:result.value.findings.filter(f=>!b.has(f.fingerprint)),removed:baseline.value.findings.filter(f=>!a.has(f.fingerprint)),unchanged:[...a].filter(x=>b.has(x)).length};
});
async function loadResult(){const run=++epoch;result.value=null;baseline.value=null;error.value='';busy.value=false;if(!selected.value)return;busy.value=true;try{const [r,b]=await Promise.all([api.attemptResult(props.task.id,selected.value.attempt),baselineId.value?api.attemptResult(props.task.id,Number(baselineId.value)):Promise.resolve(null)]);if(run===epoch){result.value=r;baseline.value=b}}catch(e){if(run===epoch)error.value=(e as Error).message}finally{if(run===epoch)busy.value=false}}
watch(()=>props.selectedAttempt,()=>{baselineId.value='';void loadResult()});watch(baselineId,loadResult);
watch(()=>selected.value?.resultAvailable,(ready,was)=>{if(ready&&!was)void loadResult()});
function choose(a:ScanAttempt){emit('select',String(a.attempt))}
function download(){if(!result.value||!selected.value)return;const blob=new Blob([JSON.stringify({taskId:props.task.id,attempt:selected.value,...result.value},null,2)],{type:'application/json'});const url=URL.createObjectURL(blob);const link=document.createElement('a');link.href=url;link.download=`scan-${props.task.id}-attempt-${selected.value.attempt}.json`;link.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}
</script>
<template>
 <section class="execution-record" aria-label="调度与执行记录">
  <div v-if="currentQueue" class="execution-meta">
   <template v-if="task.status==='pending'"><strong>{{queueReasons[currentQueue.reason]??'等待调度'}}</strong><span>入队第 {{currentQueue.position}} 位</span><span>已等待 {{elapsed(currentQueue.queuedAt,overview?.serverTime)}}</span></template>
   <template v-else><span>执行机 <strong>{{currentQueue.agentId||'尚未分配'}}</strong></span><span>排队 {{elapsed(currentQueue.queuedAt,currentQueue.startedAt||currentQueue.finishedAt||overview?.serverTime)}}</span><span>执行 {{elapsed(currentQueue.startedAt,currentQueue.finishedAt||overview?.serverTime)}}</span><span v-if="currentQueue.reason">{{queueReasons[currentQueue.reason]}}</span></template>
   <span v-if="currentQueue.automaticRetries">租约过期后自动重新执行 {{currentQueue.automaticRetries}} 次<span v-if="overview?.maxRecoveries!==undefined"> / 上限 {{overview.maxRecoveries}} 次</span></span>
  </div>
  <ExecutionCompatibility v-if="task.status==='pending' && currentQueue?.compatibility" :compatibility="currentQueue.compatibility" :agents="overview?.agents??[]" />
  <p v-if="task.status==='pending' && currentQueue && !currentQueue.compatibility" class="record-note" role="status">Server 未上报兼容诊断；任务状态与历史报告仍可读取。若任务持续等待，请更新 Server 和配套执行节点。</p>
  <p v-if="task.status==='pending'" class="record-note">按本次入队时间排序；受模型或项目配额限制的任务会暂时跳过。位置不代表预计完成时间。</p>
  <div v-if="fault" class="failure-note" role="status"><strong>{{fault.title}}</strong><p>{{fault.action}}</p><details v-if="rawError"><summary>原始错误</summary><pre>{{rawError}}</pre></details></div>
  <details v-if="attempts.length" class="history-table-wrap">
   <summary>执行历史 <span>{{attempts.length}} 次</span></summary>
   <div class="table-scroll"><table><thead><tr><th>执行</th><th>状态</th><th>开始时间</th><th>执行机</th><th>排队 / 执行耗时</th><th>检出</th><th><span class="sr-only">操作</span></th></tr></thead><tbody><tr v-for="a in attempts" :key="a.attempt" :class="{chosen:selected?.attempt===a.attempt}"><td>第 {{a.attempt}} 次<span v-if="a.resultRecovery"> · 结果恢复入库</span></td><td><StatusBadge :status="a.status" /></td><td>{{absoluteTime(a.startedAt)}}</td><td>{{a.agentId}}</td><td>{{elapsed(a.queuedAt,a.startedAt)}} / {{elapsed(a.startedAt,a.finishedAt||overview?.serverTime)}}</td><td>{{a.resultAvailable?a.findingCount:'—'}}</td><td><Button size="sm" variant="ghost" @click="choose(a)">查看结果与日志</Button></td></tr></tbody></table></div>
   <p class="record-note">每次重新执行保留独立记录，不代表 SDK 断点续扫。旧版缺失的排队时间与结果以「—」表示，不作推算。</p>
  </details>
  <section v-if="selected" class="attempt-result" aria-label="所选执行结果">
   <div class="result-heading"><h2>第 {{selected.attempt}} 次执行</h2><StatusBadge :status="selected.status" /><Button variant="ghost" size="sm" @click="emit('select','current')">返回当前任务</Button><Button v-if="result?.available" variant="outline" size="sm" @click="download">下载本次结果</Button></div>
   <p class="record-note">以下为该次执行的归档结果；活动流也已切换到该次执行。页面下方漏洞工作区仍为当前任务结果。</p>
   <p v-if="busy" role="status">正在读取结果…</p><div v-if="error" role="alert" class="request-error">{{error}}<Button variant="ghost" size="sm" @click="loadResult">重新加载</Button></div>
   <template v-if="result">
    <p v-if="!result.available" class="record-note">{{['claimed','scanning'].includes(selected.status)?'执行尚未结束，结果暂未归档。可在活动流查看实时进展。':'该次执行没有可用的归档结果，请查看日志。旧版已覆盖的结果无法还原。'}}</p>
    <template v-else>
     <div class="comparison-select"><label for="attempt-baseline">对比基准</label><Select id="attempt-baseline" v-model="baselineId" :options="baselineOptions" aria-label="选择对比基准执行" /></div>
     <div v-if="difference" class="difference"><p>相比第 {{baselineId}} 次：新增 {{difference.added.length}} · 本次未检出 {{difference.removed.length}} · 相同 {{difference.unchanged}}</p><p class="record-note">按漏洞指纹对比。「本次未检出」不代表已经修复，尤其在部分完成或扫描失败时。</p><ul v-if="difference.removed.length"><li v-for="f in difference.removed" :key="f.fingerprint">本次未检出 · {{f.title}} · {{f.filePath}}</li></ul></div>
     <p v-if="!result.findings.length" class="record-note">该次执行返回 0 条漏洞结果，请结合执行状态判断扫描是否完整。</p>
     <details v-for="f in result.findings" :key="f.fingerprint" class="archived-finding"><summary><span>{{f.severity}}</span><strong>{{f.title}}</strong><span>{{f.filePath}}:{{f.startLine??'—'}}</span></summary><p>{{f.description}}</p><h3 v-if="f.evidence">证据</h3><pre v-if="f.evidence">{{f.evidence}}</pre><h3 v-if="f.remediation">修复建议</h3><p v-if="f.remediation">{{f.remediation}}</p><details><summary>原始结果</summary><pre>{{JSON.stringify(f,null,2)}}</pre></details></details>
    </template>
   </template>
  </section>
 </section>
</template>
<style scoped>
.execution-record{min-width:0;max-width:100%;margin-bottom:var(--space-5);font-size:var(--font-size-xs);color:var(--text-secondary)}.execution-meta{display:flex;gap:var(--space-4);flex-wrap:wrap;padding:var(--space-3) 0;border-bottom:1px solid var(--border-default)}strong{color:var(--text-primary);font-weight:500}.record-note{line-height:var(--line-height-body);margin:var(--space-3) 0;color:var(--text-secondary)}.failure-note{margin-top:var(--space-3);padding:var(--space-3) var(--space-4);background:var(--surface-secondary);border:1px solid var(--border-default);border-radius:var(--radius-md)}.failure-note>strong{color:var(--state-warning)}.failure-note p{margin:var(--space-2) 0;line-height:var(--line-height-body)}summary{cursor:pointer;line-height:var(--line-height-body)}.history-table-wrap{margin-top:var(--space-4)}.history-table-wrap>summary{font-weight:500;color:var(--text-primary)}.history-table-wrap>summary span{margin-left:var(--space-2);color:var(--text-secondary)}.table-scroll{position:relative;max-width:100%;min-width:0;overflow-x:auto;margin-top:var(--space-3)}table{width:100%;border-collapse:collapse;text-align:left;font-variant-numeric:tabular-nums}th{font-weight:500;background:var(--surface-secondary)}th,td{padding:var(--space-2) var(--space-3);border-bottom:1px solid var(--border-default);white-space:nowrap}tr.chosen{background:var(--surface-selected)}.attempt-result{margin-top:var(--space-4);border:1px solid var(--border-default);border-radius:var(--radius-md);padding:var(--space-4);background:var(--surface-primary)}.result-heading{display:flex;align-items:center;gap:var(--space-3);flex-wrap:wrap}h2{font-size:var(--font-size-sm);margin:0;color:var(--text-primary)}.comparison-select{display:flex;align-items:center;gap:var(--space-3);flex-wrap:wrap;margin:var(--space-3) 0}.comparison-select>:last-child{max-width:320px}.difference{border-block:1px solid var(--border-default);margin-bottom:var(--space-3)}ul{padding-left:var(--space-5)}li{margin:var(--space-2) 0;overflow-wrap:anywhere}.archived-finding{border-top:1px solid var(--border-default);padding:var(--space-3) 0}.archived-finding>summary span{margin-right:var(--space-3);overflow-wrap:anywhere}.archived-finding>summary strong{margin-right:var(--space-3)}.archived-finding p{white-space:pre-wrap;overflow-wrap:anywhere;line-height:var(--line-height-body)}h3{font-size:var(--font-size-xs);font-weight:600;color:var(--text-primary)}pre{white-space:pre-wrap;overflow-wrap:anywhere;max-height:360px;overflow:auto;padding:var(--space-3);background:var(--surface-secondary);border:1px solid var(--border-default);font-size:var(--font-size-xs)}
</style>
