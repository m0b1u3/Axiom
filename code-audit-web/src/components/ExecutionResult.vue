<script setup lang="ts">
import {computed,onUnmounted,ref,shallowRef,watch} from 'vue';
import {api} from '../api';
import type {AttemptResult} from '../types';
import type {ArtifactRef} from '../contracts';
import {estimatedCost,reportedModel,processMismatch,nativeCostSections} from '../utils/scan-result';
import {absoluteTime} from '../utils/time';
import Button from './ui/Button.vue';
import CodeViewer from './CodeViewer.vue';
const props=defineProps<{scanId:number;attempt:number;status:string}>();
const archive=shallowRef<AttemptResult|null>(null),busy=ref(false),error=ref(''),artifactError=ref(''),downloading=ref('');
const rawOpen=ref(false);let generation=0;
const result=computed(()=>archive.value?.result);
const sdk=computed(()=>result.value?.extensions?.sdk);
const costs=computed(()=>result.value?nativeCostSections(result.value):[]);
const raw=computed(()=>JSON.stringify(archive.value?.completion??archive.value,null,2));
async function load(){
 const current=++generation;archive.value=null;error.value='';rawOpen.value=false;busy.value=false;
 if(props.attempt<1 || !['completed','partial','failed','canceled','timeout'].includes(props.status))return;
 busy.value=true;
 try{const value=await api.attemptResult(props.scanId,props.attempt);if(current===generation)archive.value=value}
 catch(e){if(current===generation)error.value='归档结果读取失败：'+(e as Error).message}
 finally{if(current===generation)busy.value=false}
}
watch(()=>[props.scanId,props.attempt,props.status],load,{immediate:true});
onUnmounted(()=>generation++);
function save(blob:Blob,name:string){const url=URL.createObjectURL(blob);const link=document.createElement('a');link.href=url;link.download=name;link.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}
function download(){save(new Blob([raw.value+'\n'],{type:'application/json'}),`scan-${props.scanId}-attempt-${props.attempt}.json`)}
async function artifact(ref:ArtifactRef){artifactError.value='';downloading.value=ref.relativePath;try{save(await api.resultArtifact(props.scanId,props.attempt,ref.relativePath),ref.relativePath.split('/').at(-1)??'artifact')}catch(e){artifactError.value='产物下载失败：'+(e as Error).message}finally{downloading.value=''}}
</script>
<template>
 <section v-if="busy||error||archive" class="execution-result" aria-label="归档结果">
  <div class="result-heading"><h2>归档结果</h2><span>第 {{attempt}} 次执行</span><Button v-if="archive?.available" variant="outline" size="sm" @click="download">下载完整结果 JSON</Button></div>
  <p v-if="busy" role="status">正在读取本次 SDK 结果…</p>
  <div v-if="error" class="request-error" role="alert">{{error}}<Button variant="ghost" size="sm" @click="load">重新加载</Button></div>
  <template v-if="result">
   <p v-if="result.status!=='completed'" class="result-note">SDK 结果为 {{result.status}}；已保留费用、发现与证据，本次未形成完整扫描结论。</p>
   <p v-if="archive?.completion?.executionStatus!==result.status" class="result-note">执行状态为 {{archive?.completion?.executionStatus}}，保留的 SDK 结果状态为 {{result.status}}。保留的发现不代表完整扫描结论。</p>
   <p v-if="processMismatch(archive?.completion)" class="result-note" role="status">SDK 结果已校验并归档；进程退出与结果状态不同，退出记录保留在执行元数据中。</p>
   <dl class="result-properties">
    <div><dt>估算费用 · USD</dt><dd class="mono">{{estimatedCost(result.cost?.estimatedUsd)}}</dd></div>
    <div><dt>输入 / 输出 Token</dt><dd class="mono">{{result.cost?.inputTokens??'未知'}} / {{result.cost?.outputTokens??'未知'}}</dd></div>
    <div><dt>配置模型</dt><dd>{{result.model.providerId}} · {{result.model.model}}</dd></div>
    <div><dt>SDK 报告的模型</dt><dd>{{reportedModel(sdk)}}</dd></div>
    <div><dt>引擎 / SDK 版本</dt><dd>{{result.engine.name}} · {{result.engine.version??'未知'}}</dd></div>
    <div><dt>插件版本</dt><dd>{{result.engine.pluginVersion??'未知'}}</dd></div>
   </dl>
   <p class="result-note">费用由引擎估算，可能与供应商账单不同；缓存、推理 Token 和原生计费字段保留在完整 SDK JSON 中。</p>
   <details v-if="costs.length" class="result-details"><summary>原生计费与 Token 记录</summary><section v-for="section in costs" :key="section.title"><h3>{{section.title}}</h3><dl class="metadata"><div v-for="row in section.rows" :key="row.key"><dt>{{row.label}}</dt><dd class="mono">{{row.value}}</dd></div></dl></section></details>
   <details class="result-details"><summary>执行元数据与模型快照</summary><dl class="metadata">
    <div><dt>执行身份</dt><dd class="mono">{{result.execution.taskId}} / attempt {{result.execution.attempt}}</dd></div>
    <div><dt>Scanner 开始 / 结束</dt><dd>{{absoluteTime(result.startedAt)}} / {{absoluteTime(result.finishedAt)}}</dd></div>
    <div><dt>连接 / 协议</dt><dd>{{result.model.connectionType}} / {{result.model.wireApi}}</dd></div>
    <div><dt>模型端点</dt><dd class="mono">{{result.model.baseUrl??'OpenAI 原生连接'}}</dd></div>
    <div><dt>配置版本</dt><dd class="mono">{{result.model.configurationRevision}}</dd></div>
    <div><dt>补丁版本</dt><dd class="mono">{{result.engine.patchRevision??'未知'}}</dd></div>
    <div><dt>进程退出 / 信号</dt><dd>{{archive?.completion?.process.code??'未知'}} / {{archive?.completion?.process.signal??'无'}}</dd></div>
    <div><dt>归档 SHA-256</dt><dd class="mono">{{archive?.archiveSha256??'未知'}}</dd></div>
   </dl></details>
   <details v-if="result.artifacts.length" class="result-details"><summary>原生产物 · {{result.artifacts.length}}</summary>
    <div class="artifact-scroll"><table><thead><tr><th>产物</th><th>字节数</th><th>SHA-256</th><th><span class="sr-only">操作</span></th></tr></thead><tbody><tr v-for="ref in result.artifacts" :key="ref.relativePath"><td class="mono">{{ref.relativePath}}</td><td class="mono">{{ref.sizeBytes}}</td><td class="mono hash">{{ref.sha256}}</td><td><Button variant="ghost" size="sm" :disabled="!!downloading" @click="artifact(ref)">{{downloading===ref.relativePath?'下载中…':'下载产物'}}</Button></td></tr></tbody></table></div>
    <p v-if="artifactError" role="alert" class="request-error">{{artifactError}}</p>
   </details>
  </template>
  <p v-else-if="archive?.available" class="result-note">{{archive.contractVersion===1?'SDK 未生成可提交的结果，执行错误与进程记录已归档。':'此历史执行只保留旧版结果；费用、模型快照和引擎元数据未知。'}}<template v-if="archive.completion?.error"> {{archive.completion.error.code}}：{{archive.completion.error.message}}</template></p>
  <p v-else-if="archive" class="result-note">本次执行没有可用的归档结果。</p>
  <template v-if="archive?.available"><button type="button" class="raw-toggle" :aria-expanded="rawOpen" :aria-controls="`result-raw-${attempt}`" @click="rawOpen=!rawOpen">{{rawOpen?'收起':'查看'}}完整 JSON（含 SDK 原始结果与扩展字段）</button><div v-if="rawOpen" :id="`result-raw-${attempt}`" role="region" aria-label="完整结果 JSON"><CodeViewer :code="raw" language="json" :max-height="360" /></div></template>
 </section>
</template>
<style scoped>
.execution-result{border-top:1px solid var(--border-default);border-bottom:1px solid var(--border-default);padding:var(--space-3) 0;margin:var(--space-3) 0;min-width:0}
.result-heading{display:flex;align-items:center;flex-wrap:wrap;gap:var(--space-3);margin-bottom:var(--space-3)}h2{font-size:var(--font-size-sm);font-weight:600;margin:0}.result-heading>span,.result-note{font-size:var(--font-size-xs);color:var(--text-secondary)}.result-note{line-height:var(--line-height-body);margin:var(--space-2) 0;overflow-wrap:anywhere}
.result-properties{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:var(--space-3);margin:0}.result-properties>div{min-width:0}dt{font-size:var(--font-size-xs);color:var(--text-secondary)}dd{font-size:var(--font-size-sm);color:var(--text-primary);margin:var(--space-1) 0 0;overflow-wrap:anywhere}
.result-details{margin-top:var(--space-2)}h3{font-size:var(--font-size-xs);font-weight:600;margin:var(--space-3) 0 var(--space-2)}summary,.raw-toggle{font-size:var(--font-size-xs);color:var(--text-secondary);cursor:pointer}summary:focus-visible,.raw-toggle:focus-visible{outline:2px solid var(--border-focus);outline-offset:2px}.raw-toggle{display:block;background:none;border:0;padding:var(--space-2) 0;text-align:left}.raw-toggle:hover{color:var(--text-primary)}
.metadata{margin:var(--space-2) 0}.metadata>div{display:grid;grid-template-columns:180px minmax(0,1fr);gap:var(--space-3);margin:var(--space-2) 0}.metadata dd{margin:0;font-size:var(--font-size-xs)}.artifact-scroll{overflow:auto;max-width:100%;margin-top:var(--space-2)}table{width:100%;border-collapse:collapse;font-size:var(--font-size-xs)}th{text-align:left;font-weight:500;color:var(--text-secondary)}th,td{padding:var(--space-2);border-bottom:1px solid var(--border-subtle)}.hash{overflow-wrap:anywhere;min-width:180px;max-width:240px}
@media(max-width:800px){.result-properties{grid-template-columns:repeat(2,minmax(0,1fr))}.metadata>div{grid-template-columns:minmax(0,1fr);gap:var(--space-1)}}
</style>
