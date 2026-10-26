import type {InterimPage,InterimDocument} from '../contracts';
const object=(v:unknown):v is Record<string,any>=>!!v&&typeof v==='object'&&!Array.isArray(v);
const digest=(v:unknown)=>typeof v==='string'&&/^[a-f0-9]{64}$/.test(v);
const count=(v:unknown)=>Number.isSafeInteger(v)&&Number(v)>=0;
const timestamp=(v:unknown)=>typeof v==='string'&&Number.isFinite(Date.parse(v));
function binding(v:unknown,id:number,attempt:number,source:string){return object(v)&&v.schemaVersion===1&&v.sdkVersion==='0.5.0'&&v.execution?.taskId===String(id)&&v.execution?.attempt===attempt&&v.sourceContentHash===source;}
export function interimPage(value:unknown,id:number,attempt:number,source:string):InterimPage|null {
 if(!binding(value,id,attempt,source)||!object(value)||!['unavailable','observed','incomplete'].includes(value.state)||!count(value.revision)||!count(value.total)||!count(value.documentTotal)||!count(value.page)||value.page<1||!count(value.pageSize)||value.pageSize<1||value.pageSize>100||!count(value.documentPage)||value.documentPage<1||!Array.isArray(value.items)||value.items.length>100||!Array.isArray(value.documents)||value.documents.length>50||!Array.isArray(value.issues)||value.issues.some((v:unknown)=>typeof v!=='string')||typeof value.reason!=='string'||(value.observedAt!==null&&!timestamp(value.observedAt)))return null;
 if(value.items.some((c:any)=>!object(c)||!digest(c.id)||!digest(c.artifact)||c.assessment!=='unverified'||typeof c.pointer!=='string'||typeof c.title!=='string'||!c.title||!Array.isArray(c.evidence)||c.evidence.some((e:unknown)=>typeof e!=='string')||!count(c.omittedEvidence)||typeof c.truncated!=='boolean'||['summary','claimId','reportedStatus','reportedSeverity'].some(k=>c[k]!==null&&typeof c[k]!=='string')))return null;
 if(value.documents.some((d:any)=>!object(d)||!digest(d.sourceSha256)||!object(d.artifact)||!digest(d.artifact.sha256)||d.artifact.relativePath!=='checkpoints/interim/objects/'+d.artifact.sha256+'.json'||d.artifact.kind!=='engine_raw'||!count(d.artifact.sizeBytes)||!count(d.candidateCount)||typeof d.sourcePath!=='string'||typeof d.label!=='string'||!timestamp(d.observedAt)))return null;
 return value as InterimPage;
}
export function interimDocument(value:unknown,id:number,attempt:number,source:string):InterimDocument|null {
 if(!binding(value,id,attempt,source)||!object(value)||value.assessment!=='unverified'||value.redacted!==true||typeof value.content!=='string'||!digest(value.sourceSha256)||typeof value.sourcePath!=='string'||typeof value.label!=='string'||!timestamp(value.observedAt)||!['checkpoint','sdk_draft','worker_message'].includes(value.kind)||(value.threadId!==null&&typeof value.threadId!=='string'))return null;
 return value as InterimDocument;
}


export interface InterimExecutionSummary {
 taskId:number;scanNo:string;attempt:number;status:string;sourceContentHash:string;sourceName:string;
 projectId:number;projectName:string;startedAt:string;finishedAt:string;
 state:'observed'|'incomplete'|'unavailable'|'rejected';reason:string;
 observation:import('../contracts').InterimObservation|null;
}
export function interimExecutionPage(value:unknown):import('../api').CatalogPage<InterimExecutionSummary>|null {
 if(!object(value)||!count(value.total)||!count(value.page)||value.page<1||!count(value.pageSize)||value.pageSize<1||value.pageSize>100||!Array.isArray(value.items)||value.items.length>value.pageSize||value.items.length>value.total)return null;
 const seen=new Set<string>();
 for(const row of value.items){
  if(!object(row)||!count(row.taskId)||row.taskId<1||!count(row.attempt)||row.attempt<1||!count(row.projectId)||!digest(row.sourceContentHash)||!['observed','incomplete','unavailable','rejected'].includes(row.state)||['scanNo','status','sourceName','projectName','startedAt','finishedAt','reason'].some(k=>typeof row[k]!=='string'))return null;
  const key=row.taskId+':'+row.attempt;if(seen.has(key))return null;seen.add(key);
  if(['observed','incomplete'].includes(row.state)){
   const o=row.observation;
   if(!binding(o,row.taskId,row.attempt,row.sourceContentHash)||!object(o)||o.state!==row.state||!count(o.revision)||!count(o.documentCount)||!count(o.candidateRecords)||!timestamp(o.observedAt))return null;
  }else if(row.observation!==null)return null;
 }
 return value as import('../api').CatalogPage<InterimExecutionSummary>;
}
