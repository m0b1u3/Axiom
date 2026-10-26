import type {DeepResumeSnapshot,DeepResumeAction} from '../contracts';
const record=(v:unknown):v is Record<string,any>=>v!==null&&typeof v==='object'&&!Array.isArray(v);
const digest=(v:unknown)=>typeof v==='string'&&/^[a-f0-9]{64}$/.test(v);
const text=(v:unknown,max=2048)=>typeof v==='string'&&v.length>0&&v.length<=max;
const ref=(v:unknown,id:number)=>record(v)&&v.taskId===String(id)&&Number.isSafeInteger(v.attempt)&&v.attempt>=1;
const nullableId=(v:unknown)=>v===null||text(v,128);
const tokenKeys=['inputTokens','cachedInputTokens','cacheWriteInputTokens','outputTokens','reasoningOutputTokens','totalTokens'];
function inspection(p:unknown,id:number,attempt:number,sourceHash:string):boolean{
 if(!record(p)||p.schemaVersion!==1||p.sdkVersion!=='0.5.0'||!ref(p.execution,id)||p.execution.attempt!==attempt||!ref(p.origin,id)||p.origin.attempt>attempt||!text(p.agentId,128)||p.sourceHash!==sourceHash||!digest(p.configurationHash)||typeof p.eligible!=='boolean'||!text(p.reason)||typeof p.checkedAt!=='string'||!Number.isFinite(Date.parse(p.checkedAt))||!record(p.baseline))return false;
 if(p.baseline.estimatedUsd!==null&&(!Number.isFinite(p.baseline.estimatedUsd)||p.baseline.estimatedUsd<0))return false;
 if(p.baseline.usage!==null&&(!record(p.baseline.usage)||tokenKeys.some(k=>p.baseline.usage[k]!==null&&(!Number.isSafeInteger(p.baseline.usage[k])||p.baseline.usage[k]<0))))return false;
 if(!nullableId(p.scanId)||!nullableId(p.threadId))return false;
 return p.eligible?text(p.scanId,128)&&text(p.threadId,128)&&digest(p.proofHash):p.proofHash===null;
}
// Continuations are separate from full reexecution and result-only recovery.
// Future or foreign replies keep history readable but cannot enable mutations.
export function deepResumeView(raw:unknown,taskId:number,attempt:number,status:string,sourceHash:string):DeepResumeSnapshot|null{
 if(!record(raw)||raw.schemaVersion!==1||!ref(raw.execution,taskId)||raw.execution.attempt!==attempt||!['unsupported','idle','queued','checking','ready','blocked','pending','running','used'].includes(raw.state)||!text(raw.reason)||typeof raw.canCheck!=='boolean'||typeof raw.canResume!=='boolean'||!nullableId(raw.probeId))return null;
 const p=raw.inspection;if(p!==null&&!inspection(p,taskId,attempt,sourceHash))return null;
 if(raw.canResume&&(!['failed','timeout'].includes(status)||raw.state!=='ready'||!raw.canCheck||!text(raw.probeId,128)||!p?.eligible))return null;
 if(raw.continuation!==null){const v=raw.continuation;if(!record(v)||v.schemaVersion!==1||!ref(v.execution,taskId)||v.execution.attempt>attempt+1||!ref(v.previous,taskId)||v.previous.attempt>=v.execution.attempt||!inspection(v.inspection,taskId,v.previous.attempt,sourceHash)||!v.inspection.eligible)return null;}
 return raw as DeepResumeSnapshot;
}
export function deepResumeAction(v:DeepResumeSnapshot):DeepResumeAction|null{
 if(!v.canResume||v.state!=='ready'||!v.probeId||!v.inspection?.eligible||!v.inspection.proofHash)return null;
 return {schemaVersion:1,execution:v.execution,probeId:v.probeId,proofHash:v.inspection.proofHash,acknowledgeAdditionalCost:true};
}
