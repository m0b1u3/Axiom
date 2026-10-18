import type {EvidenceGraph,EvidenceNode,EvidenceRelation} from '../contracts';

export const evidenceKinds:Record<EvidenceNode['kind'],string>={finding:'漏洞假设',source:'输入源',step:'路径步骤',control:'检查点',sink:'危险操作',impact:'影响',root_cause:'根因假设',validation:'SDK 复核',code_evidence:'代码证据',counter_evidence:'反证描述',assumption:'前提假设',limitation:'限制'};
export const evidenceStates:Record<EvidenceNode['state'],string>={inferred:'推断',observed:'已观察到代码',verified:'已验证',contradicted:'存在反证',unknown:'未知'};
export const evidenceRelations:Record<EvidenceRelation['kind'],string>={supports:'SDK 引用支持',contradicts:'SDK 报告反证',describes_next:'SDK 描述顺序',contains:'结果包含',constrained_by:'受约束',references:'引用'};
const object=(v:unknown):v is Record<string,unknown>=>v!==null&&typeof v==='object'&&!Array.isArray(v);
const digest=(v:unknown)=>typeof v==='string'&&/^[a-f0-9]{64}$/.test(v);
const own=(v:object,key:string)=>Object.hasOwn(v,key);
export function graphSource(source:unknown,pointer:string):{found:boolean;value?:unknown}{
 if(pointer==='')return {found:true,value:source};if(!pointer.startsWith('/')||/~(?![01])/u.test(pointer))return {found:false};
 let value:unknown=source;for(const token of pointer.slice(1).split('/')){const key=token.replaceAll('~1','/').replaceAll('~0','~');if(Array.isArray(value)){if(!/^(0|[1-9][0-9]*)$/.test(key)||!own(value,key))return {found:false};value=value[Number(key)];}else if(object(value)&&own(value,key))value=value[key];else return {found:false};}return {found:true,value};
}
/** Fail closed for this optional view; historical report readers stay separate. */
export function evidenceGraphView(value:unknown,taskId:number,attempt:number,fingerprint:string,sourceHash?:string):EvidenceGraph|null{
 if(!object(value)||value.schemaVersion!==1||value.projectorVersion!==1||!object(value.execution)||value.execution.taskId!==String(taskId)||value.execution.attempt!==attempt||!Number.isSafeInteger(attempt)||attempt<1||value.findingFingerprint!==fingerprint)return null;
 if(!['native_evidence','legacy_projection'].includes(String(value.origin))||!['complete','partial','empty'].includes(String(value.completeness))||(value.sourceContentHash!==null&&!digest(value.sourceContentHash))||(sourceHash&&value.sourceContentHash!==sourceHash)||(value.archiveSha256!==null&&!digest(value.archiveSha256))||!object(value.sourceFinding)||value.sourceFinding.fingerprint!==fingerprint)return null;
 if(!Array.isArray(value.nodes)||value.nodes.length>256||!Array.isArray(value.edges)||value.edges.length>1024||!Array.isArray(value.diagnostics)||value.diagnostics.length>256)return null;
 const ids=new Set<string>(),edges=new Set<string>();
 for(const n of value.nodes){
  if(!object(n)||typeof n.id!=='string'||!n.id||ids.has(n.id)||typeof n.kind!=='string'||!own(evidenceKinds,n.kind)||typeof n.label!=='string'||!n.label||typeof n.state!=='string'||!own(evidenceStates,n.state)||typeof n.stateReason!=='string'||!n.stateReason||typeof n.sourcePointer!=='string'||!graphSource(value.sourceFinding,n.sourcePointer).found||!['not_checked','matched','mismatch','unavailable'].includes(String(n.sourceCheck)))return null;
  if((n.code!==null&&typeof n.code!=='string')||(n.explanation!==null&&typeof n.explanation!=='string')||(n.sdkEvidenceId!==null&&typeof n.sdkEvidenceId!=='string')||(n.sourceFileSha256!==null&&!digest(n.sourceFileSha256)))return null;
  if(n.location!==null){if(!object(n.location)||typeof n.location.path!=='string'||!n.location.path||/^[A-Za-z]:|^[/\\]|\\|\x00/u.test(n.location.path)||n.location.path.split('/').some(v=>!v||v==='..'||v==='.')||(n.location.startLine!==null&&(!Number.isSafeInteger(n.location.startLine)||Number(n.location.startLine)<1))||(n.location.endLine!==null&&(!Number.isSafeInteger(n.location.endLine)||n.location.startLine===null||Number(n.location.endLine)<Number(n.location.startLine))))return null;}
  if(n.verification!==null&&(!object(n.verification)||!['runtime','human'].includes(String(n.verification.kind))||typeof n.verification.recordId!=='string'||!n.verification.recordId||!digest(n.verification.artifactSha256)||typeof n.verification.rationale!=='string'||!n.verification.rationale))return null;
  if(n.state==='verified'&&n.verification===null)return null;
  if(n.state==='observed'&&(n.kind!=='code_evidence'||n.sourceCheck!=='matched'||!digest(n.sourceFileSha256)||!object(n.location)||n.location.startLine===null||typeof n.code!=='string'))return null;
  if(n.kind==='code_evidence'){const source=graphSource(value.sourceFinding,n.sourcePointer).value;if(!object(source)||(typeof source.code==='string'&&source.code.length>0?source.code:null)!==n.code||(n.sdkEvidenceId!==null&&source.id!==n.sdkEvidenceId)||(object(n.location)&&(source.path!==n.location.path||(source.startLine??null)!==n.location.startLine||(source.endLine??null)!==n.location.endLine)))return null;}
  ids.add(n.id);
 }
 for(const e of value.edges){if(!object(e)||typeof e.id!=='string'||!e.id||edges.has(e.id)||typeof e.from!=='string'||!ids.has(e.from)||typeof e.to!=='string'||!ids.has(e.to)||typeof e.kind!=='string'||!own(evidenceRelations,e.kind)||!['inferred','unknown'].includes(String(e.state))||typeof e.reason!=='string'||!e.reason||typeof e.sourcePointer!=='string'||!graphSource(value.sourceFinding,e.sourcePointer).found)return null;if(e.kind==='supports'){const from=value.nodes.find((n:Record<string,unknown>)=>n.id===e.from);if(from.sdkEvidenceId===null||from.sdkEvidenceId!==graphSource(value.sourceFinding,e.sourcePointer).value)return null;}edges.add(e.id);}
 for(const d of value.diagnostics)if(!object(d)||typeof d.code!=='string'||typeof d.message!=='string'||(d.sourcePointer!==null&&typeof d.sourcePointer!=='string'))return null;
 return value as unknown as EvidenceGraph;
}

export type GraphView='path'|'relations';
export function graphSlice(graph:EvidenceGraph,view:GraphView,limit=48){
 const pathKinds=new Set(['source','step','control','sink','impact','assumption','limitation']);
 const candidates=view==='relations'?graph.nodes:graph.nodes.filter(n=>pathKinds.has(n.kind));
 const nodes=candidates.slice(0,Math.max(1,limit)),ids=new Set(nodes.map(n=>n.id));
 const edges=graph.edges.filter(e=>ids.has(e.from)&&ids.has(e.to)&&(view==='relations'||e.kind==='describes_next'||e.kind==='constrained_by'));
 return {nodes,edges,total:candidates.length};
}

export function graphPositions(nodes:EvidenceNode[],view:GraphView){
 const path:Record<EvidenceNode['kind'],number>={finding:0,source:0,step:1,control:1,sink:2,impact:3,root_cause:2,validation:3,code_evidence:0,counter_evidence:0,assumption:0,limitation:3};
 const relations:Record<EvidenceNode['kind'],number>={...path,finding:2,code_evidence:0,root_cause:1,validation:3,counter_evidence:0,source:0,step:1,sink:2,assumption:3,control:3,limitation:3};
 const counts=[0,0,0,0];return nodes.map(n=>{let col=(view==='path'?path:relations)[n.kind];if(view==='relations'&&n.kind==='step'&&counts[2]!<counts[1]!)col=2;return {id:n.id,x:col*220,y:(counts[col]!++)*76};});
}

export type EvidenceGraphDimensions={width:number;nodeWidth:number;nodeHeight:number;gap:number;padding:number;minHeight:number};
/** Reflow the existing semantic order into columns that fit actual node bounds.
 * Positions are pixels; the renderer must not squeeze them into a fixed lane count.
 * This changes presentation only, never the evidence objects or their relations.
 */
export function layoutEvidenceGraph(nodes:EvidenceNode[],view:GraphView,dimensions:EvidenceGraphDimensions){
 const width=Math.max(1,dimensions.width),padding=Math.min(dimensions.padding,width/4);
 const nodeWidth=Math.max(1,Math.min(dimensions.nodeWidth,width-padding*2)),nodeHeight=dimensions.nodeHeight;
 const columns=Math.max(1,Math.min(4,nodes.length,Math.floor((width-padding*2+dimensions.gap)/(nodeWidth+dimensions.gap))));
 const rows=Math.max(1,Math.ceil(nodes.length/columns));
 const height=Math.max(dimensions.minHeight,padding*2+rows*nodeHeight+(rows-1)*dimensions.gap);
 const stepX=columns===1?0:(width-padding*2-nodeWidth)/(columns-1);
 const stepY=rows===1?0:(height-padding*2-nodeHeight)/(rows-1);
 const ordered=graphPositions(nodes,view).sort((a,b)=>a.y-b.y||a.x-b.x);
 const positions=ordered.map((n,i)=>({id:n.id,x:columns===1?width/2:padding+nodeWidth/2+(i%columns)*stepX,y:rows===1?height/2:padding+nodeHeight/2+Math.floor(i/columns)*stepY}));
 const xs=positions.map(p=>p.x),ys=positions.map(p=>p.y);
 const minX=xs.length?Math.min(...xs):width/2,maxX=xs.length?Math.max(...xs):width/2;
 const minY=ys.length?Math.min(...ys):height/2,maxY=ys.length?Math.max(...ys):height/2;
 // ECharts expands a degenerate data extent by one pixel on each side. Match
 // that extent so single-column, single-row and single-node graphs stay 1:1.
 const bounds={left:minX-(minX===maxX?1:0),top:minY-(minY===maxY?1:0),width:maxX-minX||2,height:maxY-minY||2};
 return {positions,columns,width,height,nodeWidth,nodeHeight,bounds};
}
