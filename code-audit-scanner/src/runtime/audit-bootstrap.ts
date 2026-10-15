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
  const helper=path.resolve(path.dirname(entry),'../_bundled_plugin/scripts/resolve_security_md.py');
  await exec(process.env['PYTHON']??'python3',[helper,'--repo',task.sourcePath,'--scope',scope,'--out',result],{timeout:15000,maxBuffer:1024*1024});
  return {restore,ensureTemp,prompt:`预检已由宿主执行 resolve_security_md.py --repo <目标仓库> --scope <审计范围>，结果保存在 ${JSON.stringify(result)}。读取该结果并继续审计，不重复探索 helper 参数。审计范围仍以 SDK scope 为准。临时文件写入 ${JSON.stringify(temp)}；不要写 /tmp，不改目标源码。`};
 }catch{ return {restore,ensureTemp,prompt:`临时文件只写 ${JSON.stringify(temp)}，不要写 /tmp。指导文件预解析未成功，请调用 "$PYTHON" "$CODEX_SECURITY_PLUGIN_ROOT/scripts/resolve_security_md.py" --repo "$CODEX_SECURITY_REPOSITORY" --scope "$CODEX_SECURITY_REPOSITORY" --out <扫描产物目录内的文件>；--scope 不可省略。`};}
}
