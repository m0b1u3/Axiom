<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, shallowRef, watch } from 'vue';
import { api } from '../api';
import type { Progress, ScanLogEvent } from '../types';
import { activityEntry, isImportant, localLogTime, mergeLogEvents, phaseLabel, progressEntries, fileProgressLabel, phaseProgressLabel } from '../utils/scan-log';
import type {ContractError} from '../contracts';
import {sdkLifecycleView,authenticationMethods,trustedAccessStates} from '../utils/sdk-lifecycle';
import {progressSources} from '../utils/scan-progress';
import {workerLabel,workerStates} from '../utils/scan-log';
import Button from './ui/Button.vue';
import Input from './ui/Input.vue';
import SDKDiagnostics from './SDKDiagnostics.vue';
const props=defineProps<{scanId:number;attempt?:number;running:boolean;status:string;progress:Progress|null;operationKind?:string;diagnostic?:ContractError}>();
const events=shallowRef<ScanLogEvent[]>([]), total=ref(0),error=ref(''),loading=ref(false),follow=ref(true),query=ref(''),filter=ref('important'),box=ref<HTMLElement|null>(null);
const invalidRecords=ref(0),journalFormat=ref(''),progressRawOpen=ref(false),rawOpen=ref<Record<string,boolean>>({});
function eventKey(e:ScanLogEvent){return e.event?`${e.event.execution.taskId}:${e.event.execution.attempt}:${e.event.sequence}`:`legacy:${e.n}`;}
function setRaw(e:ScanLogEvent,event:Event){rawOpen.value[eventKey(e)]=(event.currentTarget as HTMLDetailsElement).open;}
let cursor=0,timer:ReturnType<typeof setTimeout>|undefined,disposed=false,queued=false;
const entries=computed(()=>progressEntries(events.value,props.progress?.progressReportedAt).map(e=>props.operationKind?{...e,summary:e.summary.replaceAll('扫描完成','操作完成').replaceAll('扫描失败','操作失败'),label:e.label==='扫描器结束'?'操作结束':e.label}:e));
const recoveries=computed(()=>entries.value.filter(e=>e.recovered).length);
const failures=computed(()=>entries.value.filter(e=>e.failed||e.warning).length);
const visible=computed(()=>entries.value.filter(e=>(filter.value==='all'||filter.value==='issues'?(filter.value==='all'||e.failed||e.warning||e.recovered):isImportant(e))&&e.message.toLowerCase().includes(query.value.toLowerCase().trim())));
const options=[{value:'important',label:'重要进展'},{value:'all',label:'全部记录'},{value:'issues',label:'异常'}];
const lifecycle=computed(()=>sdkLifecycleView(props.progress?.snapshot?.extensions?.sdkLifecycle));
const current=computed(()=>props.progress?.snapshot?.activity?{label:props.progress.snapshot.activity.status==='failed'?'最近调用失败':props.progress.snapshot.activity.kind==='reasoning'?'最近推理记录':'最近上报动作',summary:props.progress.snapshot.activity.description}:props.progress?.activity?activityEntry({n:0,timestamp:'',level:'info',kind:'activity',message:props.progress.activity}):null);
async function scrollLatest(){await nextTick();if(follow.value&&box.value)box.value.scrollTop=box.value.scrollHeight;}
function onScroll(){const el=box.value;if(el)follow.value=el.scrollHeight-el.scrollTop-el.clientHeight<32;}
function resume(){follow.value=true;void scrollLatest();}
async function pull(){
 if(disposed)return;if(loading.value){queued=true;return;}
 clearTimeout(timer);loading.value=true;
 try{let res=await api.scanLogs(props.scanId,cursor,props.attempt);if(disposed)return;
 if(res.total!==undefined&&res.total<cursor){cursor=0;events.value=[];res=await api.scanLogs(props.scanId,0,props.attempt);if(disposed)return;}
 events.value=mergeLogEvents(events.value,res.events);if (res.hasMore) queued=true;
    cursor=res.nextAfter;total.value=res.recordCount??res.total??cursor;invalidRecords.value=res.invalidRecords??0;journalFormat.value=res.format??'legacy_jsonl';error.value='';void scrollLatest();
 }catch(e){if(!disposed)error.value=(e as Error).message;}finally{loading.value=false;if(!disposed){if(queued){queued=false;void pull();}else if(props.running)timer=setTimeout(pull,3000);}}
}
function download(){const data=events.value.map(e=>JSON.stringify(e.event??e)).join('\n');const url=URL.createObjectURL(new Blob([data],{type:'application/x-ndjson'}));const a=document.createElement('a');a.href=url;a.download=`scan-${props.scanId}-attempt-${props.attempt??"current"}-loaded-logs.jsonl`;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);}
watch(()=>props.running,()=>void pull());watch([filter,query],()=>void scrollLatest());
onMounted(()=>void pull());onUnmounted(()=>{disposed=true;clearTimeout(timer);});
</script>
<template>
<section class="activity-panel" aria-label="扫描活动与日志">
 <header class="activity-header"><div><strong>{{operationKind?'SDK 操作日志':running?'扫描活动':'扫描日志'}}</strong><span class="phase">{{running?phaseLabel(progress?.phase||'')+(progress?.rawPhase?` · ${progress.rawPhase}`:''):operationKind&&status==='completed'?'操作完成':phaseLabel(status)}}</span><span v-if="!operationKind&&(progress?.snapshot || progress?.filesTotal)" class="subtle">{{fileProgressLabel(progress)}}</span></div><span v-if="failures||recoveries" class="subtle"><span v-if="failures" class="issue-count">{{failures}} 条待关注异常</span><span v-if="recoveries"> · {{recoveries}} 条已恢复</span></span></header>
 <p v-if="phaseProgressLabel(progress)" class="current-action">阶段进度：{{phaseProgressLabel(progress)}}</p>
 <dl v-if="progress?.deep || progress?.worker || progress?.reconnect" class="execution-observations">
  <div v-if="progress.deep"><dt>独立审查</dt><dd>已完成 {{progress.deep.completed??'—'}} 次 · {{running?'活跃':'最后观测活跃'}} {{progress.deep.active??'—'}} 个 · 最多 {{progress.deep.maximum??'—'}} 次<span>发现次数不代表文件覆盖率</span></dd></div>
  <div v-if="progress.worker?.kind==='preflight'"><dt>Worker 预检</dt><dd>{{({available:'委派可用',unavailable:'委派不可用',unknown:'委派状态未知'} as Record<string,string>)[progress.worker.delegation??'unknown']??'状态未知'}}<template v-if="progress.worker.configuredSlots!=null"> · {{progress.worker.configuredSlots}} 个配置槽位</template></dd></div>
  <div v-if="progress.worker?.kind==='dispatch'"><dt>最近调度</dt><dd>{{phaseLabel(progress.worker.phase??'')}} · 已启动 {{progress.worker.started??'—'}} / 计划 {{progress.worker.planned??'—'}}</dd></div>
  <div v-if="progress.reconnect"><dt>最近连接重试</dt><dd>{{({rate_limit:'供应商限流',network:'网络中断',authentication:'认证失败',authorization:'授权失败'} as Record<string,string>)[progress.reconnect.reason]??'原因未知'}} · 第 {{progress.reconnect.attempt}} / {{progress.reconnect.maximum}} 次 · {{localLogTime(progress.reconnect.updatedAt)}}<template v-if="progress.reconnect.retryAfterSeconds!=null"> · 建议等待 {{progress.reconnect.retryAfterSeconds}} 秒</template><span>历史事件，不表示当前仍在重连</span></dd></div>
 </dl>
 <dl v-if="progress?.snapshot" class="execution-observations">
  <div><dt>观察来源</dt><dd>{{progressSources[progress.snapshot.source]}} · {{localLogTime(progress.snapshot.observedAt)}} · 修订 {{progress.snapshot.revision}}<span v-if="!running">最后归档快照</span><span v-else>观察时间不代表当前仍在执行该动作</span></dd></div>
  <div v-if="!operationKind"><dt>估算费用</dt><dd>{{progress.snapshot.cost?.estimatedUsd==null?'尚未上报':progress.snapshot.cost.estimatedUsd>0&&progress.snapshot.cost.estimatedUsd<0.0001?'< $0.0001':`$${progress.snapshot.cost.estimatedUsd.toFixed(4)}`}} · 输入 {{progress.snapshot.cost?.inputTokens??'—'}} / 输出 {{progress.snapshot.cost?.outputTokens??'—'}} Token<span v-if="progress.snapshot.cost?.reportedModel">{{progress.snapshot.cost.reportedModel}}</span><span>以 SDK 上报的累计估算为准</span></dd></div>
 </dl>
 <dl v-if="lifecycle.value" class="execution-observations" aria-label="SDK 生命周期">
  <div><dt>认证来源</dt><dd>{{lifecycle.value.authentication?authenticationMethods[lifecycle.value.authentication.method]:'尚未上报'}}<span v-if="lifecycle.value.authentication">来源已识别，访问权限尚未验证</span></dd></div>
  <div><dt>Trusted Access</dt><dd>{{lifecycle.value.trustedAccess===null?'尚未上报':trustedAccessStates[lifecycle.value.trustedAccess]}}<span>与认证来源分别记录</span></dd></div>
  <div><dt>扫描生命周期</dt><dd>{{lifecycle.value.scanStarted?'已收到开始回调':'尚未收到开始回调'}} · {{lifecycle.value.outputReady?'产物目录已就绪':'产物目录尚未上报'}}<span v-if="lifecycle.value.outputArchived">原输出已归档</span></dd></div>
  <div v-if="lifecycle.value.budgetApproaching"><dt>预算提醒</dt><dd>接近本次上限 ${{lifecycle.value.budgetApproaching.maxCostUsd}} · 保持已批准额度<span>未自动增加预算 · {{localLogTime(lifecycle.value.budgetApproaching.observedAt)}}</span></dd></div>
 </dl>
 <SDKDiagnostics v-if="running&&progress?.snapshot?.extensions?.sdkDiagnostics" :value="progress.snapshot.extensions.sdkDiagnostics" :execution="progress.snapshot.execution" class="live-sdk-diagnostics"/>
 <div v-if="lifecycle.invalid" class="log-error" role="alert">SDK 生命周期记录的格式不受支持；其他进度与原始快照仍可查看。</div>
 <div v-if="diagnostic" class="log-error" role="alert">进度接收异常 · {{diagnostic.code}} · {{diagnostic.origin}}：{{diagnostic.message}}</div>
 <div v-if="progress?.snapshot?.error" class="log-error">最近扫描错误 · {{progress.snapshot.error.code}} · {{progress.snapshot.error.origin}}：{{progress.snapshot.error.message}}<span class="subtle"> · 执行结论以任务状态为准</span></div>
 <section v-if="progress?.snapshot?.workers.length" class="worker-observations" aria-label="Worker 活动">
  <h3>Worker 与线程活动 <span>最近 {{progress.snapshot.workers.length}} 个线程<template v-if="progress.snapshot.workersTruncated"> · 完整历史见结构化事件</template></span></h3>
  <ul tabindex="0" aria-label="最近线程状态，可滚动查看"><li v-for="worker in progress.snapshot.workers" :key="worker.worker.threadId??worker.worker.index">
   <div class="worker-heading"><strong>{{workerLabel(worker.worker)}}</strong><span :class="{ 'worker-failed':worker.status==='failed' }">{{workerStates[worker.status]}}</span><time :datetime="worker.observedAt">{{localLogTime(worker.observedAt)}}</time></div>
   <p v-if="worker.activity">{{worker.activity.status==='failed'?'调用失败':worker.activity.status==='running'?'上报时运行中':'最近活动'}} · {{worker.activity.description}}</p>
   <p v-if="worker.error" class="worker-failed">{{worker.error.code}} · {{worker.error.message}}</p>
   <p v-if="worker.worker.parentThreadId" class="subtle">父线程 {{worker.worker.parentThreadId}}</p>
  </li></ul>
 </section>
 <p v-if="running&&current" class="current-action"><span>{{current.label}}：</span>{{current.summary}}<span v-if="progress?.snapshot?.activity?.truncated" class="subtle"> · 完整记录见事件</span></p>
 <details v-if="progress?.snapshot" class="progress-raw" @toggle="progressRawOpen=($event.currentTarget as HTMLDetailsElement).open"><summary>查看原始进度快照</summary><pre v-if="progressRawOpen">{{JSON.stringify(progress.snapshot,null,2)}}</pre></details>
 <div class="activity-tools"><div class="log-tabs" aria-label="日志类型"><button v-for="option in options" :key="option.value" :aria-pressed="filter===option.value" @click="filter=option.value">{{option.label}}</button></div><Input v-model="query" placeholder="搜索日志内容…" aria-label="搜索日志" class="log-search"/><Button variant="ghost" size="sm" :disabled="!events.length" @click="download">下载已加载日志</Button></div>
 <div v-if="error" class="log-error" role="alert">日志读取失败：{{error}} <Button variant="outline" size="sm" @click="pull">重试</Button></div>
 <div v-if="invalidRecords" class="log-error" role="alert">{{invalidRecords}} 条事件未通过大小、合同或执行身份校验，已跳过。有效记录仍可查看。</div>
 <div ref="box" class="activity-list" @scroll="onScroll" tabindex="0" aria-label="日志记录，可展开查看详情" :aria-busy="loading">
 <details v-for="e in visible" :key="e.event?`${e.event.execution.taskId}:${e.event.execution.attempt}:${e.event.sequence}`:e.n" class="event" :class="{failed:e.failed,warning:e.warning}"><summary><time :datetime="e.timestamp" :title="e.timestamp">{{localLogTime(e.timestamp)}}</time><span class="event-kind">{{e.label}}</span><span class="event-summary">{{e.summary}}</span></summary><div class="event-detail"><div class="event-meta">记录 #{{e.n}} · {{e.kind}} · {{e.level}} · {{e.timestamp}}</div><pre>{{e.message}}</pre><details v-if="e.event" class="event-raw" @toggle="setRaw(e,$event)"><summary>结构化原始事件 · 执行 {{e.event.execution.attempt}}</summary><pre v-if="rawOpen[eventKey(e)]">{{JSON.stringify(e.event,null,2)}}</pre></details><p v-if="e.message.endsWith('…')" class="subtle">此记录在日志源头已截断，无法恢复省略部分。</p></div></details>
 <div v-if="!visible.length" class="log-empty">{{loading?'正在读取日志…':events.length?'没有匹配的记录，试试其他分类或搜索词。':running?'等待扫描器输出…':'没有可用日志。'}}</div>
 </div>
 <footer class="activity-footer"><span>显示 {{visible.length}} / 已加载 {{events.length}} · 源日志 {{total}} 条 · 本地时间<span v-if="journalFormat==='legacy_jsonl'"> · 旧版文本日志</span><span v-if="total>events.length"> · 仅保留最近 1500 条</span></span><button :aria-pressed="follow" @click="follow?follow=false:resume()">{{follow?'跟随最新':'已暂停跟随 · 回到最新'}}</button></footer>
</section>
</template>
<style scoped>
.live-sdk-diagnostics{margin-inline:var(--space-4)}
.progress-raw{padding:0 var(--space-4) var(--space-3);font-size:var(--font-size-xs);color:var(--text-secondary)}.progress-raw summary{cursor:pointer}.progress-raw pre{max-height:calc(var(--height-table-row) * 7);overflow:auto}
.worker-observations{padding:0 var(--space-4) var(--space-3)}.worker-observations h3{margin:0 0 var(--space-2);font-size:var(--font-size-xs);font-weight:var(--font-weight-medium)}.worker-observations h3 span{color:var(--text-tertiary);font-weight:var(--font-weight-normal);margin-left:var(--space-3)}.worker-observations ul{list-style:none;margin:0;padding:0;max-height:calc(var(--height-table-row) * 7);overflow:auto}.worker-observations li{border-top:1px solid var(--border-subtle);padding:var(--space-2) 0;font-size:var(--font-size-xs)}.worker-heading{display:flex;align-items:baseline;gap:var(--space-3);flex-wrap:wrap}.worker-heading strong{font-family:var(--font-mono);font-weight:var(--font-weight-medium);overflow-wrap:anywhere}.worker-heading span,.worker-heading time{color:var(--text-tertiary)}.worker-heading time{margin-left:auto}.worker-observations p{margin:var(--space-1) 0 0;white-space:pre-wrap;overflow-wrap:anywhere;color:var(--text-secondary)}.worker-observations .worker-failed{color:var(--state-danger)}.event-raw{margin-top:var(--space-2)}

.execution-observations{margin:0;padding:0 var(--space-4) var(--space-3);display:grid;gap:var(--space-2);font-size:var(--font-size-xs)}.execution-observations>div{display:flex;gap:var(--space-3);flex-wrap:wrap}.execution-observations dt{color:var(--text-tertiary);min-width:80px}.execution-observations dd{margin:0;color:var(--text-secondary);overflow-wrap:anywhere}.execution-observations dd span{margin-left:var(--space-3);color:var(--text-tertiary)}
.activity-panel{border:1px solid var(--border-default);border-radius:var(--radius-sm);background:var(--surface-primary);overflow:hidden;margin-bottom:var(--space-5)}
.activity-header,.activity-tools,.activity-footer{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;padding:14px 16px}.activity-header>div{display:flex;align-items:center;gap:14px;flex-wrap:wrap}.activity-header strong{font-size:13px}.phase{font-size:12px;color:var(--accent-primary)}.subtle,.activity-footer{font-size:11px;color:var(--text-tertiary)}.issue-count{font-size:12px;color:var(--state-warning)}
.current-action{padding:0 16px 12px;margin:0;font-size:12px;color:var(--text-secondary);display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;overflow-wrap:anywhere}.current-action span{font-weight:500}.activity-tools{border-top:1px solid var(--border-subtle);border-bottom:1px solid var(--border-subtle);padding:10px 16px}.log-tabs{display:flex;gap:4px}.log-tabs button{font-size:12px;padding:6px 10px;border-radius:4px;color:var(--text-secondary)}.log-tabs button[aria-pressed=true]{background:var(--surface-selected);color:var(--accent-primary)}.log-search{max-width:240px;margin-left:auto}.activity-list{max-height:340px;min-height:100px;overflow:auto;padding:4px 16px;overscroll-behavior:contain}.event{border-bottom:1px solid var(--border-subtle)}.event:last-child{border:0}.event summary{display:flex;gap:12px;padding:10px 0;cursor:pointer;list-style:none;align-items:baseline}.event summary::-webkit-details-marker{display:none}.event summary::before{content:'›';color:var(--text-tertiary)}.event[open] summary::before{content:'⌄'}.event time{font:11px var(--font-mono);color:var(--text-tertiary);white-space:nowrap}.event-kind{font-size:11px;color:var(--text-tertiary);min-width:48px}.event-summary{font-size:12px;line-height:1.7;color:var(--text-secondary);overflow:hidden;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow-wrap:anywhere}.failed .event-kind{color:var(--state-danger)}.warning .event-kind{color:var(--state-warning)}.event-detail{padding:8px 12px 14px;background:var(--surface-secondary);border-radius:4px;margin-bottom:8px}.event-meta{font-size:10px;color:var(--text-tertiary)}pre{font:11px/1.8 var(--font-mono);white-space:pre-wrap;overflow-wrap:anywhere;margin:8px 0 0}.activity-footer{border-top:1px solid var(--border-default);padding:10px 16px}.activity-footer button{font-size:11px;color:var(--accent-primary)}.log-error{padding:10px 16px;font-size:12px;color:var(--state-danger)}.log-empty{text-align:center;padding:28px 8px;font-size:12px;color:var(--text-tertiary)}@media(max-width:650px){.log-search{max-width:none;width:100%;order:3}.event summary{gap:6px}.event time{font-size:10px}.event-kind{min-width:40px}.activity-tools{gap:8px}.event-summary{-webkit-line-clamp:3}}
</style>
