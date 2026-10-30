/** Exact SDK 0.5.0 internal continuation bridge. Never migrate native state. */
import {createHash,randomUUID} from 'node:crypto';
import {DatabaseSync} from 'node:sqlite';
import {lstat,mkdir,readFile,readdir,realpath,writeFile,rm,cp} from 'node:fs/promises';
import {createRequire} from 'node:module';
import {execFile} from 'node:child_process';
import {promisify,isDeepStrictEqual} from 'node:util';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
import {decode,validate,type DeepResumeContext,type DeepResumeAssignment,type DeepResumeProbeClaim,type DeepResumeInspection,type DeepResumeUsage,type DeepResumeBaseline} from '@axiom/contracts';
import type {ScanOptions,CodexSecurity} from '@openai/codex-security';
import type {ScanTask} from '../contract/scan-task.js';
import type {SDKRuntimeBinding} from './sdk-runtime.js';
const exec=promisify(execFile),hash=(v:string|Uint8Array)=>createHash('sha256').update(v).digest('hex');
const sdkRoot=()=>path.resolve(path.dirname(createRequire(import.meta.url).resolve('@openai/codex-security')),'..');
const part=async(name:string)=>import(pathToFileURL(path.join(sdkRoot(),'dist',name+'.js')).href);
const usageKeys=['input_tokens','cached_input_tokens','cache_write_input_tokens','output_tokens','reasoning_output_tokens','total_tokens'] as const;
const tokens=['inputTokens','cachedInputTokens','cacheWriteInputTokens','outputTokens','reasoningOutputTokens','totalTokens'] as const;
const same=isDeepStrictEqual;
function nativePath(value:string){const v=path.resolve(value);return process.platform==='darwin'&&/^\/(var|tmp)(\/|$)/.test(v)?'/private'+v:v;}
export const DEEP_RESUME_MANIFEST='runtime/deep-resume.json';
interface Manifest {schemaVersion:1;context:DeepResumeContext;sourceHash:string;identity:unknown;stateRoot:string;rawDir:string;sourcePath:string;model:string;runtime?:SDKRuntimeBinding;}
export function originOutput(output:string,attempt:number):string {
 if(!Number.isSafeInteger(attempt)||attempt<1||!/^attempt-[1-9]\d*$/.test(path.basename(output)))throw new Error('续扫执行目录身份无效');
 return path.join(path.dirname(nativePath(output)),'attempt-'+attempt);
}
async function regular(file:string,max=1024*1024):Promise<Buffer>{const i=await lstat(file);if(!i.isFile()||i.isSymbolicLink()||i.size>max||await realpath(file)!==nativePath(file))throw new Error('续扫文件缺失、超限或包含链接');return readFile(file);}
async function canonical(directory:string){if(await realpath(directory)!==nativePath(directory))throw new Error('续扫目录身份已变化');}
export async function acquireDeepGuard(output:string):Promise<()=>void>{
 await mkdir(path.join(output,'runtime'),{recursive:true,mode:0o700});await canonical(path.join(output,'runtime'));
 const name=path.join(output,'runtime','.deep-resume-guard.sqlite');const stat=await lstat(name).catch(()=>null);if(stat&&(!stat.isFile()||stat.isSymbolicLink()))throw new Error('续扫锁必须是普通文件');
 const db=new DatabaseSync(name);try{db.exec('PRAGMA busy_timeout=0; BEGIN IMMEDIATE');}catch{db.close();throw new Error('原 Deep 执行或另一项续扫仍占用工作目录');}
 return ()=>{try{db.exec('ROLLBACK')}finally{db.close()}};
}
export async function certifyDeepSDK(){const {verifyRecoveryBaseline}=await import(new URL('../../../deploy/sdk/recovery-check.mjs',import.meta.url).href);await verifyRecoveryBaseline();}
export async function saveDeepManifest(task:ScanTask):Promise<void>{
 const context=task.deepResumeContext;if(!context||task.scanMode!=='deep'||task.deepResume)return;
 validate('DeepResumeContext',context,{taskId:String(task.taskId),attempt:task.executionAttempt!});await certifyDeepSDK();
 if(task.componentScan||task.gitTarget||task.sdkOperation||task.componentPlanning||task.scanInput?.workflowId||task.scanInput?.parentScanId)return;
 const stateRoot=process.env.CODEX_SECURITY_STATE_DIR?nativePath(process.env.CODEX_SECURITY_STATE_DIR):undefined;if(!stateRoot||!task.sourceContentHash)throw new Error('Deep 恢复状态目录缺失');
 await mkdir(stateRoot,{recursive:true,mode:0o700});await canonical(stateRoot);
 const value:Manifest={schemaVersion:1,context,sourceHash:task.sourceContentHash,identity:task.sdkIdentity?.identity??null,stateRoot,rawDir:path.join(nativePath(task.outputPath),'raw/codex-security'),sourcePath:nativePath(task.sourcePath),model:task.model,...(task.sdkRuntime?{runtime:task.sdkRuntime}:{})};
 await writeFile(path.join(task.outputPath,DEEP_RESUME_MANIFEST),JSON.stringify(value)+'\n',{mode:0o600,flag:'wx'});
}
async function owningProcess(stateRoot:string,rawDir:string):Promise<boolean>{
 if(process.platform!=='linux')return false;
 for(const name of await readdir('/proc')){
  if(!/^\d+$/.test(name)||Number(name)===process.pid||Number(name)===process.ppid)continue;
  try{const env=await readFile('/proc/'+name+'/environ');if(env.includes(Buffer.from('CODEX_SECURITY_SCAN_DIR='+rawDir+'\0'))||env.includes(Buffer.from('CODEX_SECURITY_STATE_DIR='+stateRoot+'\0')))return true;}catch{}
 }
 return false;
}
async function fingerprintTree(directory:string):Promise<string>{
 const digest=createHash('sha256');
 async function visit(root:string){for(const entry of (await readdir(root,{withFileTypes:true})).sort((a,b)=>a.name.localeCompare(b.name))){const f=path.join(root,entry.name);if(entry.isSymbolicLink())throw new Error('检查点或会话含链接');if(entry.isDirectory())await visit(f);else if(entry.isFile()){digest.update(path.relative(directory,f)+'\0');digest.update(await regular(f,256*1024*1024));}}}
 await visit(directory);return digest.digest('hex');
}
async function databaseDigest(file:string):Promise<string>{
 await regular(file,256*1024*1024);const copy=file+'.resume-proof-'+randomUUID();
 const db=new DatabaseSync(file,{readOnly:true});try{db.prepare('VACUUM INTO ?').run(copy);return hash(await readFile(copy));}finally{db.close();await rm(copy,{force:true});}
}
export async function inspectDeepResume(claim:DeepResumeProbeClaim,checkProcesses=true,guardHeld=false):Promise<DeepResumeInspection>{
 validate('DeepResumeProbeClaim',claim,claim.execution);
 const empty:DeepResumeInspection={schemaVersion:1,sdkVersion:'0.5.0',execution:claim.execution,origin:claim.origin,agentId:claim.agentId,checkedAt:new Date().toISOString(),eligible:false,reason:'原生恢复条件未通过；原记录与证据保留。',sourceHash:claim.sourceHash,configurationHash:claim.configurationHash,scanId:null,threadId:null,proofHash:null,baseline:{estimatedUsd:null,usage:null}};
 let unlock:(()=>void)|undefined;
 try{
  await certifyDeepSDK();const origin=originOutput(claim.outputPath,claim.origin.attempt);await canonical(origin);if(!guardHeld)unlock=await acquireDeepGuard(origin);
  if(claim.origin.taskId!==claim.execution.taskId||claim.origin.attempt>claim.execution.attempt)throw new Error('原始执行身份不匹配');
  const m=JSON.parse((await regular(path.join(origin,DEEP_RESUME_MANIFEST),65536)).toString()) as Manifest;
  if(m.runtime){const runtime=JSON.parse((await regular(path.join(origin,'logs/sdk-runtime.json'),65536)).toString());if(runtime.id!==m.runtime.id||runtime.fingerprint!==m.runtime.fingerprint)throw new Error('原 Deep 的 SDK 运行基线发生变化');}
  if(m.schemaVersion!==1||!same(m.context.execution,claim.origin)||m.context.agentId!==claim.agentId||m.context.configurationHash!==claim.configurationHash||m.sourceHash!==claim.sourceHash||m.sourcePath!==nativePath(claim.sourcePath)||m.model!==claim.model||!same(m.identity,claim.identity))throw new Error('冻结源码、模型配置、原 Agent 或身份代次已变化');
  if(await lstat(path.join(origin,'normalized/result.commit.json')).catch(()=>null))throw new Error('原执行已有完整提交结果，应恢复结果入库或新建扫描');
  const expectedState=process.env.AXIOM_DEEP_RESUME_EXPECTED_STATE_ROOT??process.env.CODEX_SECURITY_STATE_DIR;
  if(!expectedState||nativePath(expectedState)!==m.stateRoot)throw new Error('原 SDK 状态目录与 Agent 绑定不匹配');
  await canonical(m.stateRoot);await canonical(m.rawDir);
  if(m.rawDir!==path.join(origin,'raw/codex-security'))throw new Error('原生输出目录不匹配');
  if(checkProcesses&&await owningProcess(m.stateRoot,m.rawDir))throw new Error('原生执行进程仍然存活，请等待停止后再核验');
  const dbPath=path.join(m.stateRoot,'workbench.sqlite3');await regular(dbPath,256*1024*1024);
  const db=new DatabaseSync(dbPath,{readOnly:true});let row:any;try{row=db.prepare('SELECT id FROM scans WHERE scan_dir=? AND mode=?').get(m.rawDir,'deep');}finally{db.close()}
  if(!row)throw new Error('原生 Deep 登记或检查点缺失');
  let context:any;
  try{const result=await exec(process.env.PYTHON??'python3',[path.join(sdkRoot(),'_bundled_plugin/scripts/workbench_db.py'),'get-cli-scan-resume','--scan-id',String(row.id)],{env:{...process.env,CODEX_SECURITY_STATE_DIR:m.stateRoot,CODEX_HOME:path.join(m.stateRoot,'codex-home')},timeout:30000,maxBuffer:8*1024*1024});context=JSON.parse(result.stdout);}
  catch{throw new Error('SDK 拒绝恢复：须为 running Deep、原 owning session、未停止协调器及未改变的源码目录/修订/内容');}
  if(context.scanId!==row.id||context.scanDir!==m.rawDir||!context.threadId||context.recipe?.repository!==m.sourcePath)throw new Error('SDK 返回的恢复身份或目录不匹配');
  const {findScanSession}=await part('scan-logs');const home=path.join(m.stateRoot,'codex-home');const session=await findScanSession(home,context.threadId);
  if(!session||session.workingDirectory!==m.rawDir)throw new Error('原 Codex 会话日志丢失或工作目录已改变');
  await regular(session.path,256*1024*1024);
  if(context.recipe.scanInputs?.knowledgeBase){const {restoreScanKnowledge}=await part('scan-inputs');await restoreScanKnowledge(m.rawDir,m.sourcePath,context.recipe.scanInputs);}
  const {ScanCostTracker}=await part('cost');const tracker=new ScanCostTracker({codexHome:home,model:claim.model,repository:m.sourcePath,scanDirectory:m.rawDir,onUsage:()=>{}});tracker.start(context.threadId);let snapshot:any;try{snapshot=await tracker.stop();}finally{await tracker.stop().catch(()=>null)}
  const usage=snapshot.usage?Object.fromEntries(tokens.map((k,i)=>[k,Number.isSafeInteger(snapshot.usage[usageKeys[i]!])&&snapshot.usage[usageKeys[i]!]>=0&&(k!=='cacheWriteInputTokens'||snapshot.usage.cache_write_input_tokens_reported!==false)?snapshot.usage[usageKeys[i]!]:null])) as DeepResumeUsage:null;
  const baseline:DeepResumeBaseline={estimatedUsd:typeof snapshot.cost?.estimatedUsd==='number'?snapshot.cost.estimatedUsd:null,usage};
  const proofHash=hash(JSON.stringify({context,manifest:hash(JSON.stringify(m)),database:await databaseDigest(dbPath),sessions:await fingerprintTree(path.join(home,'sessions')),checkpoints:await fingerprintTree(m.rawDir),baseline}));
  return {...empty,eligible:true,reason:'SDK 0.5.0 已核验原 Deep 会话、检查点、源码和冻结配置；续扫仍可能产生新增费用。',scanId:context.scanId,threadId:context.threadId,proofHash,baseline};
 }catch(error){
  const code=(error as NodeJS.ErrnoException)?.code;
  const reason=code==='ENOENT'?'原 Deep 恢复记录或检查点缺失，不能接续原会话；历史报告仍保留。':code==='EACCES'?'原 Agent 无法读取 Deep 会话和检查点，请检查执行节点的状态目录权限。':error instanceof SyntaxError||String(code).startsWith('ERR_SQLITE')?'原生状态库或恢复记录不可读取，恢复条件未通过；原证据仍保留。':error instanceof Error?error.message:'Deep 恢复条件未通过';
  return {...empty,reason:reason.slice(0,2048)};
 }finally{unlock?.()}

}
export async function runDeepProbe(file:string){const claim=decode('DeepResumeProbeClaim',await regular(file));const inspection=await inspectDeepResume(claim);const report={schemaVersion:1,id:claim.id,leaseToken:claim.leaseToken,agentId:claim.agentId,inspection};validate('DeepResumeProbeReport',report);process.stdout.write(JSON.stringify(report)+'\n');}
export async function loadDeepSidecars(contextFile:string|undefined,resumeFile:string|undefined,task:ScanTask){
 if(contextFile)task.deepResumeContext=decode('DeepResumeContext',await regular(contextFile),{taskId:String(task.taskId),attempt:task.executionAttempt!});
 if(resumeFile){task.deepResume=decode('DeepResumeAssignment',await regular(resumeFile),{taskId:String(task.taskId),attempt:task.executionAttempt!});const v=task.deepResume;if(!v.inspection.eligible||v.previous.taskId!==String(task.taskId)||v.previous.attempt+1!==task.executionAttempt!||v.inspection.origin.taskId!==String(task.taskId)||!same(v.previous,v.inspection.execution))throw new Error('续扫合同身份无效');}
}
export async function prepareDeepResume(task:ScanTask):Promise<{unlock:()=>void;rawDir:string;options:Record<string,unknown>;copyArtifacts:()=>Promise<void>}|null>{
 const assignment=task.deepResume;if(!assignment)return null;if(task.scanMode!=='deep'||task.mock||!task.sourceContentHash)throw new Error('续扫仅适用于真实 Deep 扫描');
 const v=assignment.inspection,origin=originOutput(task.outputPath,v.origin.attempt);
 const claim:DeepResumeProbeClaim={schemaVersion:1,id:'execute-check',leaseToken:'execute-check',execution:assignment.previous,origin:v.origin,agentId:v.agentId,sourcePath:task.sourcePath,outputPath:originOutput(task.outputPath,assignment.previous.attempt),sourceHash:task.sourceContentHash,configurationHash:v.configurationHash,model:task.model,identity:task.sdkIdentity?.identity??null};
 const unlock=await acquireDeepGuard(origin);
 try{
  const observed=await inspectDeepResume(claim,true,true);if(!observed.eligible||observed.proofHash!==v.proofHash)throw new Error('续扫证据已变化，SDK 尚未启动；请重新核验');
  // Recheck the same native eligibility under the exclusive execution guard.
  const manifest=JSON.parse((await regular(path.join(origin,DEEP_RESUME_MANIFEST))).toString()) as Manifest;
  if((!process.env.CODEX_SECURITY_STATE_DIR||nativePath(process.env.CODEX_SECURITY_STATE_DIR)!==manifest.stateRoot))throw new Error('续扫未绑定原 SDK 状态目录');
  const backup=path.join(task.outputPath,'runtime','resume-before');await mkdir(backup,{recursive:true,mode:0o700});await cp(manifest.rawDir,path.join(backup,'raw'),{recursive:true,errorOnExist:true,force:false,verbatimSymlinks:true});
  const nativeDB=new DatabaseSync(path.join(manifest.stateRoot,'workbench.sqlite3'),{readOnly:true});try{nativeDB.prepare('VACUUM INTO ?').run(path.join(backup,'workbench.sqlite3'));}finally{nativeDB.close()}
  await cp(path.join(manifest.stateRoot,'codex-home','sessions'),path.join(backup,'sessions'),{recursive:true,errorOnExist:true,force:false,verbatimSymlinks:true});
  const options:Record<string,unknown>={resumeScanId:v.scanId,outputDir:manifest.rawDir,archiveExisting:false,parentScanId:undefined,workflowId:undefined,expectedPluginVersion:undefined};
  const {stdout}=await exec(process.env.PYTHON??'python3',[path.join(sdkRoot(),'_bundled_plugin/scripts/workbench_db.py'),'get-cli-scan-resume','--scan-id',v.scanId!],{timeout:30000,maxBuffer:8*1024*1024});const context=JSON.parse(stdout);options.scanPrompt=context.userContext??undefined;
  if(context.recipe.scanInputs?.knowledgeBase){const {restoreScanKnowledge}=await part('scan-inputs');options.knowledgeBaseSnapshot=await restoreScanKnowledge(manifest.rawDir,task.sourcePath,context.recipe.scanInputs);}
  if(task.maxCostUsd!==undefined){if(v.baseline.estimatedUsd===null)throw new Error('原生累计估算未知，不能安全下发续扫预算');options.maxCostUsd=v.baseline.estimatedUsd+task.maxCostUsd;if(!Number.isFinite(options.maxCostUsd))throw new Error('续扫预算溢出');}
  if(task.maxCostUsd!==undefined&&typeof context.recipe.maxCostUsd==='number'&&Number(options.maxCostUsd)>context.recipe.maxCostUsd){await exec(process.env.PYTHON??'python3',[path.join(sdkRoot(),'_bundled_plugin/scripts/workbench_db.py'),'set-scan-cost-limit','--scan-id',v.scanId!,'--max-cost-usd',String(options.maxCostUsd)],{timeout:30000,maxBuffer:65536});}
  return {unlock,rawDir:manifest.rawDir,options,copyArtifacts:async()=>{const dest=path.join(task.outputPath,'raw/codex-security');await rm(dest,{recursive:true,force:true});await cp(manifest.rawDir,dest,{recursive:true,verbatimSymlinks:true});}};
 }catch(e){unlock();throw e;}
}
export function deltaUsage<T extends Record<string,any>>(value:T,baseline:DeepResumeBaseline):T {
 const result={...value};for(const [i,key] of tokens.entries()){for(const actual of [key,usageKeys[i]!]){if(!(actual in value))continue;const a=value[actual],b=baseline.usage?.[key];if(Number.isSafeInteger(a)&&typeof b==='number'&&a>=b)(result as any)[actual]=a-b;else delete result[actual];}}
 // Native pricing ranges refer to cumulative tokens; never label them incremental.
 delete result.estimatedUsdRange;
 if(typeof value.estimatedUsd==='number'&&baseline.estimatedUsd!==null&&value.estimatedUsd>=baseline.estimatedUsd)(result as any).estimatedUsd=Math.max(0,value.estimatedUsd-baseline.estimatedUsd);else delete result.estimatedUsd;
 return result;
}
// Only this exact-version bridge supplies the CLI-internal continuation option.
export function runPinnedDeep(security:CodexSecurity,repository:string,options:ScanOptions,internal:Record<string,unknown>={}){return security.run(repository,{...options,...internal} as ScanOptions);}
