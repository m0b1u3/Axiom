import {ACTIVE_SDK_VERSION,type SupportedSDKVersion} from '../runtime/sdk-version.js';
import {createHash} from 'node:crypto';
import {validate,type Finding,type FindingEvidence,type EvidenceNode,type EvidenceRelation,type JsonValue} from '@axiom/contracts';
import type {ScanTask} from '../contract/scan-task.js';

const object=(v:unknown):v is Record<string,unknown>=>v!==null&&typeof v==='object'&&!Array.isArray(v);
const text=(v:unknown):string|null=>typeof v==='string'&&v.length>0?v:null;
const pointerKey=(v:string)=>v.replaceAll('~','~0').replaceAll('/','~1');
const relative=(v:unknown):v is string=>typeof v==='string'&&v.length>0&&v.length<=4096&&!/^[A-Za-z]:|^[/\\]|\\|\x00/u.test(v)&&!v.split('/').some(p=>p==='..'||p==='.'||p==='');

/** Versioned adapter for SDK 0.1.25 and 0.5.0. Text and SDK review statuses are
 * claims, never runtime observations. Only explicit evidence IDs form links. */
export function buildFindingEvidence(f:Finding,task:ScanTask,sdkVersion:SupportedSDKVersion=ACTIVE_SDK_VERSION):FindingEvidence {
 const raw=object(f.raw)?f.raw:null;
 const execution={taskId:String(task.taskId),attempt:task.executionAttempt!};
 const out:FindingEvidence={schemaVersion:1,execution,findingFingerprint:f.fingerprint,sourceContentHash:task.sourceContentHash??null,sdkVersion,adapterVersion:sdkVersion==='0.1.25'?1:2,completeness:'complete',nodes:[],edges:[],diagnostics:[]};
 const id=(kind:string,pointer:string)=>createHash('sha256').update(JSON.stringify([execution,f.fingerprint,kind,pointer])).digest('hex');
 function diagnostic(code:string,message:string,pointer:string|null){out.completeness='partial';if(out.diagnostics.length<255)out.diagnostics.push({code,message,sourcePointer:pointer});else if(out.diagnostics.length===255)out.diagnostics.push({code:'diagnostic_limit',message:'更多映射诊断未展开；完整 SDK 对象仍可读取。',sourcePointer:null});}
 function node(kind:EvidenceNode['kind'],label:string|null,pointer:string,extra:Partial<EvidenceNode>={}):EvidenceNode|null{
  if(!label)return null;
  if(out.nodes.length>=256||label.length>16384||(extra.code?.length??0)>524288||(extra.explanation?.length??0)>65536){diagnostic('projection_limit','证据超过图谱展示容量；原始对象未裁剪。',pointer);return null;}
  const value:EvidenceNode={id:id(kind,pointer),kind,label,state:'inferred',stateReason:'SDK 报告或推断；未据此确认实际执行或漏洞复现。',sourcePointer:pointer,sdkEvidenceId:null,location:null,code:null,explanation:null,sourceCheck:'not_checked',sourceFileSha256:null,verification:null,...extra};out.nodes.push(value);return value;
 }
 function edge(from:EvidenceNode|null,to:EvidenceNode|null,kind:EvidenceRelation['kind'],pointer:string,reason:string){
  if(!from||!to)return;
  const key=id(kind,JSON.stringify([from.id,to.id,pointer]));if(out.edges.some(e=>e.id===key))return;
  if(out.edges.length>=1024){diagnostic('projection_limit','更多关系保留在原始对象中，图谱未全部展开。',pointer);return;}
  out.edges.push({id:key,from:from.id,to:to.id,kind,state:'inferred',sourcePointer:pointer,reason});
 }
 const root=node('finding',f.title,'/title')!;
 const byEvidence=new Map<string,EvidenceNode[]>();
 const codeKey=raw&&Array.isArray(raw.codeEvidence)?'codeEvidence':raw&&Array.isArray(raw.code_evidence)?'code_evidence':null;
 const snippets=codeKey?raw![codeKey] as unknown[]:f.codeEvidence??[];
 for(const [index,item] of snippets.entries()){
  if(!object(item)){diagnostic('invalid_evidence','SDK 证据条目不是对象，原文仍保留。',`/raw/${codeKey}/${index}`);continue;}
  const pointer=codeKey?`/raw/${codeKey}/${index}`:`/codeEvidence/${index}`;
  let location:EvidenceNode['location']=null;
  const start=Number.isSafeInteger(item.startLine)&&Number(item.startLine)>0?Number(item.startLine):null;
  const end=Number.isSafeInteger(item.endLine)&&Number(item.endLine)>0?Number(item.endLine):null;
  if(relative(item.path)&&(end===null||(start!==null&&end>=start)))location={path:item.path,startLine:start,endLine:end};
  else if(item.path!==undefined)diagnostic('invalid_location','证据位置无法安全映射，保留原始位置。',pointer);
  const evidenceId=text(item.id);
  const n=node('code_evidence',text(item.label)??(location?`${location.path}:${start??'?'}`:'SDK 代码证据'),pointer,{sdkEvidenceId:evidenceId?.length&&evidenceId.length<=512?evidenceId:null,location,code:text(item.code),explanation:text(item.explanation)});
  if(n&&evidenceId&&evidenceId.length<=512){const list=byEvidence.get(evidenceId)??[];list.push(n);byEvidence.set(evidenceId,list);}
  else if(evidenceId&&evidenceId.length>512)diagnostic('invalid_evidence_id','SDK 证据 ID 超过可映射长度，未补造引用。',pointer);
  edge(root,n,'contains',pointer,'漏洞结果包含此证据条目；不推定该条目证明整个漏洞。');
 }
 for(const [key,list] of byEvidence)if(list.length>1)diagnostic('duplicate_evidence_id','SDK 证据 ID 重复，引用未自动选择任一条目。',list[0]!.sourcePointer);
 function refs(value:Record<string,unknown>,pointer:string,target:EvidenceNode|null){
  for(const key of ['evidenceRefs','evidence_refs']){
   if(value[key]===undefined)continue;
   const list=value[key];if(!Array.isArray(list)){diagnostic('invalid_reference','证据引用不是数组，未推断关联。',pointer+'/'+key);continue;}
   for(const [index,reference] of list.entries()){
    const origin=pointer+'/'+key+'/'+index;const matches=typeof reference==='string'?byEvidence.get(reference):undefined;
    if(matches?.length===1)edge(matches[0]!,target,'supports',origin,'SDK 通过证据 ID 明确引用；该支持关系仍属于 SDK 分析结论。');
    else diagnostic(matches&&matches.length>1?'ambiguous_reference':'missing_reference','证据引用缺失或不唯一，未补造连线。',origin);
   }
  }
 }
 function claim(value:unknown,kind:EvidenceNode['kind'],pointer:string,parent:EvidenceNode=root):EvidenceNode|null{
  const label=text(value)??(object(value)?text(value.summary)??text(value.rationale)??text(value.why):null);
  const n=node(kind,label,pointer,object(value)?{code:text(value.code)}:{});edge(parent,n,'contains',pointer,'SDK 结果中的分析内容。');
  if(object(value)){
   if(n)refs(value,pointer,n);
   else if(value.evidenceRefs!==undefined||value.evidence_refs!==undefined)diagnostic('unmapped_claim','SDK 分析对象没有可映射的结论文字；保留引用但未将它附到其他结论。',pointer);
  }
  return n;
 }
 function list(value:unknown,kind:EvidenceNode['kind'],pointer:string,parent:EvidenceNode,relation:EvidenceRelation['kind']='contains'){
  if(!Array.isArray(value))return [];
  const nodes:EvidenceNode[]=[];
  for(const [index,v] of value.entries()){const p=pointer+'/'+index;const n=node(kind,text(v),p);if(!n){if(v!==undefined)diagnostic('unmapped_step','步骤无法映射为文本，完整内容仍可读取。',p);continue;}nodes.push(n);edge(relation==='contradicts'?n:parent,relation==='contradicts'?parent:n,relation,p,relation==='contradicts'?'SDK 报告的反证；尚未独立验证。':'SDK 明确列出的内容。');}
  return nodes;
 }
 if(raw){
  for(const key of ['rootCause','root_cause'])if(raw[key]!==undefined)claim(raw[key],'root_cause','/raw/'+key);
  const path=raw.attackPath??raw.attack_path;const pathKey=raw.attackPath!==undefined?'attackPath':'attack_path';const pp='/raw/'+pathKey;
  if(typeof path==='string'){claim(path,'step',pp);diagnostic('unstructured_path','SDK 只提供路径文字，未将其解释为实际调用或传播链。',pp);}
  else if(object(path)){
   const pnode=claim(path,'step',pp)??root;
   for(const key of ['entrypoint','source','sink','impact','likelihood','reachability'])if(path[key]!==undefined)claim(path[key],key==='source'||key==='entrypoint'?'source':key==='sink'?'sink':key==='impact'?'impact':'step',pp+'/'+key,pnode);
   for(const key of ['dataFlow','data_flow','dataflow']){
    const flow=path[key];if(flow===undefined)continue;const fp=pp+'/'+key;
    if(typeof flow==='string'){claim(flow,'step',fp,pnode);diagnostic('unstructured_path','数据流为文字描述，未生成真实传播关系。',fp);}
    else if(object(flow)){
     const flowNode=claim(flow,'step',fp,pnode)??pnode;
     const source=claim(flow.source,'source',fp+'/source',flowNode),sink=claim(flow.sink,'sink',fp+'/sink',flowNode);
     const steps=list(flow.transformations,'step',fp+'/transformations',flowNode);
     const sequence=[source,...steps,sink].filter((n):n is EvidenceNode=>!!n);
     for(let i=1;i<sequence.length;i++)edge(sequence[i-1]!,sequence[i]!,'describes_next',fp,'SDK 描述的数据流次序；未验证真实调用或传播。');
     claim(flow.outcome,'impact',fp+'/outcome',flowNode);
    }
   }
   const steps=list(path.steps,'step',pp+'/steps',pnode);
   for(let i=1;i<steps.length;i++)edge(steps[i-1]!,steps[i]!,'describes_next',pp+'/steps','SDK 列出的步骤顺序；不代表真实调用栈。');
   for(const key of ['preconditions','assumptions','controls','limitations','blindspots'])list(path[key],key==='controls'?'control':key==='preconditions'||key==='assumptions'?'assumption':'limitation',pp+'/'+key,pnode);
   if(object(path.reachability)){
    const reach=path.reachability,rp=pp+'/reachability';
    for(const key of ['attacker','entrypoint','source','sink','outcome'])if(reach[key]!==undefined)claim(reach[key],key==='source'||key==='entrypoint'?'source':key==='sink'?'sink':key==='outcome'?'impact':'assumption',rp+'/'+key,pnode);
    list(reach.preconditions,'assumption',rp+'/preconditions',pnode);
   }
  }
  if(object(raw.validation)){
   const v=raw.validation;const n=node('validation',text(v.summary)??('SDK 复核：'+(text(v.status)??text(v.disposition)??text(v.result)??'结论未结构化')),'/raw/validation');
   edge(root,n,'contains','/raw/validation','已记录 SDK 复核输出；不自动视为独立复现。');refs(v,'/raw/validation',n);
   list(v.counterEvidence,'counter_evidence','/raw/validation/counterEvidence',root,'contradicts');list(v.limitations,'limitation','/raw/validation/limitations',n??root);
  }
 } else {
  diagnostic('raw_unavailable','结果没有 SDK 原始对象，仅保留明确的归一化证据关联。',null);
 }
 if(out.nodes.length===1&&out.diagnostics.length===0)out.completeness='empty';
 validate('FindingEvidence',out,execution);return out;
}

export const findingEvidenceJSON=(f:Finding,task:ScanTask):JsonValue=>JSON.parse(JSON.stringify(buildFindingEvidence(f,task))) as JsonValue;
