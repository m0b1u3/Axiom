import type {ArtifactRef,ExecutionRef,SDKCostEstimate,SDKDiagnostic,SDKDiagnostics,SDKNativeResult} from '../contracts';
const object=(v:unknown):v is Record<string,unknown>=>v!==null&&typeof v==='object'&&!Array.isArray(v);
const same=(v:unknown,e:ExecutionRef)=>object(v)&&v.taskId===e.taskId&&v.attempt===e.attempt;
const count=(v:unknown)=>typeof v==='number'&&Number.isSafeInteger(v)&&v>=0;
export function sdkDiagnosticsView(raw:unknown,execution:ExecutionRef):{value:SDKDiagnostics|null;invalid:boolean}{
  const bad={value:null,invalid:true};if(raw===undefined)return {value:null,invalid:false};
  if(!object(raw)||raw.schemaVersion!==1||!same(raw.execution,execution)||!count(raw.warningCount)||!count(raw.observerErrorCount)||!count(raw.omittedCount)||!Array.isArray(raw.entries)||raw.entries.length>32)return bad;
  const v=raw as unknown as SDKDiagnostics,total=v.warningCount+v.observerErrorCount;
  if(!Number.isSafeInteger(total)||v.omittedCount+v.entries.length!==total)return bad;
  for(const [i,e] of v.entries.entries()){
    if(!object(e)||e.schemaVersion!==1||!same(e.execution,execution)||e.sequence!==v.omittedCount+i+1||!['sdk_warning','observer_error'].includes(e.source)||typeof e.message!=='string'||typeof e.kind!=='string'||typeof e.truncated!=='boolean'||typeof e.occurredAt!=='string'||!Number.isFinite(Date.parse(e.occurredAt))||!object(e.details)||(e.observer!==null&&typeof e.observer!=='string'))return bad;
    if(e.error!==null&&(!object(e.error)||typeof e.error.name!=='string'||typeof e.error.message!=='string'||(e.error.code!==null&&typeof e.error.code!=='string')))return bad;
  }
  if(v.entries.filter(e=>e.source==='sdk_warning').length>v.warningCount||v.entries.filter(e=>e.source==='observer_error').length>v.observerErrorCount)return bad;
  return {value:v,invalid:false};
}
export function sdkCostEstimateView(raw:unknown,execution:ExecutionRef):SDKCostEstimate|null{
  if(!object(raw)||raw.schemaVersion!==1||raw.sdkVersion!=='0.1.25'||!same(raw.execution,execution)||raw.method!=='estimateScanCost'||raw.scope!=='returned_turn_usage'||raw.currency!=='USD'||raw.isEstimate!==true||!['estimated','missing_model','missing_usage','unsupported_model_or_usage','unavailable'].includes(String(raw.status)))return null;
  if((raw.model!==null&&typeof raw.model!=='string')||(raw.reason!==null&&typeof raw.reason!=='string')||!Object.hasOwn(raw,'usage'))return null;
  if(['inputTokens','cachedInputTokens','cacheWriteInputTokens','outputTokens'].some(k=>raw[k]!==null&&!count(raw[k])))return null;
  if(raw.status==='estimated'?(typeof raw.estimatedUsd!=='number'||!Number.isFinite(raw.estimatedUsd)||raw.estimatedUsd<0):raw.estimatedUsd!==null)return null;
  return raw as unknown as SDKCostEstimate;
}
export function sdkNativeView(raw:unknown,execution:ExecutionRef,artifacts:ArtifactRef[]):{value:SDKNativeResult|null;invalid:boolean}{
  const bad={value:null,invalid:true};if(raw===undefined)return {value:null,invalid:false};
  if(!object(raw)||raw.schemaVersion!==1||raw.sdkVersion!=='0.1.25'||!same(raw.execution,execution)||!object(raw.reports)||!object(raw.repositoryHistory))return bad;
  if((raw.scanId!==null&&typeof raw.scanId!=='string')||(raw.targetId!==null&&typeof raw.targetId!=='string')||typeof raw.observedAt!=='string'||!Number.isFinite(Date.parse(raw.observedAt)))return bad;
  const matches=(v:unknown)=>object(v)&&artifacts.some(a=>a.kind===v.kind&&a.relativePath===v.relativePath&&a.sizeBytes===v.sizeBytes&&a.sha256===v.sha256);
  for(const format of ['markdown','sarif']){
    const value=raw.reports[format];if(!object(value)||value.format!==format||!['available','not_generated','invalid'].includes(String(value.status)))return bad;
    if(value.status==='available'?!matches(value.artifact):value.artifact!==null)return bad;
    if(value.reason!==null&&typeof value.reason!=='string')return bad;
  }
  const history=raw.repositoryHistory;
  if(!['reported','not_reported','invalid'].includes(String(history.state))||!Array.isArray(history.records)||(history.reason!==null&&typeof history.reason!=='string'))return bad;
  for(const row of history.records){
    if(!object(row)||['findingId','occurrenceId','scanId','targetId','title','summary'].some(k=>typeof row[k]!=='string')||!object(row.severity)||!['open','closed'].includes(String(row.status))||typeof row.confirmedInLatestScan!=='boolean'||(row.knownSince!==null&&typeof row.knownSince!=='string'))return bad;
    if(['knownScanIds','matchedFindingIds'].some(k=>row[k]!==null&&(!Array.isArray(row[k])||(row[k] as unknown[]).some(v=>typeof v!=='string'))))return bad;
  }
  if(!sdkCostEstimateView(raw.usageEstimate,execution)||!sdkDiagnosticsView(raw.diagnostics,execution).value)return bad;
  if(raw.diagnosticJournal!==null&&!matches(raw.diagnosticJournal))return bad;
  if(raw.diagnosticJournalError!==null&&typeof raw.diagnosticJournalError!=='string')return bad;
  return {value:raw as unknown as SDKNativeResult,invalid:false};
}
export function diagnosticLabel(value:SDKDiagnostic):string{
  return value.source==='observer_error'?'观察器失败':value.kind==='target_changed'?'扫描目标发生变化':'SDK 警告';
}
