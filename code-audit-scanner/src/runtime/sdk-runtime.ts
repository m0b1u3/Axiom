import {createHash} from 'node:crypto';
import {createRequire} from 'node:module';
import {readFile,mkdir,writeFile} from 'node:fs/promises';
import path from 'node:path';
import type {ScanTask} from '../contract/scan-task.js';
import {nativeOpenAI,resolveTaskProvider} from '../provider/model-provider.js';

export type SDKRuntimeId='official-v2'|'legacy-v1';
export interface SDKRuntimeBinding {id:SDKRuntimeId;fingerprint:string}
interface Descriptor extends SDKRuntimeBinding {sdkVersion:string;codexVersion:string;checks:Record<string,string>}
const packages={'official-v2':'@openai/codex-security','legacy-v1':'@axiom/codex-security-compat-v1'} as const;
const loaded=new Set<typeof import('@openai/codex-security')>();
const sha=(v:string)=>createHash('sha256').update(v).digest('hex');
const url=(value:string)=>{const u=new URL(value);return u.href.replace(/\/+$/,'');};

/** Only explicitly selected connections use V1. A model name never selects it. */
export function sdkRuntimeId(task:ScanTask,environment:NodeJS.ProcessEnv=process.env):SDKRuntimeId {
 if(task.sdkRuntime)return task.sdkRuntime.id;
 if(task.mock||nativeOpenAI(task))return 'official-v2';
 const spec=resolveTaskProvider(task),endpoint=task.baseUrl??spec.defaultBaseUrl;
 if(!endpoint)return 'official-v2';
 // Native V2 is the default. DeepSeek's agent-message envelope is translated
 // by its scoped Responses transport; old executions and explicit V1 pins stay bound.
 const entries:unknown=JSON.parse(environment.AXIOM_SDK_LEGACY_CONNECTIONS??'[]');
 if(!Array.isArray(entries))throw new Error('SDK 兼容连接配置必须是数组');
 for(const e of entries){
  if(!e||typeof e!=='object'||typeof e.provider!=='string'||typeof e.baseUrl!=='string'||Object.keys(e).some(k=>!['provider','baseUrl','model'].includes(k))||(e.model!==undefined&&typeof e.model!=='string'))throw new Error('SDK 兼容连接配置无效');
  if(e.provider===task.provider&&url(e.baseUrl)===url(endpoint)&&(e.model===undefined||e.model===task.model))return 'legacy-v1';
 }
 return 'official-v2';
}

async function descriptors():Promise<Record<SDKRuntimeId,Descriptor>>{
 const value=JSON.parse(await readFile(new URL('../../node_modules/.axiom-sdk-runtimes.json',import.meta.url),'utf8'));
 if(value.schemaVersion!==1)throw new Error('SDK 运行基线缺失；请重新安装锁定依赖');
 for(const id of ['official-v2','legacy-v1'] as const){const d=value.runtimes?.[id];if(d?.id!==id||d.sdkVersion!=='0.5.0'||d.codexVersion!=='0.163.0'||!/^[a-f0-9]{64}$/.test(d.fingerprint)||!d.checks||typeof d.checks!=='object')throw new Error('SDK 运行基线无效');}
 return value.runtimes;
}
export function sdkPackageRoot(task:ScanTask):string {
 return path.resolve(path.dirname(createRequire(import.meta.url).resolve(packages[sdkRuntimeId(task)])),'..');
}

/** Existing Deep attempts stay bound to their original runtime; no migration. */
export async function bindSDKRuntime(task:ScanTask):Promise<Descriptor>{
 const all=await descriptors();let id=sdkRuntimeId(task);
 if(task.deepResume&&!task.sdkRuntime){
  const attempt=task.deepResume.inspection.origin.attempt;
  if(!Number.isSafeInteger(attempt)||attempt<1)throw new Error('原 Deep 执行身份无效');
  const file=path.join(path.dirname(task.outputPath),'attempt-'+attempt,'logs/sdk-runtime.json');
  let old:SDKRuntimeBinding;
  try{old=JSON.parse(await readFile(file,'utf8'));}catch(e){if((e as NodeJS.ErrnoException).code!=='ENOENT')throw e;old=all['legacy-v1'];}
  if(!Object.hasOwn(all,old.id)||all[old.id].fingerprint!==old.fingerprint)throw new Error('原 Deep 的 SDK 运行基线不匹配；不能迁移续扫');
  id=old.id;
 }
 const selected=all[id];
 if(task.sdkRuntime&&task.sdkRuntime.fingerprint!==selected.fingerprint)throw new Error('任务的 SDK 运行基线发生变化');
 task.sdkRuntime={id,fingerprint:selected.fingerprint};
 const root=sdkPackageRoot(task),info=JSON.parse(await readFile(path.join(root,'package.json'),'utf8'));
 if(info.name!=='@openai/codex-security'||info.version!==selected.sdkVersion)throw new Error('SDK 包身份不匹配');
 for(const [name,want] of Object.entries(selected.checks)){if(sha(await readFile(path.join(root,name),'utf8'))!==want)throw new Error('SDK 运行文件指纹不匹配：'+name);}
 const plugin=JSON.parse(await readFile(path.join(root,'_bundled_plugin/.codex-plugin/plugin.json'),'utf8'));
 const file=path.join(task.outputPath,'logs/sdk-runtime.json');
 const value={schemaVersion:1,...selected,pluginVersion:plugin.version,execution:{taskId:String(task.taskId),attempt:task.executionAttempt??1},configurationHash:sha(JSON.stringify({provider:task.provider,connectionType:task.connectionType??null,baseUrl:task.baseUrl??null,model:task.model,sourceHash:task.sourceContentHash??null,mode:task.scanMode,includePaths:task.includePaths??[],sdkParameters:task.sdkParameters??null,scanInput:task.scanInput??null}))};
 await mkdir(path.dirname(file),{recursive:true,mode:0o700});
 try{await writeFile(file,JSON.stringify(value,null,2)+'\n',{mode:0o600,flag:'wx'});}catch(e){
  if((e as NodeJS.ErrnoException).code!=='EEXIST')throw e;
  if(JSON.stringify(JSON.parse(await readFile(file,'utf8')))!==JSON.stringify(value))throw new Error('执行目录已绑定不同的 SDK 或配置；禁止覆盖');
 }
 return selected;
}
export async function sdkForTask(task:ScanTask):Promise<typeof import('@openai/codex-security')>{
 await bindSDKRuntime(task);
 const sdk=await import(packages[sdkRuntimeId(task)]);loaded.add(sdk);return sdk;
}
export function isSDKRuntimeError(value:unknown,name:'ScanCostLimitExceededError'|'ScanInterruptedError'|'AuthenticationRequiredError'|'IncompleteScanError'|'CodexSecurityError'):boolean {
 for(const sdk of loaded){const constructor=sdk[name];if(typeof constructor==='function'&&value instanceof constructor)return true;}
 return false;
}
