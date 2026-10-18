import {DatabaseSync} from 'node:sqlite';
import {createHash,randomUUID} from 'node:crypto';
import {lstat,mkdir,readFile,writeFile,rename,rm,realpath} from 'node:fs/promises';
import path from 'node:path';
import {validate,decode,type SDKParentState} from '@axiom/contracts';
import type {ScanTask} from '../contract/scan-task.js';
const MAX_STATE_BYTES=8*1024*1024;
export const PARENT_STATE_FILE='normalized/parent-state.json';
const hash=(v:Uint8Array)=>createHash('sha256').update(v).digest('hex');

/** Exact-version adapter for the SDK's local workbench. SQLite is copied as an
 * opaque database: no SDK table is read or changed, and codex-home/auth files are
 * never included. The SDK validates the restored parent and repository itself. */
export async function restoreSdkParentState(task:ScanTask):Promise<void>{
 const state=task.parentState,id=task.scanInput?.parentScanId;
 if(!id){if(state)throw new Error('Unexpected parent state');return;}
 if(!state)throw new Error('Parent scan requires a frozen SDK workbench state');validate('SDKParentState',state);
 if(state.scanId!==id||state.sourceHash!==task.sourceContentHash)throw new Error('Parent SDK state differs from the selected scan or source');
 const bytes=Buffer.from(state.data,'base64');if(bytes.length>MAX_STATE_BYTES||hash(bytes)!==state.sha256||bytes.subarray(0,16).toString()!=='SQLite format 3\0')throw new Error('Parent SDK state is invalid or changed');
 const root=await realpath(task.outputPath),directory=task.sdkIdentity?path.resolve(process.env.CODEX_SECURITY_STATE_DIR??''):path.join(root,'runtime/sdk');await mkdir(directory,{recursive:true,mode:0o700});if(await realpath(directory)!==directory)throw new Error('Parent SDK directory must not be a symlink');
 const configured=process.env.CODEX_SECURITY_STATE_DIR;if(!configured||await realpath(configured)!==directory)throw new Error('Parent SDK state requires the isolated execution workbench');
 const name=path.join(directory,'workbench.sqlite3');await writeFile(name,bytes,{flag:'wx',mode:0o600});
}
export async function exportSdkParentState(task:ScanTask,scanId:string):Promise<void>{
 if(!task.sourceContentHash||!task.executionAttempt)return;
 const source=path.join(task.sdkIdentity?path.resolve(process.env.CODEX_SECURITY_STATE_DIR??''):path.join(task.outputPath,'runtime/sdk'),'workbench.sqlite3');
 const info=await lstat(source).catch(()=>null);if(!info?.isFile()||info.isSymbolicLink())return;
 const snapshot=path.join(task.outputPath,`runtime/.parent-${randomUUID()}.sqlite3`);
 try{
  // The SDK has closed. VACUUM INTO is SQLite's consistent-copy operation and
  // includes committed WAL data without copying credentials or interpreting rows.
  const db=new DatabaseSync(source,{readOnly:true});try{db.prepare('VACUUM INTO ?').run(snapshot);}finally{db.close();}
  if((await lstat(snapshot)).size>MAX_STATE_BYTES)return;
  const bytes=await readFile(snapshot),commit=decode('ResultCommit',await readFile(path.join(task.outputPath,'normalized/result.commit.json'),'utf8'));
  const state:SDKParentState={schemaVersion:1,execution:{taskId:String(task.taskId),attempt:task.executionAttempt},sdkVersion:'0.1.25',scanId,sourceHash:task.sourceContentHash,resultSha256:commit.sha256,sha256:hash(bytes),data:bytes.toString('base64')};validate('SDKParentState',state,state.execution);
  const file=path.join(task.outputPath,PARENT_STATE_FILE),temp=file+'.tmp';await writeFile(temp,JSON.stringify(state)+'\n',{mode:0o600});await rename(temp,file);
 }finally{await rm(snapshot,{force:true});}
}
