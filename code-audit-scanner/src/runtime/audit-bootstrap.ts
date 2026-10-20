import {mkdir} from 'node:fs/promises';
import {mkdirSync} from 'node:fs';
import {createRequire} from 'node:module';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
import path from 'node:path';
import type {ScanTask} from '../contract/scan-task.js';
const exec=promisify(execFile);
export async function prepareAuditWorkspace(task:ScanTask):Promise<{prompt:string;restore:()=>void;ensureTemp:()=>void}>{
 const root=path.join(task.outputPath,'runtime/preflight');const temp=path.join(task.outputPath,'raw/codex-security/artifacts/tmp');
 await mkdir(root,{recursive:true,mode:0o700});await mkdir(temp,{recursive:true,mode:0o700});
 const keys=['TMPDIR','TMP','TEMP'];const previous=keys.map(k=>process.env[k]);for(const k of keys)process.env[k]=temp;
 const ensureTemp=()=>mkdirSync(temp,{recursive:true,mode:0o700});
 const restore=()=>keys.forEach((k,i)=>{if(previous[i]===undefined)delete process.env[k];else process.env[k]=previous[i]});
 const scope=task.includePaths?.length===1?path.join(task.sourcePath,task.includePaths[0]!):task.sourcePath;
 const result=path.join(root,'security-guidance.md');
 try{
  const entry=createRequire(import.meta.url).resolve('@openai/codex-security');
  // 0.5.0 moved this read-only resolver into the bundled Node helper.
  const helper=path.resolve(path.dirname(entry),'../_bundled_plugin/mcp/helpers.mjs');
  await exec(process.execPath,[helper,'resolve-security-md','--repo',task.sourcePath,'--scope',scope,'--out',result],{timeout:15000,maxBuffer:1024*1024});
  return {restore,ensureTemp,prompt:`指导文件已由宿主执行 resolve-security-md --repo <目标仓库> --scope <审计范围> 完成解析，结果保存在 ${JSON.stringify(result)}。这是只读指导文件解析，不代表 SDK 预检或身份验证成功。读取该结果并继续审计。审计范围仍以 SDK scope 为准。临时文件写入 ${JSON.stringify(temp)}；不要写 /tmp，不改目标源码。`};
 }catch{ return {restore,ensureTemp,prompt:`临时文件只写 ${JSON.stringify(temp)}，不要写 /tmp。指导文件预解析未成功，请按插件 references/security-guidance.md 使用 launch_codex_security_mcp --helper resolve-security-md；必须指定 --repo、--scope 和 --out，保留该解析失败信息。`};}
}
