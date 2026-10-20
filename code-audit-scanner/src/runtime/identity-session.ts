import {createHash} from 'node:crypto';
import {DatabaseSync} from 'node:sqlite';
import {lstat,mkdtemp,readFile,realpath,rename,rm} from 'node:fs/promises';
import path from 'node:path';
import {decode} from '@axiom/contracts';
import type {ScanTask} from '../contract/scan-task.js';

const sessions=new WeakMap<ScanTask,string>();
const hash=(value:Uint8Array)=>createHash('sha256').update(value).digest('hex');

/** A short filesystem critical section, not a lock held during model execution.
 * SQLite's OS lock is released even when a Scanner process crashes. The file is
 * private identity state, separate from the Server's business database.
 */
export async function withIdentitySessionLock<T>(home:string,work:()=>Promise<T>):Promise<T>{
 const db=new DatabaseSync(path.join(home,'.session-sync.sqlite3'));
 const deadline=Date.now()+15000;let locked=false;
 try{
  db.exec('PRAGMA busy_timeout=0');
  while(!locked){
   try{db.exec('BEGIN IMMEDIATE');locked=true}
   catch(error){
    if(!/locked|busy/i.test(String(error))||Date.now()>=deadline)throw new Error('SDK 凭据写回暂时不可用；执行身份未变更');
    await new Promise(resolve=>setTimeout(resolve,20));
   }
  }
  const result=await work();db.exec('COMMIT');locked=false;return result;
 }finally{if(locked){try{db.exec('ROLLBACK')}catch{}}db.close()}
}

async function credential(file:string):Promise<Buffer>{
 const info=await lstat(file);
 if(!info.isFile()||info.isSymbolicLink()||info.size>4*1024*1024)throw new Error('SDK 凭据文件不可用');
 return readFile(file);
}
async function checkIdentity(task:ScanTask,home:string):Promise<void>{
 const marker=decode('SDKIdentityRef',await readFile(path.join(home,'session.json'),'utf8'));
 const expected=task.sdkIdentity!.identity;
 if((['id','name','agentId','generation','kind'] as const).some(key=>marker[key]!==expected[key]))throw new Error('SDK session generation changed');
 const current=path.join(home,'sdk/codex-home');
 if(await realpath(current)!==current)throw new Error('SDK credential directory must not be a symlink');
}

/** Freeze an opaque auth-only copy for this execution. No credential enters
 * task artifacts, results, events or Server state.
 */
export async function snapshotIdentitySession(task:ScanTask,home:string,root:string):Promise<string>{
 return withIdentitySessionLock(home,async()=>{
  await checkIdentity(task,home);
  const current=path.join(home,'sdk/codex-home');await credential(path.join(current,'auth.json'));
  const destination=await mkdtemp(path.join(root,'.auth-source-'));
  try{
   const {importAmbientAuth}=await import('@openai/codex-security');
   if(!await importAmbientAuth(current,destination))throw new Error('SDK ChatGPT session unavailable');
   sessions.set(task,hash(await credential(path.join(destination,'auth.json'))));
   return destination;
  }catch(error){await rm(destination,{recursive:true,force:true});throw error}
 });
}

/** An older parallel execution cannot overwrite a session another execution
 * has already refreshed. Only one atomic auth-file rename is needed; workbench
 * data and the rest of the SDK home are never replaced.
 */
export async function commitIdentitySession(task:ScanTask,home:string,root:string):Promise<'saved'|'unchanged'|'superseded'>{
 const baseline=sessions.get(task);if(!baseline)throw new Error('SDK execution credential snapshot is missing');
 return withIdentitySessionLock(home,async()=>{
  await checkIdentity(task,home);
  const current=path.join(home,'sdk/codex-home/auth.json');
  if(hash(await credential(current))!==baseline)return 'superseded';
  const directory=path.join(home,'sdk'),next=await mkdtemp(path.join(directory,'.session-next-'));
  try{
   const {importAmbientAuth}=await import('@openai/codex-security');
   if(!await importAmbientAuth(path.join(root,'codex-home'),next))return 'unchanged';
   const candidate=path.join(next,'auth.json');
   if(hash(await credential(candidate))===baseline)return 'unchanged';
   await rename(candidate,current);return 'saved';
  }finally{await rm(next,{recursive:true,force:true})}
 });
}
