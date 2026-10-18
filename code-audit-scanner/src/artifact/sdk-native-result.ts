import {estimateScanCost} from '@openai/codex-security';
import {validate,type ArtifactRef,type ExecutionRef,type JsonObject,type JsonValue,type SDKCostEstimate,type SDKNativeExport,type SDKNativeResult,type SDKRepositoryFinding} from '@axiom/contracts';
import {readFile,realpath} from 'node:fs/promises';
import {createHash} from 'node:crypto';
import path from 'node:path';
import type {ScanTask} from '../contract/scan-task.js';
import type {ScanResult} from '../contract/scan-result.js';

const object=(v:unknown):Record<string,unknown>|null=>v!==null&&typeof v==='object'&&!Array.isArray(v)?v as Record<string,unknown>:null;
const id=(v:unknown):string|null=>typeof v==='string'&&v.length>0&&v.length<=512?v:null;

export function sdkUsageEstimate(execution:ExecutionRef,snapshot:Record<string,unknown>|undefined):SDKCostEstimate {
  const turn=object(snapshot?.turn),model=id(turn?.model),usage=(turn?.usage??null) as JsonValue;
  const value:SDKCostEstimate={schemaVersion:1,execution,sdkVersion:'0.1.25',method:'estimateScanCost',scope:'returned_turn_usage',observedAt:new Date().toISOString(),model,status:'unavailable',reason:null,estimatedUsd:null,inputTokens:null,cachedInputTokens:null,cacheWriteInputTokens:null,outputTokens:null,usage,currency:'USD',isEstimate:true};
  if(!model){value.status='missing_model';value.reason='SDK 未报告本轮模型，无法按锁定定价估算。';}
  else if(usage===null){value.status='missing_usage';value.reason='SDK 未提供本轮 Token 用量，无法估算。';}
  else {
    const cost=estimateScanCost(model,usage);
    if(cost){Object.assign(value,{status:'estimated',estimatedUsd:cost.estimatedUsd,inputTokens:cost.inputTokens,cachedInputTokens:cost.cachedInputTokens,cacheWriteInputTokens:cost.cacheWriteInputTokens,outputTokens:cost.outputTokens});}
    else{value.status='unsupported_model_or_usage';value.reason='该模型定价或用量格式不受 SDK 0.1.25 的估算函数支持。';}
  }
  validate('SDKCostEstimate',value,execution);return value;
}

function repositoryHistory(value:unknown):SDKNativeResult['repositoryHistory'] {
  if(value===undefined||value===null)return {state:'not_reported',records:[],reason:'SDK 未返回仓库级漏洞历史；不据此认定仓库没有历史漏洞。'};
  try {
    if(!Array.isArray(value))throw new Error('not an array');
    const records:SDKRepositoryFinding[]=value.map(row=>{
      const r=object(row);if(!r)throw new Error('invalid row');
      const required=['findingId','occurrenceId','scanId','targetId','title','summary'];
      if(required.some(k=>typeof r[k]!=='string')||!['open','closed'].includes(String(r.status))||typeof r.confirmedInLatestScan!=='boolean'||!object(r.severity))throw new Error('invalid fields');
      const ids=(v:unknown):string[]|null=>{if(v===undefined||v===null)return null;if(!Array.isArray(v)||v.some(x=>typeof x!=='string'))throw new Error('invalid history IDs');return v as string[];};
      return {findingId:r.findingId as string,occurrenceId:r.occurrenceId as string,scanId:r.scanId as string,targetId:r.targetId as string,title:r.title as string,summary:r.summary as string,severity:r.severity as JsonObject,status:r.status as 'open'|'closed',confirmedInLatestScan:r.confirmedInLatestScan,knownSince:typeof r.knownSince==='string'?r.knownSince:null,knownScanIds:ids(r.knownScanIds),matchedFindingIds:ids(r.matchedFindingIds)};
    });
    return {state:'reported',reason:null,records};
  }catch{return {state:'invalid',records:[],reason:'SDK 仓库历史的结构不受支持；完整原始值仍保留在 SDK JSON。'};}
}

async function nativeExport(task:ScanTask,format:'markdown'|'sarif',snapshot:Record<string,unknown>|undefined,artifacts:ArtifactRef[]):Promise<SDKNativeExport> {
  const unavailable=(status:'not_generated'|'invalid',reason:string):SDKNativeExport=>({format,status,artifact:null,reason});
  const key=format==='markdown'?'reportPath':'sarifPath';
  const supplied=snapshot?.[key];
  if(supplied===null)return unavailable('not_generated','SDK 未生成该格式的原生产物。');
  const candidate=typeof supplied==='string'?supplied:path.join(task.outputPath,'raw/codex-security',format==='markdown'?'report.md':'exports/results.sarif');
  const root=path.resolve(task.outputPath),physicalRoot=await realpath(root);
  const absolute=path.resolve(candidate),physical=await realpath(absolute).catch(()=>null);
  const within=(relative:string)=>relative!==''&&relative!=='..'&&!relative.startsWith('../')&&!path.isAbsolute(relative);
  const relativeTo=(base:string,file:string)=>path.relative(base,file).split(path.sep).join('/');
  // The SDK canonicalizes output paths. Resolve /var -> /private/var aliases
  // before comparing; an existing file still must remain under the real root.
  let relative=physical?relativeTo(physicalRoot,physical):relativeTo(root,absolute);
  if(!physical&&!within(relative))relative=relativeTo(physicalRoot,absolute);
  if(!within(relative))return unavailable('invalid','SDK 报告路径不在本次执行产物目录内。');
  const ref=artifacts.find(a=>a.relativePath===relative);
  if(!ref)return unavailable('not_generated','本次归档没有该格式的原生产物。');
  if(ref.sizeBytes===0||ref.sizeBytes>8*1024*1024)return unavailable('invalid','原生报告为空或超过 8 MiB 核验上限；原始归档引用仍可访问。');
  try {
    const bytes=await readFile(path.join(task.outputPath,relative));
    if(bytes.length!==ref.sizeBytes||createHash('sha256').update(bytes).digest('hex')!==ref.sha256)throw new Error('artifact changed');
    const text=new TextDecoder('utf-8',{fatal:true}).decode(bytes);
    if(!text.trim()||text.includes('\0'))throw new Error('invalid text');
    if(format==='sarif'){
      const sarif=object(JSON.parse(text));
      if(sarif?.version!=='2.1.0'||!Array.isArray(sarif.runs))throw new Error('invalid SARIF');
    }
  }catch{return unavailable('invalid',format==='sarif'?'原生 SARIF 不是有效的 UTF-8 SARIF 2.1.0 文档。':'原生 Markdown 不是有效的非空 UTF-8 文本。');}
  return {format,status:'available',artifact:ref,reason:null};
}

export async function buildSDKNativeResult(task:ScanTask,result:ScanResult,artifacts:ArtifactRef[]):Promise<SDKNativeResult> {
  const execution={taskId:String(task.taskId),attempt:task.executionAttempt!},snapshot=result.sdkRaw;
  const history=repositoryHistory(snapshot?.repositoryFindings),manifest=object(snapshot?.manifest),scan=object(manifest?.scan),target=object(scan?.target);
  const native:SDKNativeResult={diagnosticJournal:artifacts.find(a=>a.relativePath==='logs/sdk-diagnostics.jsonl')??null,diagnosticJournalError:null,schemaVersion:1,execution,sdkVersion:'0.1.25',observedAt:new Date().toISOString(),scanId:id(scan?.id),targetId:id(target?.targetId)??id(target?.id),reports:{markdown:await nativeExport(task,'markdown',snapshot,artifacts),sarif:await nativeExport(task,'sarif',snapshot,artifacts)},repositoryHistory:history,usageEstimate:sdkUsageEstimate(execution,snapshot),diagnostics:result.sdkDiagnostics??{schemaVersion:1,execution,warningCount:0,observerErrorCount:0,omittedCount:0,entries:[]}};
  const expectedCount=native.diagnostics.warningCount+native.diagnostics.observerErrorCount;
  if(expectedCount||native.diagnosticJournal){
    try{
      const ref=native.diagnosticJournal;
      if(!ref||ref.sizeBytes>32*1024*1024)throw new Error('journal unavailable or over validation limit');
      const raw=await readFile(path.join(task.outputPath,ref.relativePath));
      if(raw.length!==ref.sizeBytes||createHash('sha256').update(raw).digest('hex')!==ref.sha256||raw.at(-1)!==10)throw new Error('journal changed');
      const lines=raw.toString('utf8').trimEnd().split('\n');
      if(lines.length!==expectedCount)throw new Error('journal incomplete');
      for(const [i,line] of lines.entries()){const entry=JSON.parse(line);validate('SDKDiagnostic',entry,execution);if(entry.sequence!==i+1)throw new Error('journal out of order');}
    }catch{native.diagnosticJournal=null;native.diagnosticJournalError='完整诊断日志未通过数量、身份或内容核验，或超过 32 MiB 核验上限；仍保留最近诊断和原始归档引用。';}
  }
  // Unsupported native history must not discard an otherwise valid scan.
  try{validate('SDKNativeResult',native,execution);}catch{native.repositoryHistory={state:'invalid',records:[],reason:'SDK 仓库历史超出当前合同约束；原始数据完整保留。'};validate('SDKNativeResult',native,execution);}
  return native;
}
