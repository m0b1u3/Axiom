// Opaque fixture credentials and the real SDK copy helper only. No login/model.
import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,mkdir,readFile,writeFile,rm,realpath,readdir} from 'node:fs/promises';
import {spawn} from 'node:child_process';
import path from 'node:path';
import os from 'node:os';
import {snapshotIdentitySession,commitIdentitySession,withIdentitySessionLock} from '../src/runtime/identity-session.js';
import type {ScanTask} from '../src/contract/scan-task.js';

const task=(id:string):ScanTask=>({taskId:id,executionAttempt:1,sourcePath:'/fixture/source',outputPath:'/fixture/output',provider:'openai',model:'fixture-model',scanMode:'standard',timeoutMinutes:1,sdkIdentity:{schemaVersion:1,execution:{taskId:id,attempt:1},identity:{id:'1',name:'Fixtures / Mock session',agentId:'fixture-agent',generation:1,kind:'chatgpt'}}});
test('parallel execution snapshots cannot overwrite an already-refreshed identity, generation or workbench',async t=>{
 const temporary=await mkdtemp(path.join(os.tmpdir(),'axiom-parallel-session-')),home=await realpath(temporary);t.after(()=>rm(home,{recursive:true,force:true}));
 const auth=path.join(home,'sdk/codex-home/auth.json');await mkdir(path.dirname(auth),{recursive:true});await writeFile(auth,'Fixtures / Mock initial opaque session');
 const a=task('42'),b=task('43'),rootA=path.join(home,'executions/42-1/sdk'),rootB=path.join(home,'executions/43-1/sdk');await mkdir(rootA,{recursive:true});await mkdir(rootB,{recursive:true});
 await writeFile(path.join(home,'session.json'),JSON.stringify(a.sdkIdentity!.identity));
 const [snapshotA,snapshotB]=await Promise.all([snapshotIdentitySession(a,home,rootA),snapshotIdentitySession(b,home,rootB)]);
 assert.notEqual(snapshotA,snapshotB);for(const snapshot of [snapshotA,snapshotB])assert.equal(await readFile(path.join(snapshot,'auth.json'),'utf8'),'Fixtures / Mock initial opaque session');
 for(const [root,content] of [[rootA,'Fixtures / Mock first refresh'],[rootB,'Fixtures / Mock stale parallel refresh']]){
  await mkdir(path.join(root!,'codex-home'),{recursive:true});await writeFile(path.join(root!,'codex-home/auth.json'),content!);await writeFile(path.join(root!,'workbench.sqlite3'),'Fixtures / Mock workbench');
 }
 assert.equal(await commitIdentitySession(a,home,rootA),'saved');assert.equal(await commitIdentitySession(b,home,rootB),'superseded');
 assert.equal(await readFile(auth,'utf8'),'Fixtures / Mock first refresh');assert.deepEqual(await readdir(path.join(home,'sdk/codex-home')),['auth.json']);
 assert.equal(await readFile(path.join(snapshotB,'auth.json'),'utf8'),'Fixtures / Mock initial opaque session');
 const c=task('44'),rootC=path.join(home,'executions/44-1/sdk');await mkdir(rootC,{recursive:true});await snapshotIdentitySession(c,home,rootC);await mkdir(path.join(rootC,'codex-home'));await writeFile(path.join(rootC,'codex-home/auth.json'),'Fixtures / Mock new generation attempt');
 await writeFile(path.join(home,'session.json'),JSON.stringify({...c.sdkIdentity!.identity,generation:2}));
 await assert.rejects(()=>commitIdentitySession(c,home,rootC),/generation changed/);assert.equal(await readFile(auth,'utf8'),'Fixtures / Mock first refresh');
});

test('credential writeback OS lock is released when another Scanner process crashes',async t=>{
 const temporary=await mkdtemp(path.join(os.tmpdir(),'axiom-session-crash-')),home=await realpath(temporary);t.after(()=>rm(home,{recursive:true,force:true}));
 const program=`import {DatabaseSync} from 'node:sqlite';const db=new DatabaseSync(process.argv[1]);db.exec('BEGIN IMMEDIATE');process.stdout.write('LOCKED\\n');setInterval(()=>{},1000);`;
 const child=spawn(process.execPath,['--input-type=module','-e',program,path.join(home,'.session-sync.sqlite3')],{stdio:['ignore','pipe','pipe']});
 t.after(()=>{if(child.exitCode===null)child.kill('SIGKILL')});
 await new Promise<void>((resolve,reject)=>{let out='';child.stdout.on('data',data=>{out+=data;if(out.includes('LOCKED'))resolve()});child.once('error',reject);child.once('exit',()=>reject(new Error('fixture lock owner exited too soon')))});
 const exit=new Promise<void>(resolve=>child.once('exit',()=>resolve()));child.kill('SIGKILL');await exit;
 assert.equal(await withIdentitySessionLock(home,async()=> 'recovered'),'recovered');
});
