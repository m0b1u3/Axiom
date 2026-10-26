import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,mkdir,writeFile,readFile,rm,realpath,symlink} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {DatabaseSync} from 'node:sqlite';
import {spawnSync,spawn} from 'node:child_process';
import {randomUUID} from 'node:crypto';
import {inspectDeepResume,saveDeepManifest,prepareDeepResume,deltaUsage,acquireDeepGuard,originOutput} from '../src/runtime/deep-resume.js';
import type {DeepResumeProbeClaim,DeepResumeAssignment,DeepResumeBaseline} from '@axiom/contracts';
import type {ScanTask} from '../src/contract/scan-task.js';
const sdk=new URL('../node_modules/@openai/codex-security/',import.meta.url);
const wb=new URL('_bundled_plugin/scripts/workbench_db.py',sdk).pathname;
async function fixture(t:any){
 const root=await realpath(await mkdtemp(path.join(tmpdir(),'axiom-deep-resume-')));t.after(()=>rm(root,{recursive:true,force:true}));
 const output=path.join(root,'scans/17/attempt-1'),source=path.join(root,'source'),state=path.join(output,'runtime/sdk'),raw=path.join(output,'raw/codex-security');
 await mkdir(source,{recursive:true,mode:0o700});await mkdir(raw,{recursive:true,mode:0o700});await writeFile(path.join(source,'fixture.ts'),'// Fixtures / Mock only\n');
 const before=process.env.CODEX_SECURITY_STATE_DIR;process.env.CODEX_SECURITY_STATE_DIR=state;t.after(()=>{if(before===undefined)delete process.env.CODEX_SECURITY_STATE_DIR;else process.env.CODEX_SECURITY_STATE_DIR=before});
 const task={taskId:17,executionAttempt:1,sourcePath:source,sourceContentHash:'a'.repeat(64),outputPath:output,model:'gpt-6-sol',scanMode:'deep',timeoutMinutes:60,deepResumeContext:{schemaVersion:1,execution:{taskId:'17',attempt:1},agentId:'fixture-agent',configurationHash:'b'.repeat(64)}} as ScanTask;
 await saveDeepManifest(task);
 const recipe={repository:source,mode:'deep',target:{kind:'repository',paths:[]},config:{},scanInputs:{knowledgeBase:null}};
 const env={...process.env,CODEX_HOME:path.join(state,'codex-home')};
 const run=spawnSync(process.env.PYTHON??'python3',[wb,'register-cli-scan','--repository',source,'--scan-dir',raw,'--registration-json-stdin'],{env,input:JSON.stringify({recipe,userContext:'Fixture original prompt'}),encoding:'utf8'});
 assert.equal(run.status,0,run.stderr);const registration=JSON.parse(run.stdout),thread=randomUUID();
 const db=new DatabaseSync(path.join(state,'workbench.sqlite3'));db.prepare('UPDATE scans SET continuation_thread_id=?,deep_scan_owner_thread_id=? WHERE id=?').run(thread,thread,registration.scanId);db.close();
 const sessions=path.join(state,'codex-home/sessions/2026/10/21');await mkdir(sessions,{recursive:true});
 const usage={input_tokens:1000,cached_input_tokens:500,cache_write_input_tokens:0,output_tokens:50,reasoning_output_tokens:0,total_tokens:1050};
 await writeFile(path.join(sessions,'rollout-'+thread+'.jsonl'),[
 {type:'session_meta',timestamp:'2026-10-21T07:00:00Z',payload:{id:thread,cwd:raw,timestamp:'2026-10-21T07:00:00Z'}},
 {type:'token_usage_record',timestamp:'2026-10-21T07:00:01Z',payload:{thread_id:thread,response_id:'fixture-response',usage,thread_token_usage:usage}},
 ].map(v=>JSON.stringify(v)+'\n').join(''));
 const claim:DeepResumeProbeClaim={schemaVersion:1,id:'fixture',leaseToken:'fixture-lease',execution:{taskId:'17',attempt:1},origin:{taskId:'17',attempt:1},agentId:'fixture-agent',sourcePath:source,outputPath:output,sourceHash:task.sourceContentHash!,configurationHash:'b'.repeat(64),model:task.model,identity:null};
 return {root,task,claim,output,source,state,raw,thread,registration};
}
test('official SDK checkpoint inspection is stable and preserves original identity without model calls',async t=>{
 const f=await fixture(t);const a=await inspectDeepResume(f.claim,false),b=await inspectDeepResume(f.claim,false);
 assert.equal(a.eligible,true,a.reason);assert.equal(b.eligible,true,b.reason);assert.equal(a.proofHash,b.proofHash,'read-only inspection changed proof');assert.equal(a.scanId,f.registration.scanId);assert.equal(a.threadId,f.thread);assert.equal(a.baseline.usage?.inputTokens,1000);assert.equal(a.baseline.usage?.totalTokens,1050);assert.ok(a.baseline.estimatedUsd!>0);
});
test('SDK statuses, owning session, source replacement and missing logs fail closed',async t=>{
 const f=await fixture(t),db=new DatabaseSync(path.join(f.state,'workbench.sqlite3'));t.after(()=>db.close());
 for(const status of ['failed','complete']){db.prepare('UPDATE scans SET status=? WHERE id=?').run(status,f.registration.scanId);assert.equal((await inspectDeepResume(f.claim,false)).eligible,false,status)}
 db.prepare("UPDATE scans SET status='running',canceled_at='2026-10-21T07:01:00Z' WHERE id=?").run(f.registration.scanId);assert.equal((await inspectDeepResume(f.claim,false)).eligible,false);
 db.prepare("UPDATE scans SET canceled_at=NULL,handoff_status='pending' WHERE id=?").run(f.registration.scanId);assert.equal((await inspectDeepResume(f.claim,false)).eligible,false);
 db.prepare("UPDATE scans SET handoff_status='delivered' WHERE id=?").run(f.registration.scanId);await writeFile(path.join(f.source,'fixture.ts'),'// changed frozen contents\n');assert.equal((await inspectDeepResume(f.claim,false)).eligible,false);
});
test('foreign configuration, identity, original Agent and state root cannot resume',async t=>{
 const f=await fixture(t);
 for(const change of [{agentId:'other-agent'},{configurationHash:'e'.repeat(64)},{sourceHash:'f'.repeat(64)},{model:'other-model'},{origin:{taskId:'18',attempt:1}}])assert.equal((await inspectDeepResume({...f.claim,...change},false)).eligible,false);
 process.env.AXIOM_DEEP_RESUME_EXPECTED_STATE_ROOT=path.join(f.root,'foreign');t.after(()=>delete process.env.AXIOM_DEEP_RESUME_EXPECTED_STATE_ROOT);assert.equal((await inspectDeepResume(f.claim,false)).eligible,false);
});
test('continuation refuses stale proof and backs up native state before the same-session bridge',async t=>{
 const f=await fixture(t),inspection=await inspectDeepResume(f.claim,false);assert.equal(inspection.eligible,true,inspection.reason);
 const assignment:DeepResumeAssignment={schemaVersion:1,execution:{taskId:'17',attempt:2},previous:f.claim.execution,inspection};const task={...f.task,executionAttempt:2,outputPath:path.join(f.root,'scans/17/attempt-2'),deepResume:assignment};
 const changed={...task,deepResume:{...assignment,inspection:{...inspection,proofHash:'d'.repeat(64)}}};await assert.rejects(prepareDeepResume(changed),/重新核验/);
 const before=await readFile(path.join(f.output,'runtime/deep-resume.json'));const prepared=await prepareDeepResume(task);assert.ok(prepared);try{assert.equal(prepared.options.resumeScanId,inspection.scanId);assert.equal(prepared.options.outputDir,f.raw);assert.equal(prepared.options.scanPrompt,'Fixture original prompt');assert.equal(prepared.options.archiveExisting,false);assert.ok((await readFile(path.join(task.outputPath,'runtime/resume-before/workbench.sqlite3'))).length);await writeFile(path.join(f.raw,'report.md'),'# Fixtures / Mock\n');await prepared.copyArtifacts();assert.equal(await readFile(path.join(task.outputPath,'raw/codex-security/report.md'),'utf8'),'# Fixtures / Mock\n');assert.deepEqual(await readFile(path.join(f.output,'runtime/deep-resume.json')),before);}finally{prepared.unlock()}
});
test('guard contention prevents concurrent reuse and releases without altering SDK state',async t=>{
 const f=await fixture(t),release=await acquireDeepGuard(f.output);await assert.rejects(acquireDeepGuard(f.output),/占用/);assert.equal((await inspectDeepResume(f.claim,false)).eligible,false);release();const again=await acquireDeepGuard(f.output);again();assert.throws(()=>originOutput(f.output,0));
});
test('incremental accounting covers SDK snake-case usage, cost, unknowns and regressing observations',()=>{
 const baseline:DeepResumeBaseline={estimatedUsd:1.2,usage:{inputTokens:100,cachedInputTokens:50,cacheWriteInputTokens:null,outputTokens:5,reasoningOutputTokens:null,totalTokens:105}};
 const usage=deltaUsage({input_tokens:140,cached_input_tokens:60,cache_write_input_tokens:0,output_tokens:8,reasoning_output_tokens:0,total_tokens:148},baseline);assert.deepEqual(usage,{input_tokens:40,cached_input_tokens:10,output_tokens:3,total_tokens:43});
 const cost=deltaUsage({estimatedUsd:1.5,inputTokens:140,outputTokens:8,estimatedUsdRange:{min:1.5,max:2}},baseline);assert.equal(cost.inputTokens,40);assert.ok(Math.abs(cost.estimatedUsd-.3)<1e-12);assert.equal('estimatedUsdRange' in cost,false);
 assert.deepEqual(deltaUsage({estimatedUsd:1,inputTokens:99},baseline),{});assert.deepEqual(deltaUsage({estimatedUsd:1,inputTokens:100},{estimatedUsd:null,usage:null}),{});
});

test('real SDK API selects resumeThread with the original Scan ID and never starts a replacement',async t=>{
 const f=await fixture(t),inspection=await inspectDeepResume(f.claim,false);assert.equal(inspection.eligible,true,inspection.reason);
 const task={...f.task,executionAttempt:2,outputPath:path.join(f.root,'scans/17/attempt-2'),deepResume:{schemaVersion:1,execution:{taskId:'17',attempt:2},previous:f.claim.execution,inspection}} as ScanTask;
 const prepared=await prepareDeepResume(task);assert.ok(prepared);
 const {CodexSecurity,DEFAULT_DEPENDENCIES}=await import(new URL('dist/api.js',sdk).href);
 const {pluginMetadata}=await import(new URL('dist/runtime.js',sdk).href);
 const {runPinnedDeep}=await import('../src/runtime/deep-resume.js');
 const pluginRoot=new URL('_bundled_plugin/',sdk).pathname.replace(/\/$/,'');const metadata=await pluginMetadata(pluginRoot);
 const home=path.join(f.state,'codex-home');let resumed='',started=0,boundary=0;
 const environment={PATH:process.env.PATH,HOME:path.join(f.root,'home'),CODEX_HOME:home,CODEX_SECURITY_STATE_DIR:f.state,OPENAI_API_KEY:'fixture-only-no-real-credential'};
 await mkdir(environment.HOME,{recursive:true,mode:0o700});
 const security=new CodexSecurity({codexOverrides:{model:f.task.model}}, {...DEFAULT_DEPENDENCIES,environment,
  prepareRuntime:async()=>({codexHome:home,bootstrapWorkspace:home,credentialsAvailable:true,environment,plugin:{...metadata,pluginRoot,installedRoot:pluginRoot,marketplaceRoot:pluginRoot,marketplaceName:'fixture'},deepScanConfigPath:path.join(f.state,'deep-config.json')}),
  createCodex:()=>({startThread:()=>{started++;throw new Error('fixture-forbidden-new-thread')},resumeThread:(id:string)=>{resumed=id;return {id,runStreamed:async(prompt:string)=>{boundary++;assert.match(prompt,/Resume the existing Deep Scan/);throw new Error('fixture-resume-boundary')}}}}),
 });
 try{await assert.rejects(runPinnedDeep(security,f.source,{mode:'deep',outputDir:f.raw,auth:'api-key'},prepared.options),/fixture-resume-boundary/);assert.equal(resumed,f.thread);assert.equal(started,0);assert.equal(boundary,1);const db=new DatabaseSync(path.join(f.state,'workbench.sqlite3'),{readOnly:true});try{assert.equal((db.prepare('SELECT COUNT(*) AS n FROM scans').get() as any).n,1);assert.equal((db.prepare('SELECT id,continuation_thread_id FROM scans').get() as any).id,inspection.scanId)}finally{db.close()}}finally{prepared.unlock();await security.close()}
});

test('SDK rejects stopped/cancel-requested coordinators and changed owning session',async t=>{
 const f=await fixture(t);
 // Publish synthetic mutations before another SQLite process reads the file.
 // Production changes native state only through official workbench commands.
 function mutate(sql:string,...values:any[]){const db=new DatabaseSync(path.join(f.state,'workbench.sqlite3'));try{db.prepare(sql).run(...values)}finally{db.close()}}
 mutate("INSERT INTO deep_scan_runs(scan_id,schema_version,workflow_version,status,phase,workers,subagents,stop_after_no_new,max_discovery_runs,created_at,updated_at) VALUES(?,1,'deep-security-scan/v1','running','discovery',2,0,2,4,?,?)",f.registration.scanId,'2026-10-21T07:00:00Z','2026-10-21T07:00:00Z');
 const ready=await inspectDeepResume(f.claim,false);assert.equal(ready.eligible,true,ready.reason);
 for(const status of ['failed','canceled','interrupted']){mutate('UPDATE deep_scan_runs SET status=?',status);assert.equal((await inspectDeepResume(f.claim,false)).eligible,false,status)}
 mutate("UPDATE deep_scan_runs SET status='running',cancel_requested=1");assert.equal((await inspectDeepResume(f.claim,false)).eligible,false);
 mutate("UPDATE deep_scan_runs SET status='running',cancel_requested=0");assert.equal((await inspectDeepResume(f.claim,false)).eligible,true);
 mutate('UPDATE scans SET continuation_thread_id=?',randomUUID());assert.equal((await inspectDeepResume(f.claim,false)).eligible,false);
});
test('saved knowledge, original session and a complete committed result fence native continuation',async t=>{
 const f=await fixture(t),db=new DatabaseSync(path.join(f.state,'workbench.sqlite3'));t.after(()=>db.close());
 const {scanInputIdentity,saveScanKnowledge}=await import(new URL('dist/scan-inputs.js',sdk).href);const snapshot={sources:['Fixtures / Mock'],documents:{'context.md':'Fixture immutable context'}};await saveScanKnowledge(f.raw,snapshot);
 const recipe=JSON.parse((db.prepare('SELECT recipe_json FROM scans').get() as any).recipe_json);recipe.scanInputs=scanInputIdentity(undefined,snapshot);db.prepare('UPDATE scans SET recipe_json=?').run(JSON.stringify(recipe));assert.equal((await inspectDeepResume(f.claim,false)).eligible,true);
 await writeFile(path.join(f.raw,'.scan-knowledge.json'),JSON.stringify({...snapshot,documents:{'context.md':'Changed context'}}));assert.equal((await inspectDeepResume(f.claim,false)).eligible,false);await writeFile(path.join(f.raw,'.scan-knowledge.json'),JSON.stringify(snapshot));
 await mkdir(path.join(f.output,'normalized'),{recursive:true});await writeFile(path.join(f.output,'normalized/result.commit.json'),'{}');assert.equal((await inspectDeepResume(f.claim,false)).eligible,false);await rm(path.join(f.output,'normalized/result.commit.json'));
 await rm(path.join(f.state,'codex-home/sessions'),{recursive:true});assert.equal((await inspectDeepResume(f.claim,false)).eligible,false);
});
test('native budget is cumulative baseline plus the new grant; private state is backed up before increasing it',async t=>{
 const f=await fixture(t),db=new DatabaseSync(path.join(f.state,'workbench.sqlite3'));const recipe=JSON.parse((db.prepare('SELECT recipe_json FROM scans').get() as any).recipe_json);recipe.maxCostUsd=1;db.prepare('UPDATE scans SET recipe_json=?').run(JSON.stringify(recipe));db.close();const inspection=await inspectDeepResume(f.claim,false);assert.equal(inspection.eligible,true,inspection.reason);
 const task={...f.task,maxCostUsd:2,executionAttempt:2,outputPath:path.join(f.root,'scans/17/attempt-2'),deepResume:{schemaVersion:1,execution:{taskId:'17',attempt:2},previous:f.claim.execution,inspection}} as ScanTask;const prepared=await prepareDeepResume(task);assert.ok(prepared);try{assert.equal(prepared.options.maxCostUsd,inspection.baseline.estimatedUsd!+2);const old=new DatabaseSync(path.join(task.outputPath,'runtime/resume-before/workbench.sqlite3'),{readOnly:true}),current=new DatabaseSync(path.join(f.state,'workbench.sqlite3'),{readOnly:true});try{assert.equal(JSON.parse((old.prepare('SELECT recipe_json FROM scans').get() as any).recipe_json).maxCostUsd,1);assert.equal(JSON.parse((current.prepare('SELECT recipe_json FROM scans').get() as any).recipe_json).maxCostUsd,prepared.options.maxCostUsd)}finally{old.close();current.close()}}finally{prepared.unlock()}
});
test('a killed guard owner releases the OS lock without deleting the checkpoint',async t=>{
 const f=await fixture(t),file=path.join(f.output,'runtime/.deep-resume-guard.sqlite');const child=spawn(process.execPath,['--input-type=module','--eval',`import {DatabaseSync} from 'node:sqlite';const db=new DatabaseSync(process.argv[1]);db.exec('BEGIN IMMEDIATE');process.stdout.write('locked\\n');setInterval(()=>{},1000)`,file],{stdio:['ignore','pipe','pipe']});t.after(()=>{if(child.exitCode===null)child.kill('SIGKILL')});await new Promise<void>((resolve,reject)=>{child.once('error',reject);child.stdout.once('data',()=>resolve());child.once('exit',()=>reject(new Error('guard fixture exited before lock')))});await assert.rejects(acquireDeepGuard(f.output),/占用/);const exited=new Promise(resolve=>child.once('exit',resolve));child.kill('SIGKILL');await exited;const release=await acquireDeepGuard(f.output);release();assert.equal((await inspectDeepResume(f.claim,false)).eligible,true);
});

test('macOS system aliases preserve native identity while task-owned directory links are rejected',async t=>{
 if(process.platform!=='darwin'){t.skip('macOS system path aliases');return}
 const f=await fixture(t);const alias=(v:string)=>v.replace(/^\/private\/(var|tmp)(\/|$)/,'/$1$2');const logicalSource=alias(f.source),logicalOutput=alias(f.output);process.env.CODEX_SECURITY_STATE_DIR=alias(f.state);
 const observed=await inspectDeepResume({...f.claim,sourcePath:logicalSource,outputPath:logicalOutput},false);assert.equal(observed.eligible,true,observed.reason);
 const link=path.join(f.root,'task-owned-link');await symlink(path.dirname(f.output),link,'dir');assert.equal((await inspectDeepResume({...f.claim,outputPath:path.join(link,'attempt-1')},false)).eligible,false);
});
