import {test,type TestContext} from 'node:test';import assert from 'node:assert/strict';
import {mkdtemp,mkdir,writeFile,readFile,rm} from 'node:fs/promises';import {tmpdir} from 'node:os';import path from 'node:path';
import {decode} from '@axiom/contracts';import {SDKSessionJournal} from '../src/runtime/sdk-session-journal.js';import {runSDKOperation} from '../src/engine/sdk-operation.js';import type {ScanTask} from '../src/contract/scan-task.js';

async function scratch(t:TestContext) {const root=await mkdtemp(path.join(tmpdir(),'axiom-sdk-operation-'));t.after(()=>rm(root,{recursive:true,force:true}));return root;}
test('all SessionEvent types and nested fields persist in order; credentials are redacted',async t=>{
 const root=await scratch(t),execution={taskId:'42',attempt:2};process.env.AXIOM_FIXTURE_API_KEY='synthetic-session-credential';t.after(()=>delete process.env.AXIOM_FIXTURE_API_KEY);
 const journal=new SDKSessionJournal(execution,root);
 for(let i=0;i<73;i++)journal.record({threadId:'t-'+i,parentThreadId:null,worker:0,event:{type:i%2?'response_item':'unknown_future_event',payload:{text:'synthetic-session-credential',authorization:'Bearer fixture-private-value',nested:{source:'fixture'}}}});
 const summary=journal.close();assert.equal(summary.state,'complete');assert.equal(summary.persistedCount,73);
 const data=await readFile(path.join(root,'logs/sdk-session-events.jsonl'),'utf8');assert.doesNotMatch(data,/synthetic-session-credential|fixture-private-value/);
 const entries=data.trimEnd().split('\n').map(raw=>decode('SDKSessionEvent',raw,execution));assert.equal(entries[72]!.sequence,73);assert.equal(entries[0]!.event['parentThreadId'],null);
});
test('journal capacity and creation faults preserve explicit omitted counts',async t=>{
 const root=await scratch(t),execution={taskId:'42',attempt:1},value={threadId:'t',event:{type:'fixture',payload:'a'.repeat(1024)}};
 const journal=new SDKSessionJournal(execution,root,1800);journal.record(value);journal.record(value);journal.record(value);const summary=journal.close();assert.equal(summary.state,'incomplete');assert.equal(summary.persistedCount,1);assert.equal(summary.omittedCount,2);decode('SDKSessionSummary',JSON.stringify(summary),execution);
 const blocked=path.join(root,'blocked');await writeFile(blocked,'fixture');const broken=new SDKSessionJournal(execution,blocked);broken.record(value);assert.equal(broken.close().omittedCount,1);
});
test('pinned SDK native preflight is local and keeps authentication unverified',async t=>{
 const root=await scratch(t),source=path.join(root,'source'),output=path.join(root,'output');await mkdir(source);await writeFile(path.join(source,'fixture.py'),'# Fixture only\n');
 const names=['CODEX_SECURITY_STATE_DIR','CODEX_HOME','OPENAI_API_KEY','CODEX_API_KEY','CHATGPT_ACCESS_TOKEN','CHATGPT_ACCOUNT_ID'] as const;const previous=names.map(n=>[n,process.env[n]] as const);for(const n of names)delete process.env[n];process.env.CODEX_SECURITY_STATE_DIR=path.join(output,'runtime/sdk');t.after(()=>{for(const [n,v] of previous){if(v===undefined)delete process.env[n];else process.env[n]=v;}});
 const task:ScanTask={taskId:'42',executionAttempt:1,resultSchemaVersion:1,progressSchemaVersion:2,eventSchemaVersion:2,sourcePath:source,outputPath:output,provider:'openai',connectionType:'openai',model:'fixture-local-model',scanMode:'standard',timeoutMinutes:1,mock:true,resolvedModel:{providerId:'openai',connectionType:'openai',model:'fixture-local-model',wireApi:'responses',credentialEnv:'OPENAI_API_KEY',configurationRevision:'fixture'},sdkOperation:{schemaVersion:1,execution:{taskId:'42',attempt:1},kind:'preflight',origin:null,findingId:null,finding:null,acknowledgeUnknownCost:false}};
 const result=await runSDKOperation(task,new AbortController().signal);assert.equal(result.status,'completed',result.error?.message);assert.equal((result.sdkOperation!.preflight!.authentication as {verified:boolean}).verified,false);assert.equal(result.cost?.estimatedUsd,0);assert.match(JSON.stringify(result.sdkOperation!.preflight),/\$SOURCE/);
 const archived=decode('ScanResult',await readFile(path.join(output,'normalized/result.json')),task.sdkOperation!.execution);assert.ok(archived.extensions?.sdkOperation);assert.equal(archived.extensions?.sdkNative,undefined);
});
test('mock validation cannot fall through to the non-mock public validate API',async t=>{
 const root=await scratch(t),source=path.join(root,'source');await mkdir(source);
 const task:ScanTask={taskId:'43',executionAttempt:1,resultSchemaVersion:1,sourcePath:source,outputPath:path.join(root,'output'),provider:'openai',model:'fixture-model',scanMode:'standard',timeoutMinutes:1,mock:true,resolvedModel:{providerId:'openai',connectionType:'openai',model:'fixture-model',wireApi:'responses',credentialEnv:'OPENAI_API_KEY',configurationRevision:'fixture'},sdkOperation:{schemaVersion:1,execution:{taskId:'43',attempt:1},kind:'validate',origin:{taskId:'42',attempt:1},findingId:'1',finding:{title:'Fixture only'},acknowledgeUnknownCost:true}};
 const result=await runSDKOperation(task,new AbortController().signal);assert.equal(result.status,'completed');assert.equal(result.sdkOperation!.cost.estimatedUsd,null);assert.equal(result.sdkOperation!.validation!.disposition,'deferred');assert.match(result.sdkOperation!.validation!.report,/Fixtures \/ Mock/);assert.equal(result.cost,undefined);
});
