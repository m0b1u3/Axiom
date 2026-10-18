import {selectedSDKAuth} from "../runtime/sdk-identity.js";
import {sdkScanTarget} from '../runtime/git-target.js';
import {mkdir,writeFile,realpath} from 'node:fs/promises';
import path from 'node:path';
import {CodexSecurity,VERSION} from '@openai/codex-security';
import {decode,validate,type SDKOperationResult,type JsonObject} from '@axiom/contracts';
import type {ScanTask} from '../contract/scan-task.js';
import type {ScanResult} from '../contract/scan-result.js';
import {mapEngineError} from './error-mapping.js';
import {validateScanTask,normalizeIncludePath} from '../contract/validate-task.js';
import {sdkScanInputOptions} from '../contract/sdk-scan-input.js';
import {deepScanOptions} from '../contract/deep-policy.js';
import {buildCodexOverrides,nativeOpenAI} from '../provider/model-provider.js';
import {ensureModelCatalog,maybeStartChatShim} from './codex-security-engine.js';
import {withIsolatedScanEnvironment} from '../runtime/scan-environment.js';
import {ContractProgressSink} from '../runtime/contract-progress.js';
import {sanitizeSDKData} from '../runtime/sdk-session-journal.js';
import {publishResult,contractError} from '../artifact/result-publisher.js';

/** Only this process imports SDK implementations; Go consumes portable results. */
export async function runSDKOperation(task:ScanTask,signal:AbortSignal):Promise<ScanResult> {
  validateScanTask(task);
  const operation=task.sdkOperation;
  if(!operation||!task.executionAttempt)throw new Error('SDK operation identity missing');
  const execution={taskId:String(task.taskId),attempt:task.executionAttempt};
  validate('SDKOperation',operation,execution);
  const startedAt=new Date().toISOString(),raw=path.join(task.outputPath,'raw/codex-security');
  const sink=new ContractProgressSink(task.outputPath,execution);await sink.init();
  const result:ScanResult={taskId:task.taskId,status:'completed',startedAt,finishedAt:startedAt,engine:{name:'codex-security',version:VERSION},model:{provider:task.provider,model:task.model},findings:[],rawArtifactDir:'raw/codex-security',normalizedResultPath:'normalized/result.json'};
  if(operation.kind==='preflight')result.cost={estimatedUsd:0,inputTokens:0,outputTokens:0};
  const outcome:SDKOperationResult={schemaVersion:1,execution,kind:operation.kind,sdkVersion:'0.1.25',startedAt,finishedAt:startedAt,cost:operation.kind==='preflight'?{state:'known',estimatedUsd:0,reason:'原生预检只检查本地输入与配置，不调用模型。'}:{state:'unknown',estimatedUsd:null,reason:'SDK 0.1.25 validate 没有公开费用回调或费用上限；本次操作费用未知，不计为零。'},preflight:null,validation:null,error:null};
  let security:CodexSecurity|undefined,shim:Awaited<ReturnType<typeof maybeStartChatShim>>=null;
  try {
    await mkdir(raw,{recursive:true,mode:0o700});
    const target=await sdkScanTarget(task,signal);
    const overrides=buildCodexOverrides(task);
    if(overrides){const catalog=await ensureModelCatalog(task);if(catalog)overrides.model_catalog_json=catalog.replaceAll('\\','/');}
    // Preflight has no transport or inference; validation uses the same provider adapter.
    if(operation.kind==='validate'&&!task.mock)shim=await maybeStartChatShim(task,overrides,()=>undefined);
    security=new CodexSecurity(overrides?{codexOverrides:overrides}:{});
    await sink.operationStep(operation.kind==='preflight'?'preflight':'validation');
    await sink.activity({kind:'message',status:'running',description:operation.kind==='preflight'?'检查 SDK 目标、模型和扫描配置':'独立复核已冻结的漏洞证据；费用未知'});
    const source=await realpath(task.sourcePath),output=await realpath(task.outputPath),roots={[source]:'$SOURCE',[task.sourcePath]:'$SOURCE',[output]:'$OUTPUT',[task.outputPath]:'$OUTPUT'};
    await withIsolatedScanEnvironment(task,async()=>{
      if(operation.kind==='preflight') {
        const options=await sdkScanInputOptions(task);
        const value=await security!.preflight(task.sourcePath,{...options,...deepScanOptions(task),mode:task.scanMode,outputDir:raw,...(target!==undefined?{target}:{}),...(task.maxCostUsd!==undefined?{maxCostUsd:task.maxCostUsd}:{}),...(task.mock?{mock:true}:nativeOpenAI(task)?{auth:selectedSDKAuth(task)}:{}),signal});
        outcome.preflight=sanitizeSDKData(value,roots) as JsonObject;
        await writeFile(path.join(raw,'sdk-operation-native.json'),JSON.stringify(outcome.preflight)+'\n',{mode:0o600});
      } else if(task.mock) {
        // ValidationOptions has no mock flag. Never fall through to a paid call.
        outcome.validation={disposition:'deferred',report:'Fixtures / Mock：模拟独立复核。没有调用模型，不能作为漏洞验证证据。',threadId:null};
      } else {
        const value=await security!.validate({repositoryPath:task.sourcePath,finding:operation.finding!,outputDir:raw,...(nativeOpenAI(task)?{auth:selectedSDKAuth(task)}:{}),signal});
        await writeFile(path.join(raw,'sdk-operation-native.json'),JSON.stringify(sanitizeSDKData(value,roots))+'\n',{mode:0o600});
        outcome.validation={disposition:value.disposition,report:sanitizeSDKData(value.report) as string,threadId:value.threadId};
      }
    });
    try{decode('SDKOperationResult',JSON.stringify(outcome),execution);}catch{outcome.preflight=null;outcome.validation=null;throw new Error('SDK 原生响应超出当前操作合同或展示容量；完整响应保留在 sdk-operation-native.json。');}
  } catch(error) {
    const mapped=mapEngineError(error);result.status=signal.aborted?'canceled':mapped.status;
    const fullMessage=sanitizeSDKData(error instanceof Error?error.message:String(error)) as string;
    const message=Array.from(fullMessage||'SDK 未提供错误说明').slice(0,16000).join('');
    if(fullMessage!==message)await writeFile(path.join(raw,'sdk-operation-error.json'),JSON.stringify({message:fullMessage})+'\n',{mode:0o600});
    result.error={code:signal.aborted?'canceled':message.startsWith('SDK 原生响应')?'result_corrupt':mapped.code,message};outcome.error=contractError(result.error.code,message);
  } finally {
    try{await security?.close();}catch{await sink.warning('SDK 操作已返回；运行环境关闭失败，请保留执行产物。');}
    await shim?.close();
  }
  outcome.finishedAt=result.finishedAt=new Date().toISOString();
  validate('SDKOperationResult',outcome,execution);result.sdkOperation=outcome;
  await mkdir(raw,{recursive:true,mode:0o700});await writeFile(path.join(raw,'sdk-operation.json'),JSON.stringify(outcome)+'\n',{mode:0o600});
  await sink.finish(result.status,result.error);await sink.drain();await publishResult(task,result);
  return result;
}
