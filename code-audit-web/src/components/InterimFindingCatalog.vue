<script setup lang="ts">
import {computed,nextTick,onBeforeUnmount,ref,shallowRef,watch} from 'vue';
import {useRoute,useRouter} from 'vue-router';
import {api,ApiError,type CatalogPage} from '../api';
import type {Project} from '../types';
import {interimExecutionPage,type InterimExecutionSummary} from '../utils/interim-findings';
import {absoluteTime} from '../utils/time';
import Button from './ui/Button.vue';
import Input from './ui/Input.vue';
import Select from './ui/Select.vue';
import CatalogPagination from './CatalogPagination.vue';
import InterimFindings from './InterimFindings.vue';
const props=defineProps<{active:boolean;projects:Project[]}>();
const emit=defineEmits<{open:[]}>();
const route=useRoute(),router=useRouter(),result=shallowRef<CatalogPage<InterimExecutionSummary>|null>(null),selected=shallowRef<InterimExecutionSummary|null>(null);
const loading=ref(false),error=ref(''),search=ref('');let generation=0,disposed=false,debounce:ReturnType<typeof setTimeout>|undefined;
const project=computed(()=>props.active?String(route.query.interimProject??''):''),status=computed(()=>props.active?String(route.query.interimStatus??''):'');
const projectOptions=computed(()=>[{value:'0',label:'未归属项目'},...props.projects.map(p=>({value:String(p.id),label:p.name}))]);
const statusOptions=[{value:'timeout',label:'超时'},{value:'failed',label:'失败'},{value:'scanning',label:'执行中'},{value:'completed',label:'已完成'},{value:'partial',label:'部分完成'},{value:'canceled',label:'已取消'}];
const label=(s:string)=>statusOptions.find(v=>v.value===s)?.label??({claimed:'准备中',cancel_requested:'正在取消'}[s]??s);
const recentCount=computed(()=>result.value?.items.reduce((n,v)=>n+(v.observation?.candidateRecords??0),0)??0);
const diagnostics=computed(()=>result.value?.items.filter(v=>v.state==='rejected').length??0);
const selectedRunning=computed(()=>!!selected.value&&['claimed','scanning','cancel_requested'].includes(selected.value.status));
async function load(){const epoch=++generation;loading.value=true;error.value='';try{
 const value=interimExecutionPage(await api.interimExecutionPage({page:props.active?Number(route.query.interimPage)||1:1,pageSize:20,q:props.active?String(route.query.interimQ??''):'',projectId:project.value||undefined,status:status.value||undefined}));
 if(disposed||epoch!==generation)return;if(!value)throw new Error('中间发现目录的合同或执行身份不匹配，已拒绝展示。');result.value=value;
 const task=Number(route.query.interimTask),attempt=Number(route.query.interimAttempt);selected.value=props.active?value.items.find(v=>v.taskId===task&&v.attempt===attempt)??null:null;if(selected.value){await nextTick();if(!disposed&&epoch===generation)document.getElementById('selected-interim-execution')?.focus();}
 }catch(e){if(!disposed&&epoch===generation)error.value=e instanceof ApiError&&e.status===404?'当前 Server 尚未提供全局中间发现目录；请到扫描详情查看已保存证据。':(e as Error).message;}finally{if(!disposed&&epoch===generation)loading.value=false}}
async function change(values:Record<string,string|number|undefined>){generation++;loading.value=true;const query={...route.query,...values};if(router.resolve({query}).fullPath===route.fullPath)await load();else await router.push({query})}
function choose(row:InterimExecutionSummary){void change({interimTask:row.taskId,interimAttempt:row.attempt});}
watch(()=>[props.active,route.query.interimPage,route.query.interimProject,route.query.interimStatus,route.query.interimQ,route.query.interimTask,route.query.interimAttempt],()=>{clearTimeout(debounce);search.value=props.active?String(route.query.interimQ??''):'';selected.value=null;result.value=null;void load()},{immediate:true});
watch(search,v=>{if(v===String(route.query.interimQ??'')||!props.active)return;generation++;clearTimeout(debounce);debounce=setTimeout(()=>void change({interimQ:v.trim()||undefined,interimPage:undefined,interimTask:undefined,interimAttempt:undefined}),300)});
onBeforeUnmount(()=>{disposed=true;generation++;clearTimeout(debounce)});
</script>
<template>
 <section class="interim-catalog" aria-label="全局中间发现">
  <div v-if="error" class="catalog-error" role="alert">中间发现目录读取失败：{{error}}<Button size="sm" variant="outline" :disabled="loading" @click="load">重读中间发现目录</Button></div>
  <template v-if="!active">
   <div class="interim-entry" role="note">
    <div><strong>失败或超时的扫描，也可查看已保存的中间发现。</strong><p v-if="result">最近 {{result.items.length}} 次 Standard 执行已保存 <b>{{recentCount}}</b> 条候选记录，尚未验证，可能重复，不计入上方最终漏洞数量。<span v-if="diagnostics"> {{diagnostics}} 次执行的证据索引需要检查。</span></p><p v-else-if="loading">正在检查已保存的中间发现…</p></div>
    <Button size="sm" variant="outline" @click="emit('open')">查看中间发现与证据</Button>
   </div>
  </template>
  <template v-else>
   <p class="catalog-note">按原任务与执行次数保留候选和证据，失败或超时也可读取。模型声明尚未验证，同一问题可能重复出现；最终漏洞仍在“已归档漏洞”中。</p>
   <div class="catalog-toolbar"><Input v-model="search" aria-label="搜索中间发现执行" placeholder="搜索任务编号、源码包或项目…" maxlength="256"/><Select :model-value="project" :options="projectOptions" clearable placeholder="全部项目" aria-label="中间发现所属项目" @change="change({interimProject:$event||undefined,interimPage:undefined,interimTask:undefined,interimAttempt:undefined})"/><Select :model-value="status" :options="statusOptions" clearable placeholder="全部执行状态" aria-label="中间发现执行状态" @change="change({interimStatus:$event||undefined,interimPage:undefined,interimTask:undefined,interimAttempt:undefined})"/><Button size="sm" variant="outline" :disabled="loading" @click="load">刷新执行证据目录</Button></div>
   <p v-if="loading&&!result" class="catalog-note" role="status">正在读取执行证据目录…</p>
   <div class="catalog-table"><table :aria-busy="loading" aria-label="中间发现执行目录"><thead><tr><th>任务与源码</th><th>执行</th><th>候选记录</th><th>证据</th><th>开始时间</th><th>操作</th></tr></thead><tbody><tr v-for="row in result?.items??[]" :key="row.taskId+':'+row.attempt" :class="{selected:selected?.taskId===row.taskId&&selected?.attempt===row.attempt}"><td><span>任务 #{{row.taskId}} · {{row.scanNo}}</span><small>{{row.sourceName||'冻结源码'}}<template v-if="row.projectName"> · {{row.projectName}}</template></small></td><td>第 {{row.attempt}} 次 · {{label(row.status)}}</td><td><template v-if="row.observation"><b>{{row.observation.candidateRecords}}</b><small>候选 · 未验证</small></template><template v-else>—<small>数量未知</small></template></td><td><template v-if="row.observation">{{row.observation.documentCount}} 份<small>{{row.state==='incomplete'?'部分证据不完整':'已保存'}}</small></template><template v-else>{{row.state==='rejected'?'校验未通过':'尚无中间快照'}}</template></td><td><time :datetime="row.startedAt">{{absoluteTime(row.startedAt)}}</time></td><td><Button size="sm" variant="ghost" :aria-pressed="selected?.taskId===row.taskId&&selected?.attempt===row.attempt" :aria-label="'查看任务 '+row.taskId+' 第 '+row.attempt+' 次中间发现'" @click="choose(row)">查看候选与证据</Button></td></tr></tbody></table></div>
   <p v-if="!loading&&!error&&result&&!result.items.length" class="catalog-note">暂无匹配的 Standard 执行记录。历史最终报告仍可在“已归档漏洞”和扫描详情中查看。</p>
   <CatalogPagination label="中间执行分页" :page="result?.page" :page-size="result?.pageSize" :total="result?.total" :loading="loading" :unavailable="!!error" @change="change({interimPage:$event,interimTask:undefined,interimAttempt:undefined})"/>
   <section v-if="selected" id="selected-interim-execution" tabindex="-1" class="selected-execution" aria-label="所选中间发现执行"><header><h2>任务 #{{selected.taskId}} · 第 {{selected.attempt}} 次执行</h2><RouterLink :to="{name:'scan-detail',params:{id:selected.taskId},query:{attempt:selected.attempt},hash:'#interim-findings'}">打开扫描执行详情</RouterLink><Button size="sm" variant="ghost" @click="change({interimTask:undefined,interimAttempt:undefined})">关闭执行证据</Button></header><p class="catalog-note">{{selected.reason}}</p><InterimFindings :key="selected.taskId+':'+selected.attempt" :scan-id="selected.taskId" :attempt="selected.attempt" :source-hash="selected.sourceContentHash" :running="selectedRunning"/></section>
  </template>
 </section>
</template>
<style scoped>
.interim-catalog{min-width:0}.interim-entry{display:flex;align-items:center;justify-content:space-between;gap:var(--space-3);border-block:1px solid var(--border-default);padding-block:var(--space-3);margin-block:var(--space-3);font-size:var(--font-size-sm)}.interim-entry strong{font-weight:var(--font-weight-medium)}.interim-entry p,.catalog-note{color:var(--text-secondary);font-size:var(--font-size-sm);line-height:var(--line-height-body);margin-block:var(--space-2);overflow-wrap:anywhere}.catalog-toolbar{display:flex;flex-wrap:wrap;align-items:center;gap:var(--space-2);margin-block:var(--space-3)}.catalog-toolbar>input{flex:1;min-width:220px;max-width:420px}.catalog-toolbar :deep(.select-root){max-width:200px}.catalog-error{color:var(--state-danger);display:flex;flex-wrap:wrap;align-items:center;gap:var(--space-2);font-size:var(--font-size-sm);margin-block:var(--space-2)}.catalog-table{overflow:auto;border:1px solid var(--border-default)}table{width:100%;min-width:760px;border-collapse:collapse;text-align:start;font-size:var(--font-size-sm)}th,td{padding:var(--space-2) var(--space-3);vertical-align:top;border-bottom:1px solid var(--border-subtle)}th{color:var(--text-secondary);background:var(--surface-secondary);font-weight:var(--font-weight-medium);white-space:nowrap}td:first-child{min-width:180px;max-width:360px;overflow-wrap:anywhere}td small{display:block;font-size:var(--font-size-xs);color:var(--text-secondary);margin-top:var(--space-1)}td time{white-space:nowrap;font-size:var(--font-size-xs)}tr:last-child td{border-bottom:0}tr:hover{background:var(--surface-hover)}tr.selected{background:var(--surface-selected)}.selected-execution>header{display:flex;flex-wrap:wrap;align-items:center;gap:var(--space-3);border-top:1px solid var(--border-default);padding-top:var(--space-3)}h2{font-size:var(--font-size-sm);font-weight:var(--font-weight-medium);margin:0;margin-inline-end:auto}.selected-execution>header a{font-size:var(--font-size-sm);color:var(--accent-primary)}@media(max-width:760px){.interim-entry{align-items:flex-start;flex-direction:column}.catalog-toolbar>input{min-width:0;max-width:none;flex-basis:100%}}
</style>
