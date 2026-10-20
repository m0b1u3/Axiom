<script setup lang="ts">
import ExploitabilityReview from "./ExploitabilityReview.vue";
import {computed,ref,watch,onUnmounted} from 'vue';
import {api} from '../api';
import type {CoverageResponse} from '../types';
import {coverageSections,coverageLabel,coverageText} from '../utils/coverage';
import Button from './ui/Button.vue';
const props=defineProps<{scanId:number;attempt:number;status:string}>();
const response=ref<CoverageResponse|null>(null),error=ref(''),loading=ref(false);let epoch=0;
const terminal=computed(()=>['completed','partial','failed','timeout','canceled','abandoned'].includes(props.status));
const doc=computed(()=>response.value?.coverage??null),groups=computed(()=>doc.value?coverageSections(doc.value):[]);
const dispositions:Record<string,string>={reported:'已报告问题',no_issue_found:'本次未发现问题',rejected:'候选未成立',not_applicable:'不适用',needs_follow_up:'需要继续验证'};
async function load(){const run=++epoch;response.value=null;error.value='';loading.value=false;if(!terminal.value||props.attempt<1)return;loading.value=true;try{const value=await api.scanCoverage(props.scanId,props.attempt);if(run===epoch)response.value=value}catch(e){if(run===epoch)error.value=(e as Error).message}finally{if(run===epoch)loading.value=false}}
watch(()=>[props.scanId,props.attempt,props.status],()=>void load(),{immediate:true});onUnmounted(()=>{epoch++});
const checkpointBusy=ref(false),checkpointError=ref('');
const checkpoints=computed(()=>doc.value?.axiomCheckpoints as {evidenceRecords?:number;emptyWorkerThreadIds?:string[]}|undefined);
async function downloadCheckpoints(){if(checkpointBusy.value)return;checkpointBusy.value=true;checkpointError.value='';try{const blob=await api.scanCheckpoints(props.scanId,props.attempt);const url=URL.createObjectURL(blob);const a=document.createElement('a');a.href=url;a.download=`scan-${props.scanId}-attempt-${props.attempt}-evidence.jsonl`;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);}catch{checkpointError.value='中间证据下载失败，请稍后重试。';}finally{checkpointBusy.value=false;}}
function download(){if(!doc.value)return;const url=URL.createObjectURL(new Blob([JSON.stringify(doc.value,null,2)],{type:'application/json'}));const a=document.createElement('a');a.href=url;a.download=`scan-${props.scanId}-attempt-${props.attempt}-coverage.json`;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}
function rowTitle(item:unknown){if(!item||typeof item!=='object')return coverageText(item);const v=item as Record<string,unknown>;return coverageText(v.label??v.pattern??v.question??v.id??'记录')}
function objectValue(item:unknown,key:string):unknown{return item&&typeof item==='object'?(item as Record<string,unknown>)[key]:undefined}
</script>
<template>
<section class="coverage-report" aria-label="扫描覆盖报告" :aria-busy="loading">
 <header><h2>覆盖报告 <span>第 {{attempt||'—'}} 次执行</span></h2><strong v-if="doc" :class="{incomplete:doc.completeness==='partial'}">{{coverageLabel(doc)}}</strong><Button v-if="doc" variant="ghost" size="sm" @click="download">下载完整 JSON</Button></header>
 <p v-if="error" role="alert">覆盖报告读取失败：{{error}} <Button variant="outline" size="sm" @click="load">重试</Button></p>
 <p v-else-if="!doc" class="muted">{{loading?'正在读取覆盖报告…':!terminal?'扫描结束后显示覆盖报告。当前进度不能作为完整覆盖证明。':response?.reason||'没有可用的覆盖报告，无法确认完整性。'}}</p>
 <template v-else>
  <p class="muted">覆盖结论来自 SDK，仅针对本次指定范围。未发现漏洞不代表系统安全。<span v-if="response?.source==='legacy_artifact'"> 此报告从保留的历史产物读取。</span></p>
  <ExploitabilityReview v-if="doc.axiomValidationReview" :review="doc.axiomValidationReview" />
  <section v-if="checkpoints" class="coverage-group" aria-label="中间审计证据">
   <h3>中间审计证据</h3><p>已保存 {{checkpoints.evidenceRecords??0}} 条记录，包含未验证内容，不能作为已确认漏洞或完整覆盖证明。</p>
   <p v-if="checkpoints.emptyWorkerThreadIds?.length">{{checkpoints.emptyWorkerThreadIds.length}} 个子任务未交付结论，相关覆盖不完整。</p>
   <Button v-if="checkpoints.evidenceRecords" variant="outline" size="sm" :loading="checkpointBusy" @click="downloadCheckpoints">下载中间证据</Button><p v-if="checkpointError" role="alert">{{checkpointError}}</p>
  </section>
  <dl class="scope"><div v-for="entry in [['includePaths','纳入路径'],['excludePaths','排除路径'],['inventoryStrategy','清点方式']]" :key="entry[0]"><dt>{{entry[1]}}</dt><dd>{{doc[entry[0]!]===undefined?'未记录':Array.isArray(doc[entry[0]!])?(doc[entry[0]!] as unknown[]).map(coverageText).join('、')||'无记录项':coverageText(doc[entry[0]!])}}</dd></div></dl>
  <details v-for="group in groups" :key="group.key" :open="group.items.length>0&&group.key!=='surfaces'" class="coverage-group">
   <summary>{{group.title}} <span>{{group.available?`${group.items.length} 项`:'未记录'}}</span></summary>
   <p v-if="!group.available" class="muted">该报告没有保存此字段，不能据此判断没有遗漏。</p><p v-else-if="!group.items.length" class="muted">本报告没有记录此类条目。</p>
   <article v-for="(item,index) in group.items" :key="index"><h3>{{rowTitle(item)}}</h3><span v-if="objectValue(item,'disposition')" class="disposition">{{dispositions[String(objectValue(item,'disposition'))]??objectValue(item,'disposition')}}</span><p v-for="key in ['reason','notes','riskArea','followUpPrompt']" :key="key"><template v-if="objectValue(item,key)">{{coverageText(objectValue(item,key))}}</template></p><p v-if="objectValue(item,'paths')">路径：{{coverageText(objectValue(item,'paths'))}}</p><p v-if="objectValue(item,'receiptRefs')">证据引用：{{coverageText(objectValue(item,'receiptRefs'))}}</p><details><summary>查看原始记录</summary><pre>{{coverageText(item)}}</pre></details></article>
  </details>
  <details class="coverage-group"><summary>完整原始报告</summary><pre>{{JSON.stringify(doc,null,2)}}</pre></details>
 </template>
</section>
</template>
<style scoped>
.coverage-report{margin-bottom:var(--space-5);border:1px solid var(--border-default);border-radius:var(--radius-lg);background:var(--surface-primary);padding:var(--space-4);min-width:0}.coverage-report header{display:flex;gap:var(--space-3);align-items:center;flex-wrap:wrap}.coverage-report h2{font-size:var(--font-size-md);margin:0}.coverage-report h2 span,.coverage-group summary span{font-size:var(--font-size-xs);font-weight:400;color:var(--text-tertiary);margin-left:var(--space-2)}.coverage-report header>strong{font-size:var(--font-size-xs);color:var(--text-secondary)}.coverage-report header>strong.incomplete{color:var(--state-warning)}.coverage-report header>button{margin-left:auto}.muted{color:var(--text-tertiary);font-size:var(--font-size-xs)}p,dd,h3{overflow-wrap:anywhere;white-space:pre-wrap;line-height:var(--line-height-body)}p{font-size:var(--font-size-sm)}.scope{font-size:var(--font-size-xs);display:grid;gap:var(--space-2);margin:var(--space-4) 0}.scope>div{display:grid;grid-template-columns:80px minmax(0,1fr);gap:var(--space-3)}dt{color:var(--text-tertiary)}dd{margin:0}.coverage-group{border-top:1px solid var(--border-default);padding:var(--space-3) 0}.coverage-group summary{cursor:pointer;font-size:var(--font-size-sm)}article{border-top:1px solid var(--border-subtle);padding:var(--space-3) 0}h3{font-size:var(--font-size-sm);font-weight:500;margin:0 0 var(--space-2)}article p:empty{display:none}.disposition{font-size:var(--font-size-xs);color:var(--text-secondary)}pre{white-space:pre-wrap;overflow-wrap:anywhere;max-height:360px;overflow:auto;padding:var(--space-3);background:var(--surface-secondary);font-size:var(--font-size-xs)}
</style>
