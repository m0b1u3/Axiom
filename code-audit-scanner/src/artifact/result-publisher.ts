import {createHash,randomUUID} from 'node:crypto';
import {createReadStream} from 'node:fs';
import {mkdir,open,readFile,readdir,rename,rm} from 'node:fs/promises';
import path from 'node:path';
import {COMPATIBILITY,normalizeLegacyErrorCode,validate,type ArtifactRef,type ContractError,type Finding,type JsonObject,type JsonValue,type ResultCommit,type ScanResult} from '@axiom/contracts';
import type {ScanTask} from '../contract/scan-task.js';
import type {ScanResult as LegacyResult} from '../contract/scan-result.js';
import type {Finding as LegacyFinding} from '../contract/finding.js';

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
    model:task.resolvedModel,findings:legacy.findings.map(contractFinding),
    coverage:{completeness:legacy.coverage?.completeness??'unknown',details:jsonObject(legacy.coverage??{})},artifacts,
    cost:legacy.cost?{estimatedUsd:legacy.cost.estimatedUsd??null,inputTokens:legacy.cost.inputTokens??null,outputTokens:legacy.cost.outputTokens??null,currency:'USD',isEstimate:true}:null,
    extensions:{...(legacy.sdkRaw?{sdk:jsonObject(legacy.sdkRaw)}:{}),...(legacy.lastObservedCost?{lastObservedCost:jsonObject(legacy.lastObservedCost)}:{}),adapter:{format:'codex-security-sdk',version:1,source:legacy.sdkRawSource??'unavailable'}},
  };
  if(legacy.error)result.error=contractError(legacy.error.code,legacy.error.message);
  else if(['failed','canceled','timeout'].includes(result.status))result.error=contractError(result.status==='failed'?'unknown':result.status,'SDK returned '+result.status);
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

function contractFinding(f:LegacyFinding):Finding {
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
  return out;
}

export async function publishResult(task:ScanTask,legacy:LegacyResult):Promise<ScanResult> {
  const result=contractResult(task,legacy,await artifactReferences(task.outputPath,legacy.rawArtifactDir),await installedPatchRevision());
  const bytes=Buffer.from(JSON.stringify(result)+'\n');
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
  await visit(rawRelative);return refs.sort((a,b)=>a.relativePath.localeCompare(b.relativePath,'en'));
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
