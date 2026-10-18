<script setup lang="ts">
import {computed,defineAsyncComponent,nextTick,onUnmounted,ref,shallowRef,watch} from 'vue';
import {api} from '../api';
import type {EvidenceGraph,GitDiffTarget} from '../contracts';
import {gitEvidenceBinding} from '../utils/git-diff';
import {evidenceGraphView,evidenceKinds,evidenceRelations,evidenceStates,graphSlice,graphSource,type GraphView} from '../utils/evidence-graph';
import Button from './ui/Button.vue';
import CodeViewer from './CodeViewer.vue';
const EvidenceGraphCanvas=defineAsyncComponent(()=>import('./EvidenceGraphCanvas.vue'));
const props=defineProps<{scanId:number;attempt:number;fingerprint:string;sourceHash?:string;gitTarget?:GitDiffTarget}>();
const open=ref(false),busy=ref(false),error=ref(''),view=ref<GraphView>('relations'),mode=ref<'graph'|'list'>(window.matchMedia('(max-width:600px)').matches?'list':'graph'),limit=ref(48),selected=ref(''),selectionKind=ref<'node'|'edge'>('node'),rawOpen=ref(false),wholeRaw=ref(false);
const graph=shallowRef<EvidenceGraph|null>(null),canvas=ref<InstanceType<typeof EvidenceGraphCanvas>|null>(null);
const gitBinding=computed(()=>{if(!props.gitTarget)return null;const source=graph.value?.sourceFinding as {extensions?:{axiomGitTarget?:unknown}}|undefined;return gitEvidenceBinding(source?.extensions?.axiomGitTarget,props.scanId,props.attempt,props.sourceHash,props.gitTarget);});
let generation=0,disposed=false;
const visible=computed(()=>graph.value?graphSlice(graph.value,view.value,limit.value):{nodes:[],edges:[],total:0});
const node=computed(()=>selectionKind.value==='node'?graph.value?.nodes.find(n=>n.id===selected.value):undefined);
const edge=computed(()=>selectionKind.value==='edge'?graph.value?.edges.find(e=>e.id===selected.value):undefined);
const reference=computed(()=>node.value?.sourcePointer??edge.value?.sourcePointer);
const raw=computed(()=>graph.value&&reference.value!==undefined?JSON.stringify(graphSource(graph.value.sourceFinding,reference.value).value,null,2):'');
const linked=computed(()=>graph.value&&node.value?graph.value.edges.filter(e=>e.from===node.value!.id||e.to===node.value!.id):[]);
const options=computed(()=>[...visible.value.nodes.map(n=>({id:n.id,kind:'node' as const,label:`${evidenceKinds[n.kind]} · ${n.label}`})),...visible.value.edges.map(e=>({id:e.id,kind:'edge' as const,label:`关系 · ${evidenceRelations[e.kind]}`}))]);
function select(kind:'node'|'edge',id:string){selectionKind.value=kind;selected.value=id;rawOpen.value=false;}
function locate(event:Event){const option=options.value.find(v=>v.id===(event.target as HTMLSelectElement).value);if(option)select(option.kind,option.id);}
function byId(id:string){return graph.value?.nodes.find(n=>n.id===id)?.label??'未知节点';}
function move(event:KeyboardEvent){
 if(!['ArrowDown','ArrowUp','ArrowLeft','ArrowRight','Home','End','Escape','Enter'].includes(event.key))return;
 const focused=event.target instanceof Element?event.target.closest<HTMLElement>('[data-evidence-object]')?.dataset.evidenceObject:undefined;
 // A focused list button keeps native Enter/Space activation. The canvas
 // itself uses Enter to move into the selected object's inspector.
 if(event.key==='Enter'&&focused)return;
 event.preventDefault();event.stopPropagation();
 if(event.key==='Escape'){selected.value='';return;}
 if(event.key==='Enter'){document.getElementById('evidence-selection-title')?.focus();return;}
 const index=options.value.findIndex(v=>v.id===(focused??selected.value));
 const direction=['ArrowDown','ArrowRight'].includes(event.key)?1:-1;
 const next=event.key==='Home'?0:event.key==='End'?options.value.length-1:Math.max(0,Math.min(options.value.length-1,index+direction));
 const item=options.value[next];if(!item)return;
 select(item.kind,item.id);
 if(mode.value==='list')void nextTick(()=>Array.from(document.querySelectorAll<HTMLButtonElement>('[data-evidence-object]')).find(el=>el.dataset.evidenceObject===item.id)?.focus());
}
async function load(){
 const current=++generation;busy.value=true;error.value='';graph.value=null;
 try{const result=await api.findingEvidenceGraph(props.scanId,props.attempt,props.fingerprint);if(disposed||current!==generation)return;const parsed=evidenceGraphView(result,props.scanId,props.attempt,props.fingerprint,props.sourceHash);if(!parsed)throw new Error('图谱版本、证据结构或执行身份不匹配；原始漏洞详情仍可读取。');if(props.gitTarget&&!gitEvidenceBinding((parsed.sourceFinding as {extensions?:{axiomGitTarget?:unknown}}).extensions?.axiomGitTarget,props.scanId,props.attempt,props.sourceHash,props.gitTarget))throw new Error('Git 证据版本绑定缺失或不匹配；原始漏洞详情仍可读取。');graph.value=parsed;const first=graphSlice(parsed,view.value,limit.value).nodes[0];if(first)select('node',first.id);}
 catch(e){if(!disposed&&current===generation){const status=(e as {status?:number}).status;error.value=status===404?'当前 Server 或这次历史执行没有可用的证据图谱；已有报告与原始 JSON 仍可读取。':(e as Error).message;}}
 finally{if(!disposed&&current===generation)busy.value=false;}
}
function toggle(){open.value=!open.value;if(open.value&&!graph.value&&!busy.value)void load();}
watch(()=>[props.scanId,props.attempt,props.fingerprint,props.sourceHash,JSON.stringify(props.gitTarget)],()=>{generation++;graph.value=null;error.value='';busy.value=false;selected.value='';rawOpen.value=false;wholeRaw.value=false;limit.value=48;if(open.value&&props.attempt>0)void load();});
watch(view,()=>{limit.value=48;rawOpen.value=false;const first=visible.value.nodes[0];if(first)select('node',first.id);else selected.value='';});
onUnmounted(()=>{disposed=true;generation++;});
</script>

<template>
 <section class="evidence-chain" aria-label="漏洞证据链" @keydown.esc.stop>
  <div class="chain-header"><h3>证据链</h3><Button variant="outline" size="sm" :disabled="attempt<1" :aria-expanded="open" aria-controls="finding-evidence-workspace" @click="toggle">{{open?'收起证据链':'查看证据链'}}</Button></div>
  <p v-if="attempt<1" class="chain-note">本任务尚无执行批次，证据图谱将在结果归档后提供。</p>
  <div v-if="open" id="finding-evidence-workspace">
   <p v-if="busy" role="status" class="chain-note">正在读取本次执行的证据引用并核对源码快照…</p>
   <div v-else-if="error" class="chain-error" role="alert"><p>{{error}}</p><Button variant="outline" size="sm" @click="load">重新读取证据链</Button></div>
   <template v-else-if="graph">
    <p class="chain-note">任务 {{graph.execution.taskId}} · 第 {{graph.execution.attempt}} 次执行 · {{graph.origin==='legacy_projection'?'历史字段投影':'SDK 证据引用'}} · {{graph.completeness==='complete'?'已有字段映射完整':graph.completeness==='empty'?'证据不足':'部分图谱'}}。推断关系使用虚线；代码存在不等于漏洞已验证。</p>
    <p v-if="gitBinding" class="chain-note">Git 提交差异 · Base {{gitBinding.target.baseCommit}} → HEAD {{gitBinding.target.headCommit}}。本图的代码观察只对照冻结 HEAD；base 的删除内容不视为 HEAD 中已观察的代码。</p>
    <p v-else-if="gitTarget" class="chain-error" role="alert">Git 证据版本绑定缺失或不匹配；请查看原始归档，不将片段视为选定提交的证据。</p>
    <details class="chain-origin"><summary>执行来源与完整性</summary><dl><dt>源码快照 SHA-256</dt><dd>{{graph.sourceContentHash??'历史记录未知'}}</dd><dt>归档 SHA-256</dt><dd>{{graph.archiveSha256??'历史记录未知'}}</dd><dt>图谱投影版本</dt><dd>{{graph.projectorVersion}}</dd></dl></details>
    <div class="chain-toolbar" role="group" aria-label="证据链视图">
     <Button size="sm" :variant="view==='relations'?'secondary':'ghost'" :aria-pressed="view==='relations'" @click="view='relations'">证据关系</Button>
     <Button size="sm" :variant="view==='path'?'secondary':'ghost'" :aria-pressed="view==='path'" @click="view='path'">攻击路径</Button>
     <span class="toolbar-divider"/>
     <Button size="sm" :variant="mode==='graph'?'secondary':'ghost'" :aria-pressed="mode==='graph'" @click="mode='graph'">图谱</Button>
     <Button size="sm" :variant="mode==='list'?'secondary':'ghost'" :aria-pressed="mode==='list'" @click="mode='list'">列表</Button>
     <template v-if="mode==='graph'"><Button variant="ghost" size="sm" @click="canvas?.fit()">适应画布</Button><Button variant="ghost" size="sm" aria-label="放大证据图谱" @click="canvas?.scale(1.25)">放大</Button><Button variant="ghost" size="sm" aria-label="缩小证据图谱" @click="canvas?.scale(.8)">缩小</Button></template>
    </div>
    <p v-if="graph.completeness==='empty'" class="chain-note">本结果没有足够的结构化证据链信息。可继续查看漏洞描述和原始 JSON。</p>
    <p v-else-if="!visible.nodes.length" class="chain-note">本结果没有结构化攻击路径。可切换到证据关系查看已有条目。</p>
    <div v-else class="graph-workspace">
     <div class="graph-surface">
      <div class="chain-selection"><label for="evidence-object">定位节点或关系</label><select id="evidence-object" :value="selected" @change="locate"><option value="">选择证据对象</option><option v-for="option in options" :key="option.id" :value="option.id">{{option.label}}</option></select></div>
      <div v-if="mode==='graph'" class="graph-keyboard" tabindex="0" role="group" aria-label="证据图谱键盘导航" aria-describedby="evidence-keyboard-help" @keydown="move"><EvidenceGraphCanvas ref="canvas" :nodes="visible.nodes" :edges="visible.edges" :view="view" :selected="selected" @select="select"/></div>
      <div v-else class="chain-list" @keydown="move">
       <ol aria-label="证据节点列表"><li v-for="n in visible.nodes" :key="n.id"><button type="button" :data-evidence-object="n.id" :aria-pressed="selectionKind==='node'&&selected===n.id" @click="select('node',n.id)"><span>{{evidenceKinds[n.kind]}}</span><strong>{{n.label}}</strong><span :class="'evidence-state '+n.state">{{evidenceStates[n.state]}}</span></button></li></ol>
       <p v-if="!visible.edges.length" class="chain-note">这些条目之间没有已记录的结构化关系。</p>
       <ul v-else aria-label="证据关系列表"><li v-for="e in visible.edges" :key="e.id"><button type="button" :data-evidence-object="e.id" :aria-pressed="selectionKind==='edge'&&selected===e.id" @click="select('edge',e.id)">{{evidenceRelations[e.kind]}}：{{byId(e.from)}} → {{byId(e.to)}}</button></li></ul>
      </div>
      <p id="evidence-keyboard-help" class="chain-note">图谱内方向键选择对象，Enter 查看详情，Esc 清除选择；列表提供相同证据。显示 {{visible.nodes.length}} / {{visible.total}} 个节点。</p>
      <Button v-if="visible.nodes.length<visible.total" variant="outline" size="sm" @click="limit=Math.min(256,limit+48)">展开更多节点</Button>
     </div>
     <section class="evidence-inspector" aria-label="选中证据详情" aria-live="polite">
      <template v-if="node||edge">
       <h4 id="evidence-selection-title" tabindex="-1">{{node?node.label:evidenceRelations[edge!.kind]}}</h4>
       <template v-if="node">
        <p :class="'evidence-state '+node.state">{{evidenceStates[node.state]}} · {{evidenceKinds[node.kind]}}</p><p>{{node.stateReason}}</p>
        <p v-if="node.location" class="code-location">{{node.location.path}}:{{node.location.startLine??'?'}}{{node.location.endLine?'–'+node.location.endLine:''}}</p>
        <CodeViewer v-if="node.code!==null" :code="node.code" :start-line="node.location?.startLine??1" :max-height="260" />
        <p v-if="node.explanation">{{node.explanation}}</p>
        <dl v-if="node.sdkEvidenceId||node.sourceFileSha256"><template v-if="node.sdkEvidenceId"><dt>SDK 证据 ID</dt><dd>{{node.sdkEvidenceId}}</dd></template><template v-if="node.sourceFileSha256"><dt>源码文件 SHA-256</dt><dd>{{node.sourceFileSha256}}</dd></template></dl>
        <p v-if="node.verification">验证依据：{{node.verification.rationale}} · {{node.verification.recordId}}</p>
        <div v-if="linked.length" class="related-evidence"><h5>关联关系</h5><ul><li v-for="e in linked" :key="e.id"><button type="button" @click="select('edge',e.id)">{{evidenceRelations[e.kind]}} · {{byId(e.from===node.id?e.to:e.from)}}</button></li></ul></div>
       </template>
       <template v-else-if="edge"><p>推断关系 · {{edge.reason}}</p><button type="button" class="object-link" @click="select('node',edge.from)">起点：{{byId(edge.from)}}</button><button type="button" class="object-link" @click="select('node',edge.to)">终点：{{byId(edge.to)}}</button></template>
       <dl><dt>原始 JSON Pointer</dt><dd>{{reference||'（整个漏洞对象）'}}</dd></dl>
       <Button variant="outline" size="sm" :aria-expanded="rawOpen" aria-controls="evidence-original-value" @click="rawOpen=!rawOpen">{{rawOpen?'收起对应原始数据':'查看对应原始数据'}}</Button>
       <div v-if="rawOpen" id="evidence-original-value" role="region" aria-label="对应原始证据"><CodeViewer :code="raw" language="json" :max-height="300" /></div>
      </template><p v-else class="chain-note">选择节点或关系，查看证据和来源。</p>
     </section>
    </div>
    <details v-if="graph.diagnostics.length" class="chain-diagnostics"><summary>图谱缺口与诊断（{{graph.diagnostics.length}}）</summary><ul><li v-for="(d,i) in graph.diagnostics" :key="i">{{d.message}}<code v-if="d.sourcePointer">{{d.sourcePointer}}</code></li></ul></details>
    <Button variant="ghost" size="sm" :aria-expanded="wholeRaw" aria-controls="evidence-source-finding" @click="wholeRaw=!wholeRaw">{{wholeRaw?'收起图谱来源对象':'查看图谱来源对象'}}</Button>
    <div v-if="wholeRaw" id="evidence-source-finding" role="region" aria-label="图谱来源完整漏洞对象"><CodeViewer :code="JSON.stringify(graph.sourceFinding,null,2)" language="json" :max-height="400" /></div>
   </template>
  </div>
 </section>
</template>

<style scoped>
.evidence-chain{container-type:inline-size;min-width:0;border-top:1px solid var(--border-default);border-bottom:1px solid var(--border-default);padding:var(--space-3) 0;margin-bottom:var(--space-5)}
.chain-header{display:flex;align-items:center;justify-content:space-between;gap:var(--space-2)}h3,h4,h5{font-weight:var(--font-weight-semibold);line-height:var(--line-height-body);margin:0}h3{font-size:var(--font-size-md)}h4{font-size:var(--font-size-md);overflow-wrap:anywhere}h5{font-size:var(--font-size-sm)}
.chain-note{font-size:var(--font-size-xs);color:var(--text-secondary);line-height:var(--line-height-body);margin:var(--space-2) 0;overflow-wrap:anywhere}.chain-origin,.chain-diagnostics{font-size:var(--font-size-xs);padding:var(--space-2) 0}.chain-origin summary,.chain-diagnostics summary{cursor:pointer}.chain-toolbar{display:flex;gap:var(--space-1);flex-wrap:wrap;align-items:center;padding:var(--space-2) 0;border-top:1px solid var(--border-subtle)}.toolbar-divider{height:var(--height-xs);border-left:1px solid var(--border-default);margin:0 var(--space-1)}
.graph-workspace{display:grid;min-width:0}.graph-surface{min-width:0}.chain-selection{display:flex;align-items:center;gap:var(--space-2);padding:var(--space-2) 0;font-size:var(--font-size-xs)}.chain-selection select{flex:1;min-width:0;max-width:100%;height:var(--height-md);background:var(--surface-primary);color:var(--text-primary);border:1px solid var(--border-control);border-radius:var(--radius-sm);padding:0 var(--space-2);font-size:var(--font-size-sm)}.graph-keyboard{min-width:0;border:1px solid var(--border-default)}
.evidence-inspector{min-width:0;border-top:1px solid var(--border-default);padding-top:var(--space-3);margin-top:var(--space-2);font-size:var(--font-size-sm);line-height:var(--line-height-body)}.evidence-inspector p{margin:var(--space-2) 0;overflow-wrap:anywhere}.evidence-state{font-size:var(--font-size-xs)}.inferred{color:color-mix(in srgb,var(--evidence-inferred) 75%,var(--text-primary))}.observed{color:color-mix(in srgb,var(--evidence-observed) 75%,var(--text-primary))}.verified{color:color-mix(in srgb,var(--evidence-verified) 75%,var(--text-primary))}.contradicted{color:color-mix(in srgb,var(--evidence-contradicted) 75%,var(--text-primary))}.unknown{color:color-mix(in srgb,var(--evidence-unknown) 75%,var(--text-primary))}
dl{display:grid;grid-template-columns:minmax(0,140px) minmax(0,1fr);gap:var(--space-1) var(--space-2);font-size:var(--font-size-xs);margin:var(--space-2) 0}dt{color:var(--text-secondary)}dd{margin:0;overflow-wrap:anywhere;font-family:var(--font-mono)}.code-location{font-family:var(--font-mono);overflow-wrap:anywhere}.chain-list ol,.chain-list ul,.related-evidence ul{list-style:none;padding:0;margin:0}.chain-list li{border-bottom:1px solid var(--border-subtle)}.chain-list button{display:flex;flex-wrap:wrap;gap:var(--space-1) var(--space-2);align-items:baseline;width:100%;text-align:left;padding:var(--space-2);font-size:var(--font-size-xs);background:transparent;border:0;color:var(--text-primary);cursor:pointer;overflow-wrap:anywhere}.chain-list strong{font-weight:var(--font-weight-medium);flex:1;min-width:0}.chain-list button:hover,.related-evidence button:hover{background:var(--surface-hover)}.chain-list button[aria-pressed=true]{background:var(--surface-selected)}.chain-list{max-height:calc(var(--height-table-row)*14);overflow:auto;border:1px solid var(--border-default)}
.related-evidence{margin:var(--space-3) 0}.related-evidence button,.object-link{display:block;width:100%;text-align:left;border:0;background:transparent;color:var(--accent-primary);cursor:pointer;font-size:var(--font-size-xs);padding:var(--space-1) 0;overflow-wrap:anywhere}.chain-error{font-size:var(--font-size-sm);color:var(--state-danger);padding:var(--space-2) 0}.chain-diagnostics ul{padding-left:var(--space-4);color:var(--text-secondary);line-height:var(--line-height-body)}.chain-diagnostics code{display:block;overflow-wrap:anywhere;font-family:var(--font-mono)}
@container(min-width:1000px){.graph-workspace{grid-template-columns:minmax(0,1fr) var(--inspector-width-sm);gap:var(--space-4)}.evidence-inspector{border-top:0;border-left:1px solid var(--border-default);padding:var(--space-2) 0 var(--space-2) var(--space-3);margin:0}}
@media(max-width:600px){.chain-selection{align-items:stretch;flex-direction:column}dl{grid-template-columns:minmax(0,1fr)}dd{margin-bottom:var(--space-1)}}
</style>
