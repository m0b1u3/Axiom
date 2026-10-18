import {gitEvidenceBinding,assertGitSDKResult} from '../runtime/git-target.js';
import {createHash,randomUUID} from 'node:crypto';
import {createReadStream} from 'node:fs';
import {mkdir,open,readFile,readdir,rename,rm,lstat} from 'node:fs/promises';
import path from 'node:path';
import {COMPATIBILITY,normalizeLegacyErrorCode,validate,type ArtifactRef,type ContractError,type Finding,type JsonObject,type JsonValue,type ResultCommit,type ScanResult} from '@axiom/contracts';
import type {ScanTask} from '../contract/scan-task.js';
import type {ScanResult as LegacyResult} from '../contract/scan-result.js';
import type {Finding as LegacyFinding} from '../contract/finding.js';
import {buildSDKNativeResult} from './sdk-native-result.js';
import {findingEvidenceJSON} from '../converter/finding-evidence.js';

export const RESULT_FILE='normalized/result.json';
export const COMMIT_FILE='normalized/result.commit.json';
export class InvalidSDKResultError extends Error {}

/** The core result is portable; the complete SDK JSON remains in an explicit
 * extension. Consumers never infer terminal status from that native payload. */
export function contractResult(task:ScanTask,legacy:LegacyResult,artifacts:ArtifactRef[]=[],patchRevision:string|null=null):ScanResult {
  if(task.resultSchemaVersion!==1 || !task.resolvedModel || !task.executionAttempt)throw new Error('Versioned result requires a negotiated execution and model snapshot');
  const result:ScanResult={
    schemaVersion:1,execution:{taskId:String(task.taskId),attempt:task.executionAttempt},status:legacy.status,
    startedAt:legacy.startedAt,finishedAt:legacy.finishedAt,
    engine:{name:legacy.engine.name,version:legacy.engine.version||null,pluginVersion:legacy.engine.pluginVersion??null,patchRevision},
    model:task.resolvedModel,findings:legacy.findings.map(f=>contractFinding(f,task)),
    coverage:{completeness:legacy.coverage?.completeness??'unknown',details:jsonObject(legacy.coverage??{})},artifacts,
    cost:legacy.cost?{estimatedUsd:legacy.cost.estimatedUsd??null,inputTokens:legacy.cost.inputTokens??null,outputTokens:legacy.cost.outputTokens??null,currency:'USD',isEstimate:true}:null,
    extensions:{...(legacy.sdkRaw?{sdk:jsonObject(legacy.sdkRaw)}:{}),...(legacy.lastObservedCost?{lastObservedCost:jsonObject(legacy.lastObservedCost)}:{}),adapter:{format:'codex-security-sdk',version:1,source:legacy.sdkRawSource??'unavailable'}},
  };
  if(legacy.error)result.error=contractError(legacy.error.code,legacy.error.message);
  else if(['failed','canceled','timeout'].includes(result.status))result.error=contractError(result.status==='failed'?'unknown':result.status,'SDK returned '+result.status);
  // Enrichment is optional even before the first core-contract validation.
  if(task.sdkIdentity)result.extensions={...result.extensions,sdkIdentity:jsonObject(task.sdkIdentity)};
 const git=gitEvidenceBinding(task);if(git){if(!task.sdkOperation&&(["completed","partial"].includes(result.status)||result.findings.length>0))assertGitSDKResult(task,legacy.sdkRaw);result.extensions={...result.extensions,gitDiff:jsonObject(git)};}
  if(Buffer.byteLength(JSON.stringify(result))>COMPATIBILITY.limits.documentBytes-65536){for(const f of result.findings){if(f.extensions?.axiomEvidence){delete f.extensions.axiomEvidence;f.extensions.axiomEvidenceUnavailable='图谱投影超过结果容量；完整原始证据仍可读取。';}}}
  validate('ScanResult',result,result.execution);
  return result;
}

export function contractError(code:string,message:string):ContractError {
  const normalized=normalizeLegacyErrorCode(code).code;
  return {code:normalized,origin:'engine',message,retryable:['rate_limit','provider_unavailable'].includes(normalized)};
}

function jsonObject(value:unknown):JsonObject {
  if(!value || typeof value!=='object' || Array.isArray(value))throw new Error('Expected a JSON object from the SDK');
  return JSON.parse(JSON.stringify(value)) as JsonObject;
}

function contractFinding(f:LegacyFinding,task:ScanTask):Finding {
  const {confidence,confidenceRationale,extensions,...fields}=f;
  const out={...fields} as Finding;
  const native=f.raw as {confidence?:{level?:string;rationale?:string;value?:number}}|undefined;
  const c=native?.confidence;
  if(c || confidenceRationale!==undefined){
    const level=['high','medium','low'].includes(c?.level??'')?c!.level as 'high'|'medium'|'low':'unknown';
    // Numeric confidence is retained only when reported, never synthesized from a level.
    out.confidence={level,rationale:c?.rationale??confidenceRationale??null,...(typeof c?.value==='number'?{value:c.value}:{})};
  }
  if(extensions && typeof extensions==='object' && !Array.isArray(extensions))out.extensions=jsonObject(extensions);
  const git=gitEvidenceBinding(task);if(git)out.extensions={...out.extensions,axiomGitTarget:jsonObject(git)};
  try{out.extensions={...out.extensions,axiomEvidence:findingEvidenceJSON(out,task)};}
  catch{out.extensions={...out.extensions,axiomEvidenceUnavailable:'证据图谱映射不受支持；完整 SDK 对象仍保留。'};}
  return out;
}

export async function publishResult(task:ScanTask,legacy:LegacyResult):Promise<ScanResult> {
  const result=contractResult(task,legacy,await artifactReferences(task.outputPath,legacy.rawArtifactDir),await installedPatchRevision());
  if(legacy.sdkSession){const summary={...legacy.sdkSession,journal:result.artifacts.find(a=>a.relativePath==='logs/sdk-session-events.jsonl')??null};if(summary.persistedCount&&!summary.journal){summary.state='incomplete';summary.reason='已记录的会话日志未随本次结果归档，完整性无法确认。';}validate('SDKSessionSummary',summary,result.execution);result.extensions={...result.extensions,sdkSession:jsonObject(summary)};}
  if(legacy.sdkOperation){validate('SDKOperationResult',legacy.sdkOperation,result.execution);result.extensions={...result.extensions,sdkOperation:jsonObject(legacy.sdkOperation)};}
  if(legacy.componentScan){validate('ComponentScanResult',legacy.componentScan,result.execution);result.extensions={...result.extensions,componentScan:jsonObject(legacy.componentScan)};}
  if(legacy.componentPlanning){validate('ComponentPlanningResult',legacy.componentPlanning,result.execution);result.extensions={...result.extensions,componentPlanning:jsonObject(legacy.componentPlanning)};}
  const native=await buildSDKNativeResult(task,legacy,result.artifacts);
  const budget=COMPATIBILITY.limits.documentBytes-65536;
  // Optional evidence projections must never crowd out the source report.
  if(Buffer.byteLength(JSON.stringify(result))>budget){for(const f of result.findings){if(f.extensions?.axiomEvidence){delete f.extensions.axiomEvidence;f.extensions.axiomEvidenceUnavailable='图谱投影超过结果容量；完整原始证据仍可读取。';}}}
  const baseBytes=Buffer.byteLength(JSON.stringify(result)+'\n');
  const serialize=()=>{if(!legacy.sdkOperation&&!legacy.componentPlanning&&!legacy.componentScan)result.extensions={...result.extensions,sdkNative:jsonObject(native)};return Buffer.from(JSON.stringify(result)+'\n');};
  let bytes=serialize();
  // The original SDK JSON is the evidence. Optional display projections may
  // not turn an otherwise valid full result into an oversized failed scan.
  if(bytes.length>budget&&native.repositoryHistory.records.length){
    native.repositoryHistory={state:'invalid',records:[],reason:'结构化历史超过结果展示容量；完整原生历史仍保留在 SDK JSON。'};bytes=serialize();
  }
  if(bytes.length>budget&&native.usageEstimate.usage!==null){
    native.usageEstimate.usage=null;native.usageEstimate.reason='补充用量依据超过展示容量；完整原始用量仍保留在 SDK JSON。';bytes=serialize();
  }
  while(bytes.length>budget&&native.diagnostics.entries.length){native.diagnostics.entries.shift();native.diagnostics.omittedCount++;bytes=serialize();}
  if(bytes.length>budget&&baseBytes<=budget){
    delete result.extensions!.sdkNative;bytes=Buffer.from(JSON.stringify(result)+'\n');
    process.stderr.write('scan-warning optional SDK display metadata exceeds result capacity; full SDK JSON retained\n');
  }
  // Reserve space for the completion envelope. Oversized data stays on disk as
  // SDK artifacts and fails explicitly; no field is silently truncated.
  if(bytes.length>COMPATIBILITY.limits.documentBytes-65536)throw new InvalidSDKResultError('Full scan result exceeds the completion document size limit');
  const marker:ResultCommit={schemaVersion:1,execution:result.execution,resultSchemaVersion:1,relativePath:RESULT_FILE,sizeBytes:bytes.length,sha256:createHash('sha256').update(bytes).digest('hex')};
  validate('ResultCommit',marker,result.execution);
  await atomicWrite(path.join(task.outputPath,RESULT_FILE),bytes);
  await atomicWrite(path.join(task.outputPath,COMMIT_FILE),Buffer.from(JSON.stringify(marker)+'\n'));
  return result;
}

async function atomicWrite(file:string,bytes:Buffer):Promise<void> {
  await mkdir(path.dirname(file),{recursive:true,mode:0o700});
  const temp=file+'.'+process.pid+'.'+randomUUID()+'.tmp';
  try {
    const handle=await open(temp,'wx',0o600);
    try {await handle.writeFile(bytes);await handle.sync();}finally{await handle.close();}
    await rename(temp,file);
    if(process.platform!=='win32'){
      const directory=await open(path.dirname(file),'r');
      try{await directory.sync();}finally{await directory.close();}
    }
  }finally{await rm(temp,{force:true});}
}

async function artifactReferences(output:string,rawRelative:string):Promise<ArtifactRef[]> {
  const refs:ArtifactRef[]=[];
  async function visit(relative:string):Promise<void>{
    let entries;
    try{entries=await readdir(path.join(output,relative),{withFileTypes:true});}catch(error){if((error as NodeJS.ErrnoException).code==='ENOENT')return;throw error;}
    for(const entry of entries){
      const next=path.posix.join(relative.replaceAll('\\','/'),entry.name);
      if(entry.isSymbolicLink())throw new Error('SDK artifact must not be a symbolic link');
      if(entry.isDirectory()){await visit(next);continue;}
      if(!entry.isFile())throw new Error('SDK artifact must be a regular file');
      const hash=createHash('sha256');let size=0;
      for await(const part of createReadStream(path.join(output,next))){size+=part.length;hash.update(part);}
      const kind=entry.name==='coverage.json'?'coverage':entry.name==='report.md'?'report':entry.name.endsWith('.sarif')?'sarif':'engine_raw';
      refs.push({kind,relativePath:next,sizeBytes:size,sha256:hash.digest('hex')});
    }
  }
  await visit(rawRelative);
  for(const journal of ['logs/sdk-diagnostics.jsonl','logs/sdk-session-events.jsonl']){
  const info=await lstat(path.join(output,journal)).catch(()=>null);
  if(info){
    if(!info.isFile()||info.isSymbolicLink()){if(journal==='logs/sdk-session-events.jsonl'){process.stderr.write('scan-warning session journal not archived: not a regular file\n');continue;}throw new Error('SDK diagnostic journal must be a regular file');}
    const hash=createHash('sha256');let size=0;
    for await(const part of createReadStream(path.join(output,journal))){size+=part.length;hash.update(part);}
    refs.push({kind:'engine_raw',relativePath:journal,sizeBytes:size,sha256:hash.digest('hex')});
  }
  }
  return refs.sort((a,b)=>a.relativePath.localeCompare(b.relativePath,'en'));
}

let patchRevision:Promise<string|null>|undefined;
function installedPatchRevision():Promise<string|null>{
  return patchRevision??= (async()=>{
    try{
      const raw=await readFile(new URL('../../../deploy/sdk/manifest.json',import.meta.url));
      const manifest=JSON.parse(raw.toString()) as {patches:Array<{path:string;after:string}>};
      for(const patch of manifest.patches){
        const bytes=await readFile(new URL('../../node_modules/'+patch.path,import.meta.url));
        if(createHash('sha256').update(bytes).digest('hex')!==patch.after)return null;
      }
      return 'sha256:'+createHash('sha256').update(raw).digest('hex');
    }catch{return null;}
  })();
}
