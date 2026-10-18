<script setup lang="ts">
import {computed,onUnmounted,ref,shallowRef,watch} from 'vue';
import {api,ApiError} from '../api';
import type {TaskBudgetSnapshot,AttemptCost} from '../contracts';
import {useAuth} from '../composables/auth';
import {absoluteTime} from '../utils/time';
import Button from './ui/Button.vue';
import Input from './ui/Input.vue';
import Dialog from './ui/Dialog.vue';
import CodeViewer from './CodeViewer.vue';
const props=defineProps<{scanId:number;status:string;limitsUnsupported?:boolean;progressRevision?:number;attempt:number}>();
const emit=defineEmits<{snapshot:[TaskBudgetSnapshot|null];selectAttempt:[number]}>();
const {canOperate}=useAuth();
const data=shallowRef<TaskBudgetSnapshot|null>(null),loading=ref(false),error=ref(''),unsupported=ref(false),open=ref(false),saving=ref(false),formError=ref('');
const target=shallowRef<AttemptCost|null>(null),value=ref(''),note=ref(''),revision=ref(0);let generation=0;
const rawOpen=ref(false);
const editable=computed(()=>canOperate.value && data.value?.accountingVersion===1 && !['claimed','scanning','cancel_requested'].includes(props.status));
const money=(n:number|null|undefined)=>{if(n==null)return '未知';const v=BigInt(n),fraction=String(v%1000000n).padStart(6,'0').replace(/0+$/,'');return `$${v/1000000n}${fraction?'.'+fraction:''}`;};
const states={legacy:'历史任务 · 原单次预算语义',no_total_limit:'未设置任务总预算',ready:'可分配预算',running:'本次额度已预留',exhausted:'累计预算用尽',unknown:'费用未知 · 累计预算重试受阻'};
const sources={none:'未上报',sdk_progress:'SDK 进度估算',sdk_result:'SDK 最终结果',manual_estimate:'人工估算'};
const reasons:Record<string,string>={final_cost_missing:'最终结果缺少费用',execution_interrupted:'执行失联或异常终止',execution_incomplete:'执行异常，费用仅作为下界',final_below_progress:'最终估算低于进度，按较高金额累计',amount_out_of_range:'费用超出可记账范围',cost_unavailable:'SDK 未提供 USD 估算',operator_reconciled:'已提交人工估算',recovered_final_above_manual:'恢复的 SDK 最终估算更高，累计按较高金额计入，人工依据保留'};
async function load(){const n=++generation;loading.value=true;error.value='';unsupported.value=false;try{const result=await api.taskBudget(props.scanId);if(n===generation){data.value=result;emit('snapshot',result)}}catch(e){if(n===generation){data.value=null;emit('snapshot',null);if(e instanceof ApiError && [404,405].includes(e.status))unsupported.value=true;else error.value=(e as Error).message}}finally{if(n===generation)loading.value=false}}
watch(()=>[props.scanId,props.status,props.attempt,props.progressRevision],load,{immediate:true});onUnmounted(()=>generation++);
function edit(attempt:AttemptCost|null){target.value=attempt;revision.value=data.value!.revision;value.value=attempt?String((attempt.bookedMicros??attempt.grantedMicros??0)/1e6):data.value!.totalLimitMicros==null?'':String(data.value!.totalLimitMicros/1e6);note.value='';formError.value='';open.value=true;}
async function save(){if(saving.value)return;const amount=Number(value.value);if(!String(value.value).trim()||!Number.isFinite(amount)||amount<0||(!target.value&&amount===0)){formError.value='请输入有效的 USD 估算金额。';return}if(target.value&&[...note.value.trim()].length<4){formError.value='请填写费用核对依据，至少 4 个字符。';return}saving.value=true;formError.value='';try{const next=target.value?await api.reconcileTaskCost(props.scanId,target.value.execution.attempt,amount,note.value,revision.value):await api.updateTaskBudget(props.scanId,amount,revision.value);data.value=next;emit('snapshot',next);open.value=false;}catch(e){formError.value=(e as Error).message}finally{saving.value=false}}
</script>
<template>
 <section class="execution-budget" aria-label="任务预算与费用">
  <header><h2>任务预算与费用</h2><span v-if="data">{{states[data.state]}}</span><div class="budget-actions"><Button variant="ghost" size="sm" :loading="loading" @click="load">刷新费用</Button><Button v-if="editable&&!limitsUnsupported" variant="outline" size="sm" @click="edit(null)">调整总预算</Button></div></header>
  <p v-if="loading&&!data" role="status">正在读取任务费用…</p>
  <p v-if="unsupported" class="budget-note">Server 未提供任务累计费用信息，保留历史结果读取；不能把缺失费用当成零。</p>
  <p v-if="error" role="alert" class="request-error">任务费用读取失败：{{error}}</p>
  <template v-if="data">
   <p v-if="limitsUnsupported" class="budget-note">此 SDK 操作不支持费用上限。独立复核费用保持未知，可在执行结束后根据账单人工核对；不能自动重跑。</p>
   <p v-if="data.accountingVersion===0" class="budget-note">此历史任务未启用累计记账。重试沿用原有单次预算，历史结果与报告保持可读。</p>
   <template v-else>
    <dl class="budget-properties"><div><dt>单次上限</dt><dd>{{data.singleLimitUsd==null?'未设置':`$${data.singleLimitUsd}`}}</dd></div><div><dt>任务总预算</dt><dd>{{data.totalLimitMicros==null?'未设置':money(data.totalLimitMicros)}}</dd></div><div><dt>{{data.unknownAttempts.length||data.attempts.some(a=>a.state==='active')?'已记录费用下界':'累计估算'}}</dt><dd>{{money(data.estimatedMicros)}}</dd></div><div><dt>当前预留额度</dt><dd>{{money(data.reservedMicros)}}</dd></div><div><dt>可用剩余额度</dt><dd>{{data.totalLimitMicros==null?'未设总上限':money(data.remainingMicros)}}</dd></div><div><dt>下次可下发上限</dt><dd>{{data.totalLimitMicros==null?'沿用单次上限':data.nextLimitMicros==null?'暂不可下发':money(data.nextLimitMicros)}}</dd></div></dl>
    <p class="budget-note">金额来自 SDK 或人工估算，未与供应商账单核对。当前预留额度不表示已经产生费用。</p>
    <p v-if="data.unknownAttempts.length" class="budget-block" role="status">第 {{data.unknownAttempts.join('、')}} 次执行的完整费用未知。{{data.totalLimitMicros!=null?'累计预算任务需先核对估算费用，才能重试；增加总预算不能跳过核对。':'当前未设置总预算，费用累计仅为下界。'}}</p>
    <p v-if="data.state==='exhausted'" class="budget-block" role="status">累计预算已用尽，请调整任务总预算后重试。</p>
    <div class="budget-table-wrap"><table><caption class="sr-only">每次执行的费用依据</caption><thead><tr><th>执行</th><th>下发上限</th><th>计入累计</th><th>来源 / 状态</th><th>依据时间</th><th>操作</th></tr></thead><tbody><tr v-for="attempt in data.attempts" :key="attempt.execution.attempt"><td><button class="attempt-link" @click="emit('selectAttempt',attempt.execution.attempt)">第 {{attempt.execution.attempt}} 次</button></td><td>{{attempt.grantedMicros==null?'未设置':money(attempt.grantedMicros)}}</td><td>{{money(attempt.bookedMicros)}}</td><td>{{sources[attempt.source]}} · {{attempt.state==='unknown'?'完整费用未知':attempt.state==='active'?'执行中':'已收尾'}}<small v-if="attempt.reason">{{reasons[attempt.reason]??attempt.reason}}</small><small v-if="attempt.note">{{attempt.updatedBy}} · {{attempt.note}}</small></td><td>{{absoluteTime(attempt.updatedAt)}}</td><td><Button v-if="editable&&['completed','partial','failed','canceled','timeout'].includes(status)&&attempt.state==='unknown'" variant="ghost" size="sm" :aria-label="`核对第 ${attempt.execution.attempt} 次费用`" @click="edit(attempt)">核对费用</Button></td></tr><tr v-if="!data.attempts.length"><td colspan="6">尚未执行，没有 SDK 费用记录。</td></tr></tbody></table></div>
   </template>
   <details @toggle="rawOpen=($event.target as HTMLDetailsElement).open"><summary>原始费用快照</summary><CodeViewer v-if="rawOpen" :code="JSON.stringify(data,null,2)" language="json" /></details>
  </template>
 </section>
 <Dialog v-model:open="open" :title="target?'核对执行费用':'调整任务总预算'" width="480px">
  <form id="budget-form" class="budget-form" @submit.prevent="save"><p v-if="formError" class="field-error" role="alert">{{formError}}</p><p class="budget-note">{{target?'人工估算会被明确标记，并保留原始 SDK 证据；低估费用会放大可用额度。金额不能低于已记录费用。':'调整仅影响后续执行，单次上限及已下发额度保持不变。金额不能低于累计估算。'}}</p><label for="budget-amount">{{target?'本次执行估算费用（USD）':'任务总预算（USD）'}}</label><Input id="budget-amount" v-model="value" type="number" min="0" step="0.000001" inputmode="decimal" /><template v-if="target"><label for="budget-note">核对依据</label><textarea id="budget-note" v-model="note" maxlength="1024" rows="3" placeholder="说明使用了哪份报告或费用记录；请勿填写凭据。" /></template></form>
  <template #footer><Button variant="secondary" @click="open=false">取消</Button><Button type="submit" form="budget-form" :loading="saving">{{target?'保存人工估算':'保存总预算'}}</Button></template>
 </Dialog>
</template>
<style scoped>
.execution-budget{border-top:1px solid var(--border-default);border-bottom:1px solid var(--border-default);padding:var(--space-4) 0;margin:var(--space-4) 0;min-width:0}header{display:flex;align-items:center;gap:var(--space-3);flex-wrap:wrap}h2{font-size:var(--font-size-md);font-weight:var(--font-weight-medium)}header>span{font-size:var(--font-size-xs);color:var(--text-secondary)}.budget-actions{display:flex;gap:var(--space-2);margin-left:auto}.budget-properties{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:var(--space-3);margin:var(--space-3) 0}.budget-properties>div{min-width:0}dt,.budget-note{font-size:var(--font-size-xs);color:var(--text-secondary)}dd{margin:var(--space-1) 0 0;font-size:var(--font-size-sm);font-variant-numeric:tabular-nums;overflow-wrap:anywhere}.budget-note,.budget-block{line-height:var(--line-height-body);margin:var(--space-2) 0}.budget-block{font-size:var(--font-size-sm);color:var(--text-primary)}.budget-table-wrap{max-width:100%;overflow:auto;margin:var(--space-3) 0}table{width:100%;border-collapse:collapse;font-size:var(--font-size-xs);white-space:nowrap}th{text-align:left;color:var(--text-secondary);font-weight:var(--font-weight-medium)}td,th{padding:var(--space-2);border-bottom:1px solid var(--border-subtle)}td small{display:block;max-width:40ch;white-space:normal;overflow-wrap:anywhere;color:var(--text-secondary)}.attempt-link{color:var(--accent-primary);text-decoration:underline;text-underline-offset:var(--space-1)}summary{font-size:var(--font-size-xs);cursor:pointer;color:var(--text-secondary)}.budget-form{display:flex;flex-direction:column;gap:var(--space-2);font-size:var(--font-size-sm)}textarea{border:1px solid var(--border-control);background:var(--surface-primary);border-radius:var(--radius-sm);padding:var(--space-2);width:100%;color:var(--text-primary)}.field-error{color:var(--state-danger);font-size:var(--font-size-sm)}
@media(max-width:700px){.budget-properties{grid-template-columns:repeat(2,minmax(0,1fr))}.budget-actions{margin-left:0}}
</style>
