<script setup lang="ts">
import {computed,h,onUnmounted,ref,watch} from 'vue';
import type {ColumnDef} from '@tanstack/vue-table';
import {api,type CatalogPage} from '../api';
import type {ArtifactRef,ExecutionRef,ScanResult,SDKNativeExport,SDKRepositoryFinding} from '../contracts';
import {sdkNativeView} from '../utils/sdk-native';
import Button from './ui/Button.vue';import Input from './ui/Input.vue';import Select from './ui/Select.vue';
import DataTable from './DataTable.vue';import CatalogPagination from './CatalogPagination.vue';import SDKDiagnostics from './SDKDiagnostics.vue';
const props=defineProps<{scanId:number;attempt:number;result:ScanResult}>();
const view=computed(()=>sdkNativeView(props.result.extensions?.sdkNative,props.result.execution,props.result.artifacts));
const native=computed(()=>view.value.value);
const artifactErrorContext=ref('report');
const busyArtifact=ref(''),artifactError=ref(''),previewText=ref(''),previewFormat=ref('');
const query=ref(''),historyPage=ref(1),selected=ref<SDKRepositoryFinding|null>(null),selectedSDKID=ref('');
const links=ref<CatalogPage<{execution:ExecutionRef;scanNo:string;status:string}>|null>(null),linksLoading=ref(false),linksError=ref('');
let generation=0,linkGeneration=0;
function reset(){generation++;linkGeneration++;busyArtifact.value='';artifactError.value='';previewText.value='';selected.value=null;selectedSDKID.value='';links.value=null;linksLoading.value=false;linksError.value='';historyPage.value=1;query.value='';}
watch(()=>[props.scanId,props.attempt,props.result],reset,{immediate:true});onUnmounted(()=>{generation++;linkGeneration++;});
const filtered=computed(()=>{const text=query.value.trim().toLowerCase();return (native.value?.repositoryHistory.records??[]).filter(f=>!text||[f.title,f.findingId,f.targetId,f.scanId].some(v=>v.toLowerCase().includes(text)));});
const pageRows=computed(()=>filtered.value.slice((historyPage.value-1)*50,historyPage.value*50));
watch(query,()=>historyPage.value=1);
function inspect(row:SDKRepositoryFinding){selected.value=row;selectedSDKID.value=row.scanId;linkGeneration++;links.value=null;linksError.value='';linksLoading.value=false;}
watch(selectedSDKID,()=>{linkGeneration++;links.value=null;linksError.value='';linksLoading.value=false;});
const columns:ColumnDef<SDKRepositoryFinding>[]=[
 {accessorKey:'title',header:'SDK 历史发现',size:260,enableSorting:false,cell:c=>h('button',{class:'native-finding-title',onClick:(e:Event)=>{e.stopPropagation();inspect(c.row.original);}},c.getValue<string>())},
 {accessorKey:'status',header:'SDK 状态',size:95,enableSorting:false,cell:c=>c.getValue()==='closed'?'已关闭':'开放'},
 {accessorKey:'confirmedInLatestScan',header:'最近扫描确认',size:110,enableSorting:false,cell:c=>c.getValue()?'SDK 已确认':'未确认'},
 {accessorKey:'targetId',header:'原生 Target ID',size:180,enableSorting:false},
];
const scanOptions=computed(()=>[...new Set([selected.value?.scanId,...(selected.value?.knownScanIds??[])].filter((v):v is string=>!!v))].map(id=>({value:id,label:id})));
function save(blob:Blob,name:string){const url=URL.createObjectURL(blob),link=document.createElement('a');link.href=url;link.download=name;link.click();setTimeout(()=>URL.revokeObjectURL(url),1000);}
async function artifact(ref:ArtifactRef,preview=false,format=''){
 const gen=generation;artifactError.value='';artifactErrorContext.value=format==='diagnostic'?'journal':'report';previewText.value='';
 if(preview&&ref.sizeBytes>262144){artifactError.value='该原生产物超过 256 KiB 预览上限，请下载完整文件。';return;}
 busyArtifact.value=ref.relativePath;
 try{const blob=await api.resultArtifact(props.scanId,props.attempt,ref.relativePath);if(gen!==generation)return;if(preview){previewText.value=await blob.text();previewFormat.value=format;}else save(blob,ref.relativePath.split('/').at(-1)??'sdk-artifact');}
 catch(e){if(gen===generation)artifactError.value='SDK 原生产物读取失败：'+(e as Error).message;}
 finally{if(gen===generation)busyArtifact.value='';}
}
async function loadLinks(page=1){const gen=++linkGeneration;linksLoading.value=true;linksError.value='';try{const value=await api.sdkHistoryLinks(props.scanId,props.attempt,selectedSDKID.value,page);if(gen===linkGeneration)links.value=value;}catch(e){if(gen===linkGeneration)linksError.value='关联执行读取失败：'+(e as Error).message;}finally{if(gen===linkGeneration)linksLoading.value=false;}}
const reportStates={available:'可读取',not_generated:'未生成',invalid:'未通过格式核验'};
const formats=[{key:'markdown',label:'Markdown'},{key:'sarif',label:'SARIF'}] as const;
const legacyReports=computed(()=>props.result.artifacts.filter(a=>a.kind==='report'||a.kind==='sarif'));
function exportFor(key:'markdown'|'sarif'):SDKNativeExport|undefined{return native.value?.reports[key];}
</script>
<template>
 <section class="sdk-native-result" aria-label="SDK 原生结果与导出">
  <h3>SDK 原生结果与导出</h3>
  <p v-if="view.invalid" role="alert">SDK 原生结果扩展的格式、版本、身份或产物引用不受支持；完整 JSON 与历史产物仍可访问。</p>
  <template v-if="native">
   <dl class="properties"><dt>原生 Scan ID</dt><dd>{{native.scanId??'未提供'}}</dd><dt>原生 Target ID</dt><dd>{{native.targetId??'未提供'}}</dd></dl>
   <div class="exports"><section v-for="format in formats" :key="format.key" :aria-label="'SDK '+format.label+' 原生产物'"><div class="export-heading"><strong>{{format.label}}</strong><span>{{reportStates[exportFor(format.key)!.status]}}</span><template v-if="exportFor(format.key)?.artifact"><Button variant="outline" size="sm" :disabled="!!busyArtifact" @click="artifact(exportFor(format.key)!.artifact!,true,format.label)">预览 {{format.label}}</Button><Button variant="ghost" size="sm" :disabled="!!busyArtifact" @click="artifact(exportFor(format.key)!.artifact!)">下载原生 {{format.label}}</Button></template></div><p v-if="exportFor(format.key)?.reason">{{exportFor(format.key)?.reason}}</p><dl v-if="exportFor(format.key)?.artifact" class="properties"><dt>归档路径 / 字节</dt><dd>{{exportFor(format.key)?.artifact?.relativePath}} · {{exportFor(format.key)?.artifact?.sizeBytes}}</dd><dt>SHA-256</dt><dd>{{exportFor(format.key)?.artifact?.sha256}}</dd></dl></section></div>
   <p v-if="busyArtifact" role="status">正在校验并读取原生产物…</p><p v-if="artifactError&&artifactErrorContext==='report'" role="alert" class="request-error">{{artifactError}}</p>
   <section v-if="previewText" aria-label="原生产物预览" class="artifact-preview"><div class="export-heading"><h4>{{previewFormat}} 原文预览</h4><Button size="sm" variant="ghost" @click="previewText=''">关闭预览</Button></div><pre>{{previewText}}</pre></section>
   <section aria-label="SDK 仓库级漏洞历史" class="repository-history"><h4>SDK 仓库级漏洞历史</h4><p>展示 SDK 本次返回的仓库历史。SDK 的开放/关闭与最近确认状态分别保留，不自动改变 Axiom 漏洞处理状态。</p><p v-if="native.repositoryHistory.reason" :role="native.repositoryHistory.state==='invalid'?'alert':undefined">{{native.repositoryHistory.reason}}</p><template v-if="native.repositoryHistory.state==='reported'"><Input v-model="query" aria-label="搜索 SDK 仓库历史" placeholder="搜索标题、Finding、Target 或 Scan ID"/><DataTable :data="pageRows" :columns="columns" :row-key="v=>v.findingId+':'+v.occurrenceId+':'+v.scanId" :row-click="inspect" empty-title="SDK 本次返回的仓库历史为空" empty-hint="这仅表示本次返回的数据，不代表全仓库没有漏洞。"/><CatalogPagination label="SDK 仓库历史" :page="historyPage" :page-size="50" :total="filtered.length" @change="p=>historyPage=p"/></template>
    <aside v-if="selected" aria-label="SDK 历史发现详情" class="history-inspector"><div class="export-heading"><h4>{{selected.title}}</h4><Button size="sm" variant="ghost" @click="selected=null">关闭历史详情</Button></div><p>{{selected.summary}}</p><dl class="properties"><dt>Finding / Occurrence</dt><dd>{{selected.findingId}} / {{selected.occurrenceId}}</dd><dt>Target ID</dt><dd>{{selected.targetId}}</dd><dt>SDK 状态 / 最近确认</dt><dd>{{selected.status}} / {{selected.confirmedInLatestScan?'已确认':'未确认'}}</dd><dt>已知起始时间</dt><dd>{{selected.knownSince??'未提供'}}</dd><dt>匹配 Finding ID</dt><dd>{{selected.matchedFindingIds?.join(', ')??'未提供'}}</dd></dl><p v-if="selected.knownScanIds===null">SDK 未提供跨扫描 ID 列表。</p><div class="link-tools"><Select v-model="selectedSDKID" :options="scanOptions" aria-label="选择关联原生扫描"/><Button variant="outline" size="sm" :loading="linksLoading" :disabled="!selectedSDKID" @click="loadLinks()">查找关联执行</Button></div><p v-if="linksError" role="alert" class="request-error">{{linksError}}</p><template v-if="links"><p v-if="!links.total">未找到同一源码的 Axiom 归档执行；原生 ID 继续保留。</p><p v-else-if="links.total>1">找到 {{links.total}} 个候选执行，未自动选择。</p><ul><li v-for="link in links.items" :key="link.execution.taskId+':'+link.execution.attempt"><RouterLink :to="'/scans/'+link.execution.taskId+'?attempt='+link.execution.attempt">{{link.scanNo}} · 第 {{link.execution.attempt}} 次执行</RouterLink><span> · {{link.status}}</span></li></ul><CatalogPagination label="SDK 关联执行" :page="links.page" :page-size="links.pageSize" :total="links.total" :loading="linksLoading" @change="loadLinks"/></template><details><summary>原始历史关联</summary><pre>{{JSON.stringify(selected,null,2)}}</pre></details></aside>
   </section>
   <SDKDiagnostics :value="native.diagnostics" :execution="native.execution"/>
   <Button v-if="native.diagnosticJournal" variant="outline" size="sm" :disabled="!!busyArtifact" @click="artifact(native.diagnosticJournal,false,'diagnostic')">下载完整 SDK 诊断 JSONL</Button><p v-if="artifactError&&artifactErrorContext==='journal'" role="alert" class="request-error">{{artifactError}}</p><p v-if="native.diagnosticJournalError" role="alert" class="request-error">{{native.diagnosticJournalError}}</p>
  </template>
  <template v-else><p>此历史结果未保存可读取的新版原生结果扩展。仍按已归档的产物引用下载，完整 SDK JSON 保持可访问。</p><Button v-for="ref in legacyReports" :key="ref.relativePath" size="sm" variant="outline" :disabled="!!busyArtifact" @click="artifact(ref)">下载历史原生产物 {{ref.relativePath}}</Button><p v-if="artifactError" role="alert" class="request-error">{{artifactError}}</p></template>
 </section>
</template>
<style scoped>
.sdk-native-result{border-top:1px solid var(--border-default);margin-top:var(--space-3);padding-top:var(--space-3);font-size:var(--font-size-xs);min-width:0}h3{font-size:var(--font-size-sm);font-weight:var(--font-weight-semibold);margin:0 0 var(--space-3)}h4{font-size:var(--font-size-sm);font-weight:var(--font-weight-medium);margin:0}p{color:var(--text-secondary);line-height:var(--line-height-body);margin:var(--space-2) 0;overflow-wrap:anywhere}.properties{display:grid;grid-template-columns:max-content minmax(0,1fr);gap:var(--space-2) var(--space-3);margin:var(--space-2) 0}dt{color:var(--text-tertiary)}dd{margin:0;overflow-wrap:anywhere;font-family:var(--font-mono)}.export-heading{display:flex;align-items:center;gap:var(--space-2);flex-wrap:wrap}.export-heading strong{font-weight:var(--font-weight-medium)}.export-heading span{color:var(--text-tertiary)}.exports>section,.repository-history{border-top:1px solid var(--border-subtle);margin-top:var(--space-3);padding-top:var(--space-3)}summary{cursor:pointer;color:var(--text-secondary)}pre{font:var(--font-size-xs)/var(--line-height-body) var(--font-mono);max-height:calc(var(--height-table-row)*9);overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere;padding:var(--space-2);background:var(--surface-secondary)}.request-error{color:var(--state-danger)}.artifact-preview,.history-inspector{border-block:1px solid var(--border-default);margin-top:var(--space-3);padding:var(--space-3) 0}.repository-history>input{margin:var(--space-2) 0;max-width:var(--inspector-width-lg)}.link-tools{display:flex;align-items:center;gap:var(--space-2);margin-top:var(--space-3);min-width:0}.link-tools>:first-child{min-width:0;flex:1}ul{padding-left:var(--space-4)}li{margin:var(--space-2) 0;overflow-wrap:anywhere}a{color:var(--accent-primary)}:deep(.native-finding-title){color:var(--text-primary);font-weight:var(--font-weight-medium);text-align:left}@media(max-width:650px){.properties{grid-template-columns:1fr;gap:var(--space-1)}.properties dd{margin-bottom:var(--space-2)}.link-tools{flex-wrap:wrap}}
</style>
