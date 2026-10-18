import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {createHash} from 'node:crypto';
import {lstat,readdir} from 'node:fs/promises';
import {createReadStream} from 'node:fs';
import path from 'node:path';
import {DiffTarget,type ScanTarget} from '@openai/codex-security';
import {validate,type GitDiffAssignment} from '@axiom/contracts';
import type {ScanTask} from '../contract/scan-task.js';
const run=promisify(execFile);
export const gitDiffArgs=(base:string,head:string)=>['diff','--no-ext-diff','--no-textconv','--no-color','--full-index','--src-prefix=a/','--dst-prefix=b/','--diff-algorithm=myers','--no-indent-heuristic','--find-renames=50%','-l1000',base,head,'--'];
async function git(root:string,args:string[],signal?:AbortSignal):Promise<Buffer>{
 const env:NodeJS.ProcessEnv={PATH:process.env.PATH,GIT_CONFIG_NOSYSTEM:'1',GIT_CONFIG_SYSTEM:path.sep==='\\'?'NUL':'/dev/null',GIT_CONFIG_GLOBAL:path.sep==='\\'?'NUL':'/dev/null',GIT_TERMINAL_PROMPT:'0',GIT_OPTIONAL_LOCKS:'0',GIT_NO_REPLACE_OBJECTS:'1',GIT_ALLOW_PROTOCOL:'',LC_ALL:'C',LANG:'C',...(process.env.SystemRoot?{SystemRoot:process.env.SystemRoot}:{})};
 const {stdout}=await run('git',['--no-replace-objects','--no-optional-locks','--literal-pathspecs','-c','core.hooksPath='+env.GIT_CONFIG_GLOBAL,'-c','core.attributesFile='+env.GIT_CONFIG_GLOBAL,'-c','core.quotePath=true','-C',root,...args],{env,signal,timeout:60000,maxBuffer:64<<20,encoding:'buffer'});return stdout;
}
async function worktreeHash(root:string,signal?:AbortSignal){
 const files:{name:string,size:number,hash:string}[]=[];
 let total=0;
 async function visit(dir:string){for(const entry of await readdir(dir,{withFileTypes:true})){signal?.throwIfAborted();if(entry.name==='.git'&&dir===root)continue;const file=path.join(dir,entry.name),stat=await lstat(file);if(stat.isDirectory())await visit(file);else{if(!stat.isFile()||stat.isSymbolicLink())throw new Error('Git source contains a non-regular file');if(stat.size>(100<<20)||files.length>=200000||(total+=stat.size)>10*2**30)throw new Error('Git source exceeds snapshot limits');const hash=createHash('sha256');let size=0;for await(const chunk of createReadStream(file,{signal})){size+=(chunk as Buffer).length;if(size>stat.size)throw new Error('Git source changed while checking');hash.update(chunk as Buffer);}if(size!==stat.size)throw new Error('Git source changed while checking');files.push({name:path.relative(root,file).split(path.sep).join('/'),size,hash:hash.digest('hex')});}}}
 await visit(root);files.sort((a,b)=>Buffer.compare(Buffer.from(a.name),Buffer.from(b.name)));const h=createHash('sha256');for(const f of files)h.update(`${f.size}\t${f.name}\t${f.hash}\n`);return h.digest('hex');
}
export async function sdkScanTarget(task:ScanTask,signal?:AbortSignal):Promise<ScanTarget|undefined>{
 if(!task.gitTarget)return task.includePaths?.length?task.includePaths:undefined;
 validate('GitDiffTarget',task.gitTarget);
 if(task.scanMode!=='standard'||task.includePaths?.length)throw new Error('Git diff targets require Standard mode without a path override');
 const t=task.gitTarget;
 for(const [revision,expected] of [[t.baseCommit+'^{tree}',t.baseTree],[t.headCommit+'^{tree}',t.headTree],['HEAD',t.headCommit]])if((await git(task.sourcePath,['rev-parse','--verify','--end-of-options',revision!],signal)).toString().trim()!==expected)throw new Error('Git revision or tree differs from frozen input');
 const diff=await git(task.sourcePath,gitDiffArgs(t.baseCommit,t.headCommit),signal);if(createHash('sha256').update(diff).digest('hex')!==t.diffSha256||await worktreeHash(task.sourcePath,signal)!==t.headContentHash)throw new Error('Git diff or HEAD source differs from frozen input');
 // This Scanner is already private to one attempt. Optional index refreshes
 // must not rewrite the checked source while the SDK reads it.
 // The SDK also launches Git. Keep the same repository/config boundary for
 // those subprocesses; ambient GIT_DIR, object alternates or injected config
 // must not redirect a correctly checked target after this verification.
 for(const key of Object.keys(process.env))if(key.toUpperCase().startsWith('GIT_'))delete process.env[key];
 Object.assign(process.env,{GIT_CONFIG_NOSYSTEM:'1',GIT_CONFIG_SYSTEM:path.sep==='\\'?'NUL':'/dev/null',GIT_CONFIG_GLOBAL:path.sep==='\\'?'NUL':'/dev/null',GIT_TERMINAL_PROMPT:'0',GIT_OPTIONAL_LOCKS:'0',GIT_NO_REPLACE_OBJECTS:'1',GIT_ALLOW_PROTOCOL:''});
 return DiffTarget.refs({base:t.baseCommit,head:t.headCommit});
}
export function gitEvidenceBinding(task:ScanTask):GitDiffAssignment|undefined{
 if(!task.gitTarget)return;
 const v:GitDiffAssignment={schemaVersion:1,execution:{taskId:String(task.taskId),attempt:task.executionAttempt!},sourceSnapshotId:task.sourceSnapshotId!,sourceContentHash:task.sourceContentHash!,target:task.gitTarget,evidenceRevision:'head'};validate('GitDiffAssignment',v,v.execution);return v;
}
export function assertGitSDKResult(task:ScanTask,snapshot:unknown){
 if(!task.gitTarget)return;
 const t=(snapshot as {manifest?:{scan?:{target?:{kind?:unknown;baseRevision?:unknown;headRevision?:unknown}}}}|undefined)?.manifest?.scan?.target;
 if(t?.kind!=='git_diff'||t.baseRevision!==task.gitTarget.baseCommit||t.headRevision!==task.gitTarget.headCommit)throw new Error('SDK Git result target does not match the frozen base/head');
}
