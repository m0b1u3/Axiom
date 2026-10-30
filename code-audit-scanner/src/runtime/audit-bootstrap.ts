import {mkdir,copyFile,chmod,writeFile} from 'node:fs/promises';
import {mkdirSync} from 'node:fs';
import {sdkForTask} from './sdk-runtime.js';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
import path from 'node:path';
import type {ScanTask} from '../contract/scan-task.js';
const exec=promisify(execFile);
export async function prepareAuditWorkspace(task:ScanTask,workspaceOutput=task.outputPath,options:{tempDir?:string;manageTemp?:boolean}={}){
 const root=path.join(workspaceOutput,'runtime/preflight');const temp=options.tempDir??path.join(workspaceOutput,'raw/codex-security/artifacts/tmp');
 await mkdir(root,{recursive:true,mode:0o700});if(options.manageTemp!==false)await mkdir(temp,{recursive:true,mode:0o700});
 const evidenceHelper=path.join(root,'source-evidence.mjs');
 await copyFile(new URL('../../runtime/source-evidence.mjs',import.meta.url),evidenceHelper);await chmod(evidenceHelper,0o600);
 const command=`${JSON.stringify(process.execPath)} ${JSON.stringify(evidenceHelper)}`;
 const evidencePrompt=`源码证据产出要求：使用宿主只读辅助程序 ${command} extract "$CODEX_SECURITY_REPOSITORY" <相对路径> <起始行> <结束行>，从返回的 JSON 复制 path/startLine/endLine/code，保留完整行、缩进和空白，不重打或摘要 code。解释放 explanation。提交 record_codex_security_scan_draft 前，将拟提交的同一份草稿 JSON（含 findings）保存到扫描产物目录，执行 ${command} check "$CODEX_SECURITY_REPOSITORY" <草稿JSON路径>。检查非零退出时，按诊断重新提取并修正草稿，再检查；通过后以该同一对象提交，禁止忽略失败、去空白比对或修改源码来通过。辅助程序不写文件，不改变 SDK schema；SDK 仍负责正式产物与封存。`;
 // SDK archiveExisting can rename the entire raw output before the first
 // SessionEvent. Codex's synthetic mount registry needs a stable temp root
 // even during that bootstrap window; evidence scratch remains in artifacts.
 const bootstrapTemp=path.join(workspaceOutput,'runtime','sdk-tmp');
 await mkdir(bootstrapTemp,{recursive:true,mode:0o700});
 const keys=options.manageTemp===false?[]:['TMPDIR','TMP','TEMP'];const previous=keys.map(k=>process.env[k]);for(const k of keys)process.env[k]=bootstrapTemp;
 const ensureTemp=()=>mkdirSync(temp,{recursive:true,mode:0o700});
 const restore=()=>keys.forEach((k,i)=>{if(previous[i]===undefined)delete process.env[k];else process.env[k]=previous[i]});
 const checkEvidence=async(repository:string,findings:unknown)=>{
  const draft=path.join(root,'final-evidence.json');await writeFile(draft,JSON.stringify({findings})+'\n',{mode:0o600});
  try{const {stdout}=await exec(process.execPath,[evidenceHelper,'check',repository,draft],{timeout:15000,maxBuffer:1024*1024});const result=JSON.parse(stdout);if(result.ok!==true||!Number.isSafeInteger(result.checked))throw new Error('证据检查没有返回有效回执');}
  catch(error){const details=(error as {stdout?:string}).stdout??'';throw new Error('源码证据校验未通过，原始 SDK 结果保留：'+details.slice(0,8000));}
 };
 const scope=task.includePaths?.length===1?path.join(task.sourcePath,task.includePaths[0]!):task.sourcePath;
 const result=path.join(root,'security-guidance.md');
 const sdk=await sdkForTask(task).catch(error=>{restore();throw error;});
 try{
  const helper=path.join(await sdk.bundledPluginRoot(),'mcp/helpers.mjs');
  await exec(process.execPath,[helper,'resolve-security-md','--repo',task.sourcePath,'--scope',scope,'--out',result],{timeout:15000,maxBuffer:1024*1024});
  return {restore,ensureTemp,checkEvidence,prompt:`指导文件已由宿主执行 resolve-security-md --repo <目标仓库> --scope <审计范围> 完成解析，结果保存在 ${JSON.stringify(result)}。这是只读指导文件解析，不代表 SDK 预检或身份验证成功。读取该结果并继续审计。审计范围仍以 SDK scope 为准。临时文件写入 ${JSON.stringify(temp)}；不要写 /tmp，不改目标源码。\n${evidencePrompt}`};
 }catch{ return {restore,ensureTemp,checkEvidence,prompt:`临时文件只写 ${JSON.stringify(temp)}，不要写 /tmp。指导文件预解析未成功，请按插件 references/security-guidance.md 使用 launch_codex_security_mcp --helper resolve-security-md；必须指定 --repo、--scope 和 --out，保留该解析失败信息。\n${evidencePrompt}`};}
}
