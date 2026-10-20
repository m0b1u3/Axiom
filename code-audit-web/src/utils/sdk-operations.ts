import type {ArtifactRef,ExecutionRef,SDKOperationResult,SDKSessionSummary} from '../contracts';
const object=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v);
const same=(v:unknown,e:ExecutionRef)=>object(v)&&v.taskId===e.taskId&&v.attempt===e.attempt;
export function sdkOperationView(raw:unknown,e:ExecutionRef):SDKOperationResult|null {
 if(!object(raw)||raw.schemaVersion!==1||!['0.1.25','0.5.0'].includes(String(raw.sdkVersion))||!same(raw.execution,e)||!['preflight','validate'].includes(String(raw.kind))||!object(raw.cost)||typeof raw.cost.reason!=='string')return null;
 if(typeof raw.startedAt!=='string'||typeof raw.finishedAt!=='string'||!Number.isFinite(Date.parse(raw.startedAt))||!Number.isFinite(Date.parse(raw.finishedAt)))return null;
 if(raw.error!==null&&(!object(raw.error)||typeof raw.error.code!=='string'||typeof raw.error.message!=='string'))return null;
 if(raw.kind==='preflight'){
  if(raw.cost.state!=='known'||raw.cost.estimatedUsd!==0||raw.validation!==null)return null;
  if(raw.preflight!==null&&(!object(raw.preflight)||!object(raw.preflight.authentication)||raw.preflight.authentication.verified!==false))return null;
  if(raw.error===null&&raw.preflight===null)return null;
 }else{
  if(raw.cost.state!=='unknown'||raw.cost.estimatedUsd!==null||raw.preflight!==null)return null;
  if(raw.validation!==null&&(!object(raw.validation)||!['reportable','suppressed','not_applicable','deferred'].includes(String(raw.validation.disposition))||typeof raw.validation.report!=='string'||(raw.validation.threadId!==null&&typeof raw.validation.threadId!=='string')))return null;
  if(raw.error===null&&raw.validation===null)return null;
 }
 return raw as unknown as SDKOperationResult;
}
export function sdkSessionView(raw:unknown,e:ExecutionRef,artifacts:ArtifactRef[]):SDKSessionSummary|null {
 if(!object(raw)||raw.schemaVersion!==1||!same(raw.execution,e)||!['complete','incomplete'].includes(String(raw.state)))return null;
 for(const key of ['receivedCount','persistedCount','omittedCount'])if(!Number.isSafeInteger(raw[key])||(raw[key] as number)<0)return null;
 const v=raw as unknown as SDKSessionSummary;
 if(v.persistedCount+v.omittedCount!==v.receivedCount||(v.state==='complete'&&(v.omittedCount!==0||v.reason!==null))||(v.state==='incomplete'&&typeof v.reason!=='string'))return null;
 if(v.journal!==null&&(!object(v.journal)||!artifacts.some(a=>a.kind===v.journal!.kind&&a.relativePath===v.journal!.relativePath&&a.sizeBytes===v.journal!.sizeBytes&&a.sha256===v.journal!.sha256)))return null;
 return v;
}
export const dispositionLabels={reportable:'可报告',suppressed:'SDK 建议抑制',not_applicable:'不适用',deferred:'待进一步核查'} as const;
