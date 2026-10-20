import {ACTIVE_SDK_VERSION} from '../runtime/sdk-version.js';
import {planComponents,normalizeComponentPlan,VERSION} from '@openai/codex-security';
import {validate,type ComponentPlanningResult,type ComponentPlanDocument} from '@axiom/contracts';
import {mkdir,writeFile,realpath} from 'node:fs/promises';
import path from 'node:path';
import type {ScanTask} from '../contract/scan-task.js';
import type {ScanResult} from '../contract/scan-result.js';
import {validateScanTask} from '../contract/validate-task.js';
import {ContractProgressSink} from '../runtime/contract-progress.js';
import {componentCoverage,frozenTreeHash} from '../runtime/component-plan.js';
import {sanitizeSDKData} from '../runtime/sdk-session-journal.js';
import {buildCodexOverrides,nativeOpenAI} from '../provider/model-provider.js';
import {ensureModelCatalog,maybeStartChatShim} from './codex-security-engine.js';
import {withIsolatedScanEnvironment} from '../runtime/scan-environment.js';
import {contractError,publishResult} from '../artifact/result-publisher.js';

export async function runComponentPlanning(task:ScanTask,signal:AbortSignal):Promise<ScanResult>{
 validateScanTask(task);const op=task.componentPlanning;if(!op||!task.executionAttempt)throw new Error('组件规划执行身份缺失');
 const execution={taskId:String(task.taskId),attempt:task.executionAttempt};validate('ComponentPlanningOperation',op,execution);
 if(op.source.snapshotId!==task.sourceSnapshotId||op.source.contentHash!==task.sourceContentHash)throw new Error('组件计划源码身份不匹配');
 const startedAt=new Date().toISOString(),raw=path.join(task.outputPath,'raw/codex-security');await mkdir(raw,{recursive:true,mode:0o700});
 const sink=new ContractProgressSink(task.outputPath,execution);await sink.init();
 const result:ScanResult={taskId:task.taskId,status:'completed',startedAt,finishedAt:startedAt,engine:{name:'codex-security',version:VERSION},model:{provider:task.provider,model:task.model},findings:[],rawArtifactDir:'raw/codex-security',normalizedResultPath:'normalized/result.json'};
 const outcome:ComponentPlanningResult={schemaVersion:1,execution,operation:op,sdkVersion:ACTIVE_SDK_VERSION,document:null,coverage:null,cost:op.kind==='normalize'?{state:'known',estimatedUsd:0}:{state:'unknown',estimatedUsd:null},native:null,error:null};
 if(op.kind==='normalize')result.cost={estimatedUsd:0,inputTokens:0,outputTokens:0};
 let shim:Awaited<ReturnType<typeof maybeStartChatShim>>=null;
 const roots:Record<string,string>={[task.sourcePath]:'$SOURCE',[task.outputPath]:'$OUTPUT'};
 try{
  roots[await realpath(task.sourcePath)]='$SOURCE';roots[await realpath(task.outputPath)]='$OUTPUT';
  await sink.operationStep('preflight');await sink.activity({kind:'message',status:'running',description:op.kind==='normalize'?'规范化组件路径并检查完整源码覆盖':'生成组件划分建议；等待人工审阅，费用未知'});
  if(await frozenTreeHash(task.sourcePath,signal)!==op.source.contentHash)throw new Error('组件计划源码已变化，不能使用此冻结快照');
  for(const key of Object.keys(process.env))if(key.toUpperCase().startsWith('GIT_'))delete process.env[key];
  Object.assign(process.env,{GIT_CONFIG_NOSYSTEM:'1',GIT_CONFIG_GLOBAL:process.platform==='win32'?'NUL':'/dev/null',GIT_CONFIG_SYSTEM:process.platform==='win32'?'NUL':'/dev/null',GIT_TERMINAL_PROMPT:'0',GIT_OPTIONAL_LOCKS:'0',GIT_NO_REPLACE_OBJECTS:'1',GIT_ALLOW_PROTOCOL:''});
  let proposed:unknown=op.document;
  if(op.kind==='suggest'){
   if(task.mock)throw new Error('模拟模式不执行自动组件规划，请手工编辑计划；没有调用模型。');
   const overrides=buildCodexOverrides(task);if(overrides){const catalog=await ensureModelCatalog(task);if(catalog)overrides.model_catalog_json=catalog.replaceAll('\\','/');}
   shim=await maybeStartChatShim(task,overrides,()=>undefined);
   proposed=await withIsolatedScanEnvironment(task,()=>planComponents(task.sourcePath,{...(overrides?{config:{codexOverrides:overrides}}:{}),environment:process.env,signal}));
  }
  const native=sanitizeSDKData(proposed,roots);await writeFile(path.join(raw,'component-plan-native.json'),JSON.stringify(native)+'\n',{mode:0o600});
  validate('ComponentPlanDocument',proposed);const normalized=await normalizeComponentPlan(task.sourcePath,proposed,signal) as ComponentPlanDocument;validate('ComponentPlanDocument',normalized);
  const coverage=await componentCoverage(task.sourcePath,normalized,signal);
  if(await frozenTreeHash(task.sourcePath,signal)!==op.source.contentHash)throw new Error('SDK 返回后源码身份发生变化，组件结果不能确认');
  outcome.native=native as ComponentPlanningResult['native'];outcome.document=normalized;outcome.coverage=coverage;
 }catch(error){
  result.status=signal.aborted?'canceled':'failed';const message=Array.from(String(sanitizeSDKData(error instanceof Error?error.message:String(error),roots))).slice(0,16000).join('')||'组件规划未返回错误说明';result.error={code:signal.aborted?'canceled':'invalid_input',message};outcome.error=contractError(result.error.code,message);outcome.document=null;outcome.coverage=null;
 }finally{await shim?.close();}
 result.finishedAt=new Date().toISOString();validate('ComponentPlanningResult',outcome,execution);result.componentPlanning=outcome;
 await writeFile(path.join(raw,'component-planning.json'),JSON.stringify(outcome)+'\n',{mode:0o600});await sink.finish(result.status,result.error);await sink.drain();await publishResult(task,result);return result;
}
