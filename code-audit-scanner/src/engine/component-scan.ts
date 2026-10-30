import {providerMcpOverrides} from '../runtime/provider-mcp.js';
import {ACTIVE_SDK_VERSION} from '../runtime/sdk-version.js';
import {sdkForTask,sdkRuntimeId} from '../runtime/sdk-runtime.js';
import {TokenBudget,TokenBudgetError,budgetMicros,tokenBudget} from '../runtime/token-budget.js';
import {sdkExecutionUsage,sumExecutionUsage,undispatchedUsage} from '../runtime/execution-token-usage.js';
import {componentMatchingEstimate} from '../runtime/component-usage.js';
import {ValidationTransportJournal} from '../proxy/turn-diagnostic.js';
import {sdkScanInputOptions} from '../contract/sdk-scan-input.js';
import {selectedSDKAuth} from "../runtime/sdk-identity.js";
import {CodexSecurity,runComponentScans,VERSION,type ComponentScanOptions,type ComponentScanResult as NativeSummary,type ScanResult as NativeResult} from '@openai/codex-security';
import {componentAllocations,componentReserved,validate,type ExecutionTokenUsage,type ComponentScanResult,type ComponentScanSnapshot,type ComponentScanReceipt,type ArtifactRef,type JsonObject,type ComponentFindingBinding} from '@axiom/contracts';
import {mkdir,writeFile,readFile,cp,realpath,rm} from 'node:fs/promises';
import {createHash} from 'node:crypto';
import path from 'node:path';
import type {ScanTask} from '../contract/scan-task.js';
import type {ScanResult} from '../contract/scan-result.js';
import {validateScanTask} from '../contract/validate-task.js';
import {componentCoverage,frozenTreeHash} from '../runtime/component-plan.js';
import {ContractProgressSink} from '../runtime/contract-progress.js';
import {SDKSessionJournal,sanitizeSDKData} from '../runtime/sdk-session-journal.js';
import {convertCodexResult} from '../converter/codex-finding-converter.js';
import {buildCodexOverrides,nativeOpenAI} from '../provider/model-provider.js';
import {ensureModelCatalog,startResponsesTransport} from './codex-security-engine.js';
import {withIsolatedScanEnvironment} from '../runtime/scan-environment.js';
import {contractError,publishResult} from '../artifact/result-publisher.js';
import {mapEngineError} from './error-mapping.js';
import {prepareAuditWorkspace} from '../runtime/audit-bootstrap.js';
import {quarantineSDKTempLinks} from '../runtime/scratch-links.js';

type Dependencies={run?:typeof runComponentScans;createSecurity?:NonNullable<ComponentScanOptions['createSecurity']>};
const MAX=Number.MAX_SAFE_INTEGER;
// Decimal rounding is upward for accounting, never a binary-float subtraction.
export function componentCostMicros(value:unknown):number|null{
 if(typeof value!=='number'||!Number.isFinite(value)||value<0)return null;
 const [mantissa,exponent='0']=value.toString().split('e'),[whole,decimal='']=mantissa!.split('.');
 const digits=BigInt(whole!+decimal),power=6+Number(exponent)-decimal.length;
 const n=power>=0?digits*10n**BigInt(power):(digits+10n**BigInt(-power)-1n)/10n**BigInt(-power);
 return n<=BigInt(MAX)?Number(n):null;
}
export function componentLimitUSD(micros:number):number{
 let value=micros/1e6;const bits=new DataView(new ArrayBuffer(8));
 while((componentCostMicros(value)??MAX)>micros){bits.setFloat64(0,value);bits.setBigUint64(0,bits.getBigUint64(0)-1n);value=bits.getFloat64(0)}
 return value;
}
const object=(v:unknown)=>JSON.parse(JSON.stringify(v)) as JsonObject;

/** SDK orchestration stays in Node. The parent lease and its frozen allowance
 * cover all selected components; no component is a new queue/budget admission. */
export async function runComponentAudit(task:ScanTask,signal:AbortSignal,deps:Dependencies={}):Promise<ScanResult>{
 validateScanTask(task);const request=task.componentScan;if(!request||!task.executionAttempt)throw new Error('组件执行身份缺失');
 const execution={taskId:String(task.taskId),attempt:task.executionAttempt};validate('ComponentScanRequest',request,execution);
 const version=request.planVersion,startedAt=new Date().toISOString(),raw=path.join(task.outputPath,'raw/codex-security'),sdkDir=path.join(raw,'components');
 await mkdir(raw,{recursive:true,mode:0o700});
 const sink=new ContractProgressSink(task.outputPath,execution);await sink.init();
 const journal=new SDKSessionJournal(execution,task.outputPath),controller=new AbortController(),combined=AbortSignal.any([signal,controller.signal]);
 const receipts:ComponentScanReceipt[]=request.selectedIndices.map((index,i)=>({index,status:'pending',allocatedMicros:componentAllocations(request)[i]!,observedMicros:0,finalMicros:null,findingCount:0,scanId:null,resultArtifact:null,error:null,phase:null,activity:null}));
 const componentUsage=new Map<number,ExecutionTokenUsage|undefined>();
 const state:ComponentScanSnapshot={schemaVersion:1,execution,planId:version.planId,version:version.version,versionSha256:version.sha256,source:version.source,phase:'preparing',observedAt:startedAt,grantMicros:request.grantMicros,reservedMicros:componentReserved(receipts),observedMicros:0,components:receipts,matching:{status:'not_started',costState:'known',reason:'尚未进行跨组件匹配；已知费用为 $0。',native:null}};
 const result:ScanResult={taskId:task.taskId,status:'completed',startedAt,finishedAt:startedAt,engine:{name:'codex-security',version:VERSION},model:{provider:task.provider,model:task.model},findings:[],rawArtifactDir:'raw/codex-security',normalizedResultPath:'normalized/result.json'};
 const roots:Record<string,string>={[task.sourcePath]:'$SOURCE',[task.outputPath]:'$OUTPUT'};
 let writes=Promise.resolve(),writeError:unknown,summary:NativeSummary|undefined,shim:Awaited<ReturnType<typeof startResponsesTransport>>=null;
 const safe=(value:unknown)=>sanitizeSDKData(value,roots);
 const queue=(work:()=>Promise<void>)=>{writes=writes.then(work).catch(error=>{writeError=error;controller.abort(error)});};
 const snapshot=(message:string,index?:number)=>{
  state.observedAt=new Date().toISOString();state.reservedMicros=componentReserved(receipts);state.observedMicros=receipts.reduce((n,r)=>n+(r.observedMicros??0),0);
  if(!Number.isSafeInteger(state.observedMicros))throw new Error('组件累计费用超过合同容量');
  const frozen=structuredClone(state);validate('ComponentScanSnapshot',frozen,execution);queue(async()=>{await sink.componentState(frozen,message,index);await sink.cost({estimatedUsd:frozen.matching.costState==='known'&&frozen.components.every(r=>r.finalMicros!==null)?frozen.observedMicros/1e6:null});});
 };
 const receipt=(local:string)=>{const index=Number(local.replace(/^component-/,''))-1,r=receipts[index];if(!r||!/^component-[1-9][0-9]*$/.test(local))throw new Error('SDK 返回了未知组件身份');return r};
 const observed=(row:ComponentScanReceipt,amount:unknown)=>{const n=componentCostMicros(amount);if(n!==null)row.observedMicros=Math.max(row.observedMicros??0,n);if((row.observedMicros??0)>row.allocatedMicros){row.error=contractError('budget_exceeded','组件费用估算超过预留额度，已停止继续派发。');controller.abort(new Error(row.error.message))}};
 try{
  roots[await realpath(task.sourcePath)]='$SOURCE';roots[await realpath(task.outputPath)]='$OUTPUT';
  if(task.mock)throw new Error('模拟模式不会执行组件审计或跨组件匹配；没有调用模型。请使用隔离 Fixtures 验收。');
  // Validate the parent's frozen connection/price binding before any inference.
  if(!tokenBudget(task))throw new Error('组件扫描缺少本次执行冻结的 Token 单价预算；未调用模型。');
  if(await frozenTreeHash(task.sourcePath,combined)!==version.source.contentHash)throw new Error('冻结源码摘要已变化，未启动组件审计');
  const coverage=await componentCoverage(task.sourcePath,version.document,combined);
  if(JSON.stringify(coverage)!==JSON.stringify(version.coverage))throw new Error('冻结计划覆盖证据与源码不匹配');
  if(task.maxCostUsd===undefined||Math.abs(task.maxCostUsd*1e6-request.grantMicros)>0.01)throw new Error('父执行预算与组件预留额度不一致');
  for(const key of Object.keys(process.env))if(key.toUpperCase().startsWith('GIT_'))delete process.env[key];
  Object.assign(process.env,{GIT_CONFIG_NOSYSTEM:'1',GIT_CONFIG_GLOBAL:process.platform==='win32'?'NUL':'/dev/null',GIT_CONFIG_SYSTEM:process.platform==='win32'?'NUL':'/dev/null',GIT_TERMINAL_PROMPT:'0',GIT_OPTIONAL_LOCKS:'0',GIT_NO_REPLACE_OBJECTS:'1',GIT_ALLOW_PROTOCOL:''});
  const overrides=await providerMcpOverrides(task,buildCodexOverrides(task));
  // Codex 0.163.0 normally ends an in-flight response when agent mail arrives,
  // before Responses terminal usage. Its native switch defers that mail until
  // the next input boundary, preserving V2 and finite Token accounting.
  if(overrides&&sdkRuntimeId(task)==='official-v2')overrides.features={...(overrides.features as JsonObject??{}),defer_mailbox_preemption:true};
  if(overrides){const catalog=await ensureModelCatalog(task);if(catalog)overrides.model_catalog_json=catalog.replaceAll('\\','/');}
  // Matching uses its own transport without the components' finite allowance.
  // Its receipts are disjoint from each component's transport below.
  const {tokenBudget:_componentBudget,...matchingTask}=task;
  shim=await startResponsesTransport(matchingTask,overrides,()=>undefined);
  await writeFile(path.join(raw,'component-request.json'),JSON.stringify(request)+'\n',{mode:0o600});
  await writeFile(path.join(raw,'component-map.json'),JSON.stringify(receipts.map((r,i)=>({sdkComponentId:`component-${i+1}`,componentIndex:r.index,versionSha256:version.sha256})))+'\n',{mode:0o600});
  state.phase='scanning';snapshot('已预留组件预算，开始执行冻结计划');
  const SDK=await sdkForTask(task);
  const createSecurity:NonNullable<ComponentScanOptions['createSecurity']>=(config)=>{
   return {close:async()=>{},run:async(repository,options)=>{
    const row=receipt(path.basename(options?.outputDir??''));
    if(combined.aborted)throw combined.reason;
    const budget=new TokenBudget({...task.tokenBudget!,limitMicros:row.allocatedMicros});
    const rowSignal=AbortSignal.any([combined,budget.signal]);
    let componentShim:Awaited<ReturnType<typeof startResponsesTransport>>=null;
    let security:Pick<CodexSecurity,'run'|'close'>|undefined,lastUsage:unknown;
    let workspace:Awaited<ReturnType<typeof prepareAuditWorkspace>>|undefined;
    const transportJournal=new ValidationTransportJournal(task.outputPath,4*1024*1024,`component-${row.index+1}-transport.jsonl`);
    row.status='running';row.finalMicros=null;snapshot(`开始组件 ${version.document.components[row.index]!.name}`,row.index);
    const source=path.join(task.outputPath,'runtime/component-sources',String(row.index),'source');
    let called=false,verified=false,nativeCompleted=false;
    try{
     await mkdir(path.dirname(source),{recursive:true,mode:0o700});await cp(repository,source,{recursive:true,errorOnExist:true,force:false,dereference:false});roots[source]='$COMPONENT_SOURCE';
     if(await frozenTreeHash(source,combined)!==version.source.contentHash)throw new Error('组件独立源码副本摘要不匹配');
     workspace=await prepareAuditWorkspace(task,path.join(task.outputPath,'runtime/component-audit',String(row.index)),{manageTemp:false,tempDir:path.join(options!.outputDir!,'artifacts/tmp')});
     const selected=structuredClone(config);
     componentShim=await startResponsesTransport(task,selected.codexOverrides,message=>queue(()=>sink.warning(message)),transportJournal.record,'native_schema',false,budget);
     security=(deps.createSecurity??((c)=>new SDK.CodexSecurity(c)))(selected);
     called=true;
     // SDK built-in prices do not cover arbitrary configured models. Never
     // forward maxCostUsd to the SDK or settle its cost/range as our fee.
     const {maxCostUsd:_nativeCostLimit,...nativeOptions}=options??{};
     const native=await security.run(source,{...nativeOptions,signal:rowSignal,
      scanPrompt:[nativeOptions.scanPrompt,workspace.prompt].filter(Boolean).join('\n\n'),
      onSessionEvent:event=>{workspace!.ensureTemp();options?.onSessionEvent?.(event);},
      onUsage:value=>{lastUsage=value;options?.onUsage?.(value);if(!componentShim){const usage=sdkExecutionUsage(execution,value,false);budget.observe(usage);if(budget.error)controller.abort(budget.error);}},
      onCost:()=>{},onWorkerEvent:value=>{queue(()=>sink.workerObserved({...value,componentIndex:row.index}));}});
     nativeCompleted=true;
     const json=safe(native.toJSON()) as Record<string,unknown>;
     const bytes=Buffer.from(JSON.stringify(json)+'\n'),relative=`raw/codex-security/component-${row.index+1}.json`;
     await writeFile(path.join(task.outputPath,relative),bytes,{mode:0o600});
     // Retain the raw result even if the SDK modified the private working copy.
     if(await frozenTreeHash(source,signal)!==version.source.contentHash)throw new Error('SDK 改写了组件源码；原始结果保留，不能绑定为冻结源码证据');
     if(typeof native.manifest.scan.id!=='string'||!native.manifest.scan.id)throw new Error('SDK 组件结果缺少 Scan ID');
     await workspace.checkEvidence(source,native.findings.findings);
     verified=true;row.scanId=native.manifest.scan.id;row.resultArtifact={kind:'engine_raw',relativePath:relative,sizeBytes:bytes.length,sha256:createHash('sha256').update(bytes).digest('hex')};
     const converted=convertCodexResult(task,native,{startedAt,finishedAt:new Date().toISOString(),rawArtifactDir:'raw/codex-security',normalizedResultPath:'normalized/result.json',sdkSnapshot:json});
     const binding:ComponentFindingBinding={schemaVersion:1,execution,planId:version.planId,version:version.version,versionSha256:version.sha256,source:version.source,componentIndex:row.index,scanId:row.scanId,resultArtifact:row.resultArtifact};validate('ComponentFindingBinding',binding,execution);
     for(const finding of converted.findings){finding.fingerprint='axiom/component/v1:'+createHash('sha256').update(`${row.index}\0${finding.fingerprint}`).digest('hex');finding.extensions={...object(finding.extensions??{}),axiomComponent:object(binding)};result.findings.push(finding)}
     row.findingCount=converted.findings.length;
     const usage=componentShim?.tokenUsage?.(execution,true)??sdkExecutionUsage(execution,lastUsage,true);
     if(!usage?.complete)throw new TokenBudgetError('budget_usage_unknown','组件没有完整 Token 用量，费用未知；已停止继续派发。');
     budget.observe(usage);if(budget.error)throw budget.error;
     const finalMicros=Number(budgetMicros(budget.pricing,usage));observed(row,finalMicros/1e6);
     // Status and final amount must change before yielding to late SDK events.
     row.finalMicros=finalMicros;row.status=converted.status==='completed'?'completed':'partial';
     await writeFile(path.join(raw,`component-${row.index+1}-token-usage.json`),JSON.stringify({schemaVersion:1,componentIndex:row.index,usage,pricingSnapshot:task.tokenBudget!.pricingSnapshot,estimatedMicros:row.finalMicros})+'\n',{mode:0o600});
     // Execution completion is separate from coverage gaps in 0.5.0.
     if(row.finalMicros===null)controller.abort(new Error('组件终值费用未知，已停止继续派发，需在 Web 核对。'));
     return native;
    }catch(error){if(error instanceof TokenBudgetError||budget.error)controller.abort(budget.error??error);row.status=signal.aborted?'canceled':'failed';row.error=contractError(signal.aborted?'canceled':error instanceof TokenBudgetError?error.code:'unknown',String(safe(error instanceof Error?error.message:String(error))).slice(0,16000));if(!called){row.finalMicros=0;row.observedMicros=0}throw error}
    finally{try{await security?.close()}finally{await componentShim?.close();componentUsage.set(row.index,called?(componentShim?.tokenUsage?.(execution,true)??sdkExecutionUsage(execution,lastUsage,nativeCompleted)):undispatchedUsage(execution));transportJournal.close();workspace?.restore();if(budget.error)controller.abort(budget.error)}
     const links=await quarantineSDKTempLinks(task.outputPath,`raw/codex-security/components/${path.basename(options!.outputDir!)}`);if(links.length)await writeFile(path.join(raw,`component-${row.index+1}-scratch-links.json`),JSON.stringify({schemaVersion:1,execution,componentIndex:row.index,links})+'\n',{mode:0o600,flag:'wx'});
     if(row.finalMicros===null&&called){await writeFile(path.join(raw,`component-${row.index+1}-token-usage.json`),JSON.stringify({schemaVersion:1,componentIndex:row.index,usage:componentShim?.tokenUsage?.(execution,true)??sdkExecutionUsage(execution,lastUsage,true)??null,pricingSnapshot:task.tokenBudget!.pricingSnapshot,estimatedMicros:null})+'\n',{mode:0o600});}if(verified)await rm(source,{recursive:true,force:true}).catch(()=>{queue(()=>sink.warning('组件临时副本未能释放；保留产物以便检查。'))});snapshot(`组件 ${version.document.components[row.index]!.name}：${row.status}`,row.index)}
   }};
  };
  const componentInput=task.sdkParameters?.parameters.componentInput;
 const componentOptions=componentInput?await sdkScanInputOptions({...task,scanInput:componentInput}):{};
 await withIsolatedScanEnvironment(task,async()=>{summary=await (deps.run??runComponentScans)({repository:task.sourcePath,outputDir:sdkDir,components:request.selectedIndices.map(i=>version.document.components[i]!),workers:request.workers,config:overrides?{codexOverrides:overrides}:{},environment:process.env,signal:combined,scanOptions:{...componentOptions,...(nativeOpenAI(task)?{auth:selectedSDKAuth(task)}:{})},createSecurity,
   onScanEvent:event=>{
    const row=receipt(event.componentId),name=version.document.components[row.index]!.name;
    if(event.type==='progress')row.phase=String(safe(event.value.phase)).slice(0,256);
    if(event.type==='activity'){row.activity=String(safe(event.value.description)).slice(0,512);queue(()=>sink.activity({kind:event.value.kind,status:event.value.status,description:`${name}：${String(safe(event.value.description))}`,id:`component-${row.index+1}:${event.value.id}`,paths:event.value.paths.map(p=>String(safe(p))),...(event.value.worker!==undefined?{worker:{index:event.value.worker,componentIndex:row.index}}:{})}))}
    if(event.type==='session'){journal.record({...event.value,event:object({...event.value.event,axiomComponentIndex:row.index})});queue(()=>sink.session({...event.value,componentIndex:row.index}))}
    if(event.type==='warning')queue(()=>sink.warning(`${name}：${String(safe(event.value))}`));
    if(event.type==='workers')queue(()=>sink.workerStatus(event.value));
    if(event.type==='usage')queue(()=>sink.usage({...event.value},row.index));
    snapshot(`${name} · ${event.type}`,row.index);
   },onDeduplicationStarted:()=>{state.phase='matching';state.matching={status:'running',costState:'unknown',reason:'SDK 原生跨组件匹配可能调用模型；0.5.0 无费用回调或上限，费用未知。',native:null};snapshot('跨组件匹配；匹配范围为本次执行所选组件，费用未知')},onComplete:value=>{summary=value}
  });});
  if(await frozenTreeHash(task.sourcePath,signal)!==version.source.contentHash)throw new Error('父执行源码已变化，无法确认结果来源');
 }catch(error){
  result.status=signal.aborted?'canceled':controller.signal.aborted?'partial':'failed';
  result.error={code:signal.aborted?'canceled':'unknown',message:String(safe(error instanceof Error?error.message:String(error))).slice(0,16000)||'组件审计未完成'};
 }finally{try{await shim?.close()}catch{queue(()=>sink.warning('模型连接关闭失败；保留当前组件结果。'))}}
 for(const row of receipts)if(row.status==='pending'||row.status==='running'){const pending=row.status==='pending';row.status=signal.aborted?'canceled':'failed';row.error??=contractError(signal.aborted?'canceled':'incomplete','组件未完成；重新执行会创建新的 SDK 会话。');if(pending){row.finalMicros=0;row.observedMicros=0}}
 if(summary?.deduplication&&state.matching.status!=='not_started'){state.matching.status=summary.deduplication.status==='completed'?'completed':'incomplete';state.matching.native=object(safe(summary.deduplication));if(summary.deduplication.status!=='completed')state.matching.reason=('跨组件匹配未完成：'+String(safe(summary.deduplication.error??'SDK 未提供原因'))+'；费用未知。').slice(0,2048)}
 else if(state.matching.status==='running')state.matching.status='incomplete';
 let native:JsonObject|null=summary?object(safe(summary)):null;
 // This file retains match/uncertainty/related provenance, including merged sources.
 if(summary?.findingsPath){try{const file=await realpath(summary.findingsPath);if(!file.startsWith((await realpath(sdkDir))+path.sep))throw new Error('匹配产物越界');const findings=JSON.parse(await readFile(file,'utf8'));native={...native,findings:safe(findings) as JsonObject}}catch{state.matching.status=state.matching.status==='not_started'?'not_started':'incomplete';result.status='partial';result.error??={code:'result_corrupt',message:'跨组件匹配产物不可读取；各组件原始结果仍可追溯。'}}}
 if(result.status==='completed'&&receipts.some(r=>r.status!=='completed')){
  result.status=receipts.some(r=>r.status==='completed')||result.findings.length?'partial':'failed';
  const failed=receipts.find(r=>r.error);
  if(failed?.error)result.error={code:mapEngineError(new Error(failed.error.message)).code,message:`组件 ${version.document.components[failed.index]!.name} 未完成：${failed.error.message}`.slice(0,16000)};
 }
 if(state.matching.status==='incomplete'){result.status=result.status==='completed'?'partial':result.status;result.error??={code:'provider_error',message:state.matching.reason};}
 if(task.tokenBudget){
  // Close first: the final in-flight response may carry the only usage receipt.
  const matchingUsage=shim?.tokenUsage?.(execution,state.matching.status==='completed')??(state.matching.status==='not_started'?undispatchedUsage(execution):undefined);
  result.componentMatchingEstimate=componentMatchingEstimate(task.tokenBudget.pricingSnapshot,matchingUsage);
  await writeFile(path.join(raw,'component-matching-token-usage.json'),JSON.stringify(result.componentMatchingEstimate)+'\n',{mode:0o600});
  const scopes=receipts.map(row=>componentUsage.has(row.index)?componentUsage.get(row.index):(row.finalMicros===0?undispatchedUsage(execution):undefined));
  result.executionUsage=sumExecutionUsage(execution,[...scopes,matchingUsage],result.status==='completed'&&!result.error);
  await sink.componentMatchingEstimate(result.componentMatchingEstimate,result.executionUsage);
 }
 state.phase='finished';snapshot('本次组件执行结束，结果与费用证据已保留');await writes;if(writeError)throw writeError;
 const outcome:ComponentScanResult={schemaVersion:1,execution,sdkVersion:ACTIVE_SDK_VERSION,request,snapshot:state,native,error:result.error?contractError(result.error.code,result.error.message):null};validate('ComponentScanResult',outcome,execution);result.componentScan=outcome;
 const known=state.matching.costState==='known'&&receipts.every(r=>r.finalMicros!==null);
 if(known)result.cost={estimatedUsd:state.observedMicros/1e6};
 const matchingAmount=result.componentMatchingEstimate?.amountMicros??0;
 const observedTotal=state.observedMicros+matchingAmount;
 result.lastObservedCost={estimatedUsd:Number.isSafeInteger(observedTotal)?observedTotal/1e6:null,scope:'component_scans_and_matching',matchingCost:result.componentMatchingEstimate?.status==='estimated'?'known':'unknown'};
 result.coverage={completeness:result.status==='completed'?'complete':'partial',planId:version.planId,version:version.version,selectedIndices:request.selectedIndices};
 result.sdkSession=journal.close();result.finishedAt=new Date().toISOString();
 await writeFile(path.join(raw,'component-scan.json'),JSON.stringify(outcome)+'\n',{mode:0o600});await sink.finish(result.status,result.error);await sink.drain();await publishResult(task,result);return result;
}
