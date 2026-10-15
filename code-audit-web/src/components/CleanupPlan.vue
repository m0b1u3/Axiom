<script setup lang="ts">
import type {CleanupJob} from '../contracts';
import {computed,ref,onUnmounted} from 'vue';
import {api} from '../api';
import {formatStorageBytes as bytes} from '../utils/storage';
import Button from './ui/Button.vue';
import {absoluteTime} from '../utils/time';
const props=defineProps<{job:CleanupJob}>();
const emit=defineEmits<{change:[CleanupJob];completed:[]}>();
const running=ref(false),error=ref('');let timer:ReturnType<typeof setInterval>|undefined;onUnmounted(()=>{if(timer)clearInterval(timer)});
const labels={preview:'等待确认',running:'执行未收尾 · 可继续',failed:'未完成 · 可继续',completed:'已完成',canceled:'已放弃 · 剩余数据保留'};
const reasons:Record<string,string>={unregistered_evidence:'疑似未登记执行证据，保留',unreferenced:'无引用的遗留目录',historical_report:'历史报告',running_task:'运行中任务',unknown_cost:'费用未知的证据',source_reference:'源码仍被任务引用',snapshot_record:'已登记快照',cleanup_in_progress:'已有清理记录',unsafe_entry:'链接或特殊文件',grace_period:'仍在宽限期',inspection_incomplete:'检查未完成',already_absent:'目录已不存在'};

const eligible=computed(()=>props.job.items.filter(i=>i.state!=='protected'));
const knownBytes=computed(()=>eligible.value.reduce((sum,i)=>sum+BigInt(i.bytes??0),0n));
async function run(){if(running.value)return;running.value=true;error.value='';timer=setInterval(()=>{void api.cleanupJob(props.job.id).then(j=>emit('change',j)).catch(()=>undefined)},1000);try{const next=await api.executeCleanup(props.job.id,props.job.revision);emit('change',next);if(next.state==='completed')emit('completed');}catch(e){error.value=(e as Error).message;try{const latest=await api.cleanupJob(props.job.id);emit('change',latest);if(latest.state==='completed'){error.value='';emit('completed')}}catch{/* Keep the last valid plan; never assume success. */}}finally{if(timer)clearInterval(timer);timer=undefined;running.value=false}}
async function abandon(){if(running.value)return;running.value=true;error.value='';try{emit('change',await api.cancelCleanup(props.job.id,props.job.revision))}catch(e){error.value=(e as Error).message}finally{running.value=false}}
</script>
<template>
 <section class="cleanup-plan" aria-label="清理计划">
  <header><h3>{{job.kind==='orphans'?'遗留目录清理':job.kind==='scan'?'扫描删除计划':'快照删除计划'}}</h3><span>{{running?'Server 正在执行清理':labels[job.state]}}</span></header>
  <p class="meta">{{absoluteTime(job.updatedAt)}} · 创建者 {{job.createdBy}} · 记录 {{job.id}}</p>
  <p v-if="job.kind==='orphans'">只处理本次预览中无引用且已超过宽限期的对象；历史报告与已登记快照保留。</p>
  <p v-else>确认后将永久移除列出的产物及对象记录。运行中任务、费用未知的证据与其他对象引用会阻止清理。</p>
  <p>本次涉及 {{eligible.length}} 项，逻辑文件量 {{bytes(knownBytes)}}。实际释放空间以清理后的卷容量为准。</p>
  <p v-if="job.truncated" role="status">目录较多，本次仅显示部分对象；完成后可再次预览。</p>
  <p v-if="error" role="alert" class="request-error">{{error}} 请保留此记录并重试；可在系统设置的“存储与清理”继续。</p>
  <p v-else-if="job.error" role="status">{{job.error}}</p>
  <div class="plan-table" tabindex="0" aria-label="清理对象，可横向滚动"><table><caption class="sr-only">清理对象与保护原因</caption><thead><tr><th>目录 / 对象</th><th>逻辑占用</th><th>处理状态</th><th>依据</th></tr></thead><tbody><tr v-for="item in job.items" :key="item.root+'/'+item.relativePath"><td>{{item.root}} / {{item.relativePath}}</td><td>{{bytes(item.bytes)}}</td><td>{{item.state==='protected'?'保留':item.state==='planned'?'待处理':item.state==='quarantined'?'已隔离 · 待移除':'已移除 / 不存在'}}</td><td>{{reasons[item.reason]??item.reason}}</td></tr><tr v-if="!job.items.length"><td colspan="4">没有需要清理的目录。</td></tr></tbody></table></div>
  <details class="raw-view"><summary>原始清理记录</summary><pre>{{JSON.stringify(job,null,2)}}</pre></details>
  <Button v-if="!['completed','canceled'].includes(job.state)" variant="destructive" size="sm" :loading="running" :disabled="!eligible.length&&job.kind==='orphans'" @click="run">{{job.state==='preview'?'确认执行清理':'继续清理'}}</Button>
  <Button v-if="!['completed','canceled'].includes(job.state)" variant="ghost" size="sm" :disabled="running" @click="abandon">放弃本次清理</Button>
  <p v-if="!['completed','canceled'].includes(job.state)" class="meta">放弃不会恢复已移除的文件；已有隔离数据时，须先继续处理该记录。</p>
 </section>
</template>
<style scoped>
.cleanup-plan{min-width:0;font-size:var(--font-size-sm)}header{display:flex;align-items:center;gap:var(--space-3);flex-wrap:wrap}h3{font-size:var(--font-size-md);font-weight:var(--font-weight-medium)}header>span,.meta{font-size:var(--font-size-xs);color:var(--text-secondary)}p{line-height:var(--line-height-body);margin:var(--space-2) 0;overflow-wrap:anywhere}.plan-table{max-width:100%;overflow:auto;margin:var(--space-3) 0}table{border-collapse:collapse;width:100%;font-size:var(--font-size-xs);white-space:nowrap}th,td{padding:var(--space-2);border-bottom:1px solid var(--border-subtle);text-align:left}th{font-weight:var(--font-weight-medium);color:var(--text-secondary)}td:first-child{max-width:38ch;white-space:normal;overflow-wrap:anywhere}.raw-view{font-size:var(--font-size-xs);margin:var(--space-3) 0}.raw-view summary{cursor:pointer;color:var(--text-secondary)}.raw-view pre{max-height:20rem;overflow:auto;padding:var(--space-3);background:var(--surface-secondary);white-space:pre-wrap;overflow-wrap:anywhere}.request-error{color:var(--state-danger)}
</style>
