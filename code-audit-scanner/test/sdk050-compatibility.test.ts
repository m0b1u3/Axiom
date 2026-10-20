import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,mkdir,readFile,writeFile,rm,access} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {decode,type Finding} from '@axiom/contracts';
import {convertCodexResult} from '../src/converter/codex-finding-converter.js';
import {buildFindingEvidence} from '../src/converter/finding-evidence.js';
import {loadRawCodexResult} from '../src/artifact/load-raw-result.js';
import {ContractProgressSink} from '../src/runtime/contract-progress.js';
import {restoreSdkParentState} from '../src/runtime/sdk-parent-state.js';
import {sdkLifecycleCallbacks} from '../src/runtime/sdk-lifecycle.js';
import {settledSDKCost} from '../src/runtime/sdk-cost.js';
import {sdkUsageEstimate} from '../src/artifact/sdk-native-result.js';
import type {ScanTask} from '../src/contract/scan-task.js';
const execution={taskId:'42',attempt:2};
const task={taskId:'42',executionAttempt:2,resultSchemaVersion:1,provider:'openai',model:'fixture-only',sourceContentHash:'2'.repeat(64)} as ScanTask;
const meta={startedAt:'2026-10-20T00:00:00Z',finishedAt:'2026-10-20T00:01:00Z',rawArtifactDir:'raw/codex-security',normalizedResultPath:'normalized/result.json'};
const coverage={documentType:'codex-security.coverage',schemaVersion:'1.0',deferred:[{summary:'Fixtures / Mock 未审查项'}],resolvedDeferred:[{summary:'Fixtures / Mock 已完成审查'}],openQuestions:['Fixtures / Mock 未决问题'],nativeExtension:{retained:true}};
test('0.5 coverage gaps survive without manufacturing completeness or partial execution',()=>{
 const raw={manifest:{scan:{status:'completed'}},findings:{findings:[]},coverage};
 const out=convertCodexResult(task,raw,meta);assert.equal(out.status,'completed');assert.deepEqual(out.coverage,coverage);assert.equal(Object.hasOwn(out.coverage!,'completeness'),false);
 assert.equal(convertCodexResult(task,{...raw,manifest:{scan:{status:'failed'}}},meta).status,'failed');
 const old=convertCodexResult(task,{...raw,coverage:{completeness:'partial',deferred:['legacy']}},meta);assert.equal(old.status,'partial');
});
test('interrupted output retains the whole 0.5 coverage and raw evidence documents',async t=>{
 const root=await mkdtemp(path.join(tmpdir(),'axiom-sdk050-partial-'));t.after(()=>rm(root,{recursive:true,force:true}));
 await writeFile(path.join(root,'findings.json'),JSON.stringify({findings:[]}));await writeFile(path.join(root,'coverage.json'),JSON.stringify(coverage));await writeFile(path.join(root,'scan-manifest.json'),JSON.stringify({scan:{status:'running'}}));
 const recovered=await loadRawCodexResult(root);assert.deepEqual(recovered!.coverage,coverage);assert.deepEqual(recovered!.toJSON!().coverage,coverage);
 await writeFile(path.join(root,'findings.json'),'{');assert.equal(await loadRawCodexResult(root),undefined,'torn results cannot become a complete result');
});
test('canonical dataflow and reachability keep explicit evidence provenance and inference state',async()=>{
 const f=JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/evidence050-source.json',import.meta.url),'utf8')) as Finding;
 const g=buildFindingEvidence(f,task);assert.equal(g.sdkVersion,'0.5.0');assert.equal(g.adapterVersion,2);
 for(const pointer of ['/raw/rootCause','/raw/attackPath/dataflow/outcome','/raw/attackPath/reachability/entrypoint'])assert.ok(g.nodes.some(n=>n.sourcePointer===pointer),pointer);
 assert.ok(g.nodes.find(n=>n.kind==='root_cause')!.code);assert.ok(g.nodes.every(n=>n.state==='inferred'&&n.verification===null));
 assert.deepEqual(g,JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/evidence050-facts.json',import.meta.url),'utf8')));
 const unmapped=structuredClone(f);(unmapped.raw as any).rootCause={evidenceRefs:['E-source']};const gap=buildFindingEvidence(unmapped,task);
 assert.ok(gap.diagnostics.some(d=>d.code==='unmapped_claim'));const root=gap.nodes.find(n=>n.kind==='finding')!;
 assert.equal(gap.edges.filter(e=>e.kind==='supports'&&e.to===root.id).length,0,'unmapped claims must not support the whole finding');
});
test('Worker discovery is unknown until a session event; usage never manufactures a price',async t=>{
 const root=await mkdtemp(path.join(tmpdir(),'axiom-sdk050-workers-'));t.after(()=>rm(root,{recursive:true,force:true}));const sink=new ContractProgressSink(root,execution);await sink.init();
 await sink.workerStatus({kind:'dispatch',phase:'discovery',planned:3,started:1});await sink.workerObserved({kind:'observed',worker:1});
 let snapshot=decode('ProgressSnapshotV2',await readFile(sink.progressPath),execution);assert.equal(snapshot.workers[0]!.status,'unknown');assert.equal(snapshot.workerStatus!.kind,'dispatch');assert.equal(snapshot.files,null);
 await sink.session({threadId:'fixture-thread',parentThreadId:null,worker:1,event:{type:'event_msg',payload:{type:'task_started'}}});await sink.workerObserved({kind:'observed',worker:1});
 snapshot=decode('ProgressSnapshotV2',await readFile(sink.progressPath),execution);assert.equal(snapshot.workers.length,1);assert.equal(snapshot.workers[0]!.status,'running');
 await sink.usage({input_tokens:100,cached_input_tokens:20,output_tokens:0,total_tokens:100});
 snapshot=decode('ProgressSnapshotV2',await readFile(sink.progressPath),execution);let usage=decode('SDKUsage',JSON.stringify(snapshot.extensions!.sdkUsage),execution);assert.equal(usage.outputTokens,0);assert.equal(usage.cacheWriteInputTokens,null);assert.equal(snapshot.cost,null);
 await sink.cost({estimatedUsd:0.25,inputTokens:90});await sink.usage({input_tokens:120,output_tokens:-1,total_tokens:NaN,cache_write_input_tokens:0,cache_write_input_tokens_reported:false});
 snapshot=decode('ProgressSnapshotV2',await readFile(sink.progressPath),execution);usage=decode('SDKUsage',JSON.stringify(snapshot.extensions!.sdkUsage),execution);assert.equal(usage.outputTokens,null);assert.equal(usage.totalTokens,null);assert.equal(snapshot.cost!.estimatedUsd,0.25);assert.equal(usage.cacheWriteInputTokens,null);assert.equal(usage.cacheWriteInputTokensReported,false);
 await sink.finish('failed',{code:'network_error',message:'Fixtures / Mock 中断'});const last=await readFile(sink.progressPath,'utf8');await sink.workerObserved({kind:'observed',worker:9});await sink.usage({input_tokens:999});assert.equal(await readFile(sink.progressPath,'utf8'),last);
 const events=(await readFile(sink.eventsPath,'utf8')).trim().split('\n').map(s=>decode('ScanEventV2',s,execution));assert.ok(events.some(e=>e.type==='scan.usage'));assert.ok(!events.some(e=>e.type==='scan.worker_status'&&e.data.kind==='preflight'));
});
test('old private parent state remains readable but cannot be imported into 0.5',async t=>{
 const root=await mkdtemp(path.join(tmpdir(),'axiom-sdk050-old-parent-'));t.after(()=>rm(root,{recursive:true,force:true}));await mkdir(path.join(root,'runtime'));
 const old=decode('SDKParentState',await readFile(new URL('../../contracts/fixtures/v1/sdk-parent-state.json',import.meta.url),'utf8'));
 assert.equal(old.sdkVersion,'0.1.25');const next={...task,outputPath:root,sourceContentHash:old.sourceHash,scanInput:{schemaVersion:1 as const,knowledge:[],parentScanId:old.scanId},parentState:old};
 await assert.rejects(restoreSdkParentState(next),/different SDK version/);await assert.rejects(access(path.join(root,'runtime/sdk')));
});
test('host callbacks cannot masquerade as verified local authentication',()=>{
 let observed:any;const cb=sdkLifecycleCallbacks((value,_message,warning)=>{observed={value,warning}});cb.onAuthentication!({method:'host'});assert.equal(observed.value.authentication,null);assert.equal(observed.warning,true);
});
test('0.5 pricing bounds are archived and a baseline cannot settle an uncertain total',()=>{
 const estimate=sdkUsageEstimate(execution,{turn:{model:'gpt-5.6',usage:{input_tokens:0,output_tokens:17,cached_input_tokens:0,cache_write_input_tokens:0}}});
 const native=estimate.nativeEstimate!;assert.deepEqual(native.estimatedUsdRange,{min:0.00034,max:0.00051,context:'unknown'});assert.ok(native.pricing);
 assert.equal(settledSDKCost(native),null);assert.equal(settledSDKCost({estimatedUsd:0}),0);
 assert.equal(settledSDKCost({estimatedUsd:0,estimatedUsdRange:{min:0,max:0}}),0);
 for(const range of [{min:1,max:2},{min:1,max:null},{min:2,max:1},null])assert.equal(settledSDKCost({estimatedUsd:1,estimatedUsdRange:range}),null);
 const out=convertCodexResult(task,{manifest:{scan:{status:'completed'}},findings:{findings:[]},cost:native},meta);
 assert.equal(out.status,'completed');assert.equal(out.cost?.estimatedUsd,undefined);
});
test('parallel components retain distinct Worker owners and never add token receipts into a total',async t=>{
 const root=await mkdtemp(path.join(tmpdir(),'axiom-sdk050-component-events-'));t.after(()=>rm(root,{recursive:true,force:true}));const sink=new ContractProgressSink(root,execution);await sink.init();
 for(const componentIndex of [0,1])await sink.workerObserved({kind:'observed',worker:1,componentIndex});
 await sink.session({threadId:'component-zero-thread',parentThreadId:null,worker:1,componentIndex:0,event:{type:'event_msg',payload:{type:'task_started'}}});
 await sink.usage({input_tokens:5,total_tokens:5},1);
 const snapshot=decode('ProgressSnapshotV2',await readFile(sink.progressPath),execution);assert.equal(snapshot.workers.length,2);
 assert.equal(snapshot.workers.find(w=>w.worker.componentIndex===0)!.status,'running');assert.equal(snapshot.workers.find(w=>w.worker.componentIndex===1)!.status,'unknown');
 assert.equal(decode('SDKUsage',JSON.stringify(snapshot.extensions!.sdkUsage),execution).componentIndex,1);assert.equal(snapshot.cost,null);
});
