<script setup lang="ts">
import {computed,onUnmounted,ref,shallowRef,watch} from 'vue';
import {api} from '../api';
import type {ScanTask} from '../types';
import type {TaskBudgetSnapshot} from '../contracts';
import {checkedComponentExecutions,componentRows,componentStatus,componentMoney,type ComponentExecutionsView} from '../utils/component-scans';
import Button from './ui/Button.vue';
const props=defineProps<{task:ScanTask;attempt?:number;canOperate:boolean;budget:TaskBudgetSnapshot|null}>();
const emit=defineEmits<{changed:[];selectAttempt:[number]}>();
const view=shallowRef<ComponentExecutionsView|null>(null),error=ref(''),busy=ref(false),action=ref(false),selected=ref(0),chosen=ref<number[]>([]),ack=ref(false),artifactError=ref('');let generation=0;
const active=computed(()=>['pending','claimed','scanning','cancel_requested'].includes(props.task.status));
const rows=computed(()=>view.value?componentRows(view.value,props.attempt):[]);
const current=computed(()=>view.value?.attempts.find(a=>a.request.execution.attempt===(props.attempt??props.task.attempt)));
const inspected=computed(()=>rows.value.find(r=>r.index===selected.value));
const component=computed(()=>view.value?.request.planVersion.document.components[selected.value]);
const eligible=computed(()=>rows.value.filter(r=>r.receipt?.status!=='completed').map(r=>r.index));
const retryReady=computed(()=>!active.value&&props.attempt===undefined&&props.budget?.retryAllowed===true&&chosen.value.length>0&&ack.value);
function label(index:number){const row=rows.value.find(r=>r.index===index);if(!active.value&&['pending','running'].includes(row?.receipt?.status??''))return '中断未完成';return componentStatus(row?.receipt?.status)}
async function load(){const g=++generation;busy.value=true;error.value='';try{const next=checkedComponentExecutions(await api.componentExecutions(props.task.id),props.task.id);if(g===generation)view.value=next}catch(e){if(g===generation)error.value='组件执行读取失败：'+(e as Error).message}finally{if(g===generation)busy.value=false}}
watch(()=>[props.task.id,props.task.attempt,props.task.status,props.task.progress?.revision],()=>void load(),{immediate:true});
watch(()=>props.attempt,()=>{chosen.value=[];ack.value=false});
onUnmounted(()=>generation++);
function inspect(index:number){selected.value=index;artifactError.value=''}
function keyboard(event:KeyboardEvent,index:number){if(!['ArrowDown','ArrowUp','j','k'].includes(event.key))return;event.preventDefault();const i=Math.max(0,Math.min(rows.value.length-1,index+(['ArrowDown','j'].includes(event.key)?1:-1)));inspect(i);document.getElementById(`component-row-${props.task.id}-${i}`)?.focus()}
async function retry(){if(!props.canOperate||!retryReady.value||action.value)return;action.value=true;error.value='';try{await api.retryComponents(props.task.id,{attempt:props.task.attempt,expectedStatus:props.task.status,indices:[...chosen.value].sort((a,b)=>a-b),acknowledgeReexecution:ack.value});chosen.value=[];ack.value=false;emit('changed');await load()}catch(e){error.value='组件重新执行未完成：'+(e as Error).message}finally{action.value=false}}
async function artifact(){const row=inspected.value;if(!row?.receipt?.resultArtifact||!row.attempt)return;artifactError.value='';try{const blob=await api.resultArtifact(props.task.id,row.attempt,row.receipt.resultArtifact.relativePath);const url=URL.createObjectURL(blob),link=document.createElement('a');link.href=url;link.download=`component-${row.index+1}-attempt-${row.attempt}.json`;link.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}catch(e){artifactError.value='组件证据下载失败：'+(e as Error).message}}
</script>
<template>
 <section class="component-executions" aria-label="组件执行">
  <header><h2>组件执行</h2><span v-if="view">冻结计划 <RouterLink :to="'/component-plans/'+view.request.planVersion.planId">#{{view.request.planVersion.planId}} / v{{view.request.planVersion.version}}</RouterLink> · 并发上限 {{current?.request.workers??view.request.workers}}</span><Button variant="ghost" size="sm" :disabled="busy" @click="load">{{busy?'读取中…':'刷新组件'}}</Button></header>
  <p v-if="error" role="alert" class="request-error">{{error}}</p>
  <template v-if="view">
   <p class="note">{{attempt===undefined?'父任务综合各组件最近执行；已完成结果保留在原执行中。':'当前选定执行的组件；未选择的组件显示为尚未执行。'}} 漏洞列表和报告按选定执行展示，不能据某一轮完成推定整份计划已完成。</p>
   <dl v-if="current?.snapshot" class="budget-line"><div><dt>本轮阶段</dt><dd>{{({preparing:'准备源码',scanning:'组件扫描',matching:'跨组件匹配',finished:'本轮结束'})[current.snapshot.phase]}}</dd></div><div><dt>预留中 · USD</dt><dd>{{componentMoney(current.snapshot.reservedMicros)}}</dd></div><div><dt>已知费用下界 · USD</dt><dd>{{componentMoney(current.snapshot.observedMicros)}}</dd></div><div><dt>匹配费用</dt><dd>{{current.snapshot.matching.costState==='unknown'?'未知':'尚未调用 · $0'}}</dd></div></dl>
   <p v-if="current?.snapshot" class="note" role="status">{{current.snapshot.matching.reason}}</p>
   <div class="component-panes">
    <div class="component-grid" tabindex="0" aria-label="组件执行列表，可横向滚动">
     <table><thead><tr><th v-if="canOperate&&!active&&attempt===undefined">重试</th><th>组件</th><th>状态 / 阶段</th><th>预留 / 已知费用 · USD</th><th>执行 / 发现</th></tr></thead><tbody>
      <tr v-for="row in rows" :key="row.index" :class="{selected:selected===row.index}"><td v-if="canOperate&&!active&&attempt===undefined"><input v-if="eligible.includes(row.index)" v-model="chosen" type="checkbox" :value="row.index" :aria-label="'重试组件 '+view.request.planVersion.document.components[row.index]?.name" :disabled="action"/><span v-else>—</span></td>
       <th scope="row"><button :id="`component-row-${task.id}-${row.index}`" type="button" :aria-pressed="selected===row.index" @click="inspect(row.index)" @keydown="keyboard($event,row.index)">{{view.request.planVersion.document.components[row.index]?.name}}</button></th>
       <td>{{label(row.index)}}<small v-if="row.receipt?.phase">{{row.receipt.phase}}</small></td><td class="mono">{{componentMoney(row.receipt?.allocatedMicros)}} / {{componentMoney(row.receipt?.observedMicros)}}<small>{{row.receipt?.finalMicros==null?'终值未知':'终值 '+componentMoney(row.receipt.finalMicros)}}</small></td><td>{{row.attempt?'第 '+row.attempt+' 次':'未领取'}} / {{row.receipt?.findingCount??'未知'}}</td></tr>
     </tbody></table>
    </div>
    <aside v-if="component" class="component-inspector" aria-label="组件执行详情">
     <h3>{{component.name}}</h3><p>{{label(selected)}}<template v-if="inspected?.attempt"> · 第 {{inspected.attempt}} 次执行</template></p>
     <dl><div><dt>冻结范围</dt><dd><code v-for="p in component.paths" :key="p">{{p}}</code></dd></div><div><dt>源码 SHA-256</dt><dd class="mono">{{view.request.planVersion.source.contentHash}}</dd></div><div><dt>计划 SHA-256</dt><dd class="mono">{{view.request.planVersion.sha256}}</dd></div><div v-if="view.request.planVersion.source.headCommit"><dt>Git HEAD</dt><dd class="mono">{{view.request.planVersion.source.headCommit}}</dd></div><div><dt>SDK Scan ID</dt><dd class="mono">{{inspected?.receipt?.scanId??'未返回'}}</dd></div><div><dt>当前活动</dt><dd>{{inspected?.receipt?.activity??'暂无已报告活动'}}</dd></div></dl>
     <p v-if="inspected?.receipt?.error" role="status" class="request-error">{{inspected.receipt.error.code}}：{{inspected.receipt.error.message}}</p>
     <div class="component-actions"><Button v-if="inspected?.attempt" variant="outline" size="sm" @click="emit('selectAttempt',inspected.attempt)">查看此执行的漏洞与证据</Button><Button v-if="inspected?.receipt?.resultArtifact" variant="outline" size="sm" @click="artifact">下载组件原始结果</Button></div>
     <p v-if="artifactError" role="alert" class="request-error">{{artifactError}}</p>
    </aside>
   </div>
   <section v-if="canOperate&&!active&&attempt===undefined" class="component-retry" aria-label="重新执行失败组件"><h3>重新执行失败或未完成组件</h3><p class="note">已完成组件不会重新调用模型；保留已有结果、每轮费用和匹配依据。新一轮仅匹配本次选中的组件，不代表全历史漏洞已去重。</p><p v-if="!eligible.length" class="note">没有待重试组件。匹配未完成不会把已完成组件变成失败；可在归档中检查匹配错误和独立原始发现。</p><p v-if="!budget?.retryAllowed" class="note">{{budget?.state==='unknown'?'费用未知：请先在上方任务费用中核对；不能按零费用重新执行。':'累计预算不足或费用状态未就绪，请先处理预算。'}}</p><label class="component-ack"><input v-model="ack" type="checkbox" :disabled="action"/>我确认重新调用模型，并知悉匹配可能产生新的未知费用</label><Button :disabled="!retryReady||action" :loading="action" @click="retry">重新执行选中组件（{{chosen.length}}）</Button></section>
  </template><p v-else-if="busy" role="status">正在读取父任务和组件执行…</p>
 </section>
</template>
<style scoped>
.component-executions{border-block:1px solid var(--border-default);padding-block:var(--space-3);margin-block:var(--space-3);min-width:0}header{display:flex;flex-wrap:wrap;align-items:center;gap:var(--space-3)}h2,h3{margin:0;font-size:var(--font-size-sm);font-weight:var(--font-weight-semibold)}header span,.note,dt,small{color:var(--text-secondary)}p,span,label,dl{font-size:var(--font-size-xs);line-height:var(--line-height-body)}p{margin:var(--space-2) 0;overflow-wrap:anywhere}.budget-line{display:flex;flex-wrap:wrap;gap:var(--space-3);margin-block:var(--space-3)}dd{margin:0;overflow-wrap:anywhere}.component-panes{display:grid;grid-template-columns:minmax(0,1fr) minmax(200px,280px);border-top:1px solid var(--border-default)}.component-grid{overflow:auto;min-width:0;max-height:calc(var(--space-8)*13)}table{border-collapse:collapse;width:100%;font-size:var(--font-size-xs)}th,td{text-align:left;padding:var(--space-2);border-bottom:1px solid var(--border-subtle);vertical-align:top}td:last-child,thead th:last-child{white-space:nowrap}thead th{font-weight:var(--font-weight-medium);color:var(--text-secondary)}tbody th{font-weight:var(--font-weight-normal);min-width:100px}tbody th button{text-align:left;overflow-wrap:anywhere}tr.selected{background:var(--surface-selected)}tbody tr:hover{background:var(--surface-hover)}small{display:block;min-width:80px}.component-inspector{min-width:0;border-left:1px solid var(--border-default);padding:var(--space-3)}.component-inspector dl{margin:var(--space-3) 0}.component-inspector dl>div{margin-block:var(--space-2)}code{display:block;font-family:var(--font-mono);overflow-wrap:anywhere}.component-actions{display:flex;flex-wrap:wrap;gap:var(--space-2)}.component-retry{border-top:1px solid var(--border-default);margin-top:var(--space-3);padding-top:var(--space-3)}.component-ack{display:flex;align-items:flex-start;gap:var(--space-2);margin-block:var(--space-3)}input{accent-color:var(--accent-primary)}button:focus-visible,input:focus-visible,.component-grid:focus-visible{outline:2px solid var(--border-focus);outline-offset:2px}
@media(max-width:1000px){.component-panes{grid-template-columns:minmax(0,1fr)}.component-inspector{border-left:0;border-top:1px solid var(--border-default);padding-inline:0}.component-inspector dl{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:var(--space-3)}}@media(max-width:600px){.component-inspector dl{grid-template-columns:minmax(0,1fr)}}
</style>
