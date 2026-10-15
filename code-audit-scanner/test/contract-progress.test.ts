import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,readFile,rm} from 'node:fs/promises';
import os from 'node:os';import path from 'node:path';
import {decode} from '@axiom/contracts';
import {ContractProgressSink} from '../src/runtime/contract-progress.js';
const execution={taskId:'42',attempt:2};
async function fixture(fn:(sink:ContractProgressSink)=>Promise<void>){const dir=await mkdtemp(path.join(os.tmpdir(),'axiom-contract-progress-'));const sink=new ContractProgressSink(dir,execution);try{await sink.init();await fn(sink);}finally{await rm(dir,{recursive:true,force:true});}}
async function snapshot(sink:ContractProgressSink){return decode('ProgressSnapshotV2',await readFile(sink.progressPath),execution);}
async function events(sink:ContractProgressSink){return (await readFile(sink.eventsPath,'utf8')).trim().split('\n').map(s=>decode('ScanEventV2',s,execution));}
test('single writer serializes SDK callbacks and observer counts with monotonic revisions/sequences',async()=>fixture(async sink=>{
 const before=await snapshot(sink);await sink.session({threadId:'main',parentThreadId:null,event:{type:'session_meta',payload:{type:'token_count'}}});assert.equal((await snapshot(sink)).revision,before.revision);await Promise.all(Array.from({length:25},(_,n)=>sink.progress({phase:'discovery',filesCompleted:n,filesTotal:25})));
 const after=await snapshot(sink);assert.equal(after.revision,before.revision+25);assert.equal(after.files?.completed,24);
 const records=await events(sink);assert.deepEqual(records.map(e=>e.sequence),Array.from({length:records.length},(_,n)=>n+1));
 await sink.observation({phase:'discovery',phaseReportedAt:new Date().toISOString(),reportedAt:new Date().toISOString(),filesTotal:25,completed:0,total:3,unit:'review_receipts'});
 assert.equal((await snapshot(sink)).files?.completed,24);assert.equal((await snapshot(sink)).phaseCounts?.unit,'review_receipts');
}));
test('unknown stages/counters remain unknown and duplicate/stale workbench projections cannot regress a callback',async()=>fixture(async sink=>{
 const observation={phase:'new_native_phase',phaseReportedAt:new Date().toISOString(),reportedAt:new Date().toISOString(),filesTotal:0,completed:null,total:0,unit:'future_unit'};
 await sink.observation(observation);let p=await snapshot(sink);assert.equal(p.phase,'unknown');assert.equal(p.rawPhase,'new_native_phase');assert.equal(p.files?.completed,null);assert.equal(p.files?.total,0);assert.equal(p.phaseCounts?.unit,'unknown');assert.equal(p.extensions?.rawProgressUnit,'future_unit');
 await sink.observation(observation);assert.equal((await snapshot(sink)).revision,p.revision);
 await sink.progress({phase:'validation',filesCompleted:0,filesTotal:0});p=await snapshot(sink);assert.equal(p.phaseCounts,null);assert.equal(p.files?.completed,0);
 await sink.observation({...observation,reportedAt:'2026-01-01T00:00:00Z',phaseReportedAt:'2026-01-01T00:00:00Z'});assert.equal((await snapshot(sink)).phase,'validation');assert.equal((await snapshot(sink)).phaseCounts,null);
}));
test('activity and worker/error state come from structured fields; completed turns wait instead of completing the scan',async()=>fixture(async sink=>{
 await sink.activity({kind:'tool',status:'running',description:'failed tool fake text',worker:{index:0},id:'act1'});
 await sink.session({threadId:'thread-main',parentThreadId:null,worker:0,event:{type:'event_msg',payload:{type:'task_started'}}});
 await sink.session({threadId:'thread-main',parentThreadId:null,worker:0,event:{type:'event_msg',payload:{type:'task_complete'}}});
 let p=await snapshot(sink);assert.equal(p.workers.length,1);assert.equal(p.workers[0]?.status,'waiting');assert.equal(p.workers[0]?.activity?.status,'running');assert.equal(p.phase,'preparing');
 await sink.session({threadId:'child-2',parentThreadId:'thread-main',worker:2,event:{type:'event_msg',payload:{type:'task_complete',error:{code:'rate_limited',message:'fixture failure'}}}});
 p=await snapshot(sink);assert.equal(p.workers[1]?.status,'failed');assert.equal(p.error?.code,'rate_limit');assert.equal(p.phase,'preparing');
 const activity=(await events(sink)).find(e=>e.type==='scan.activity');assert.equal(activity?.level,'info');
}));
test('zero/unknown costs, reconnects and terminal failures retain evidence and cannot be reopened by late observations',async()=>fixture(async sink=>{
 assert.equal((await snapshot(sink)).cost,null);await sink.cost({estimatedUsd:0,inputTokens:0,outputTokens:0,model:'fixture-model'});assert.equal((await snapshot(sink)).cost?.estimatedUsd,0);
 await sink.reconnect(1,3,{reason:'network',retryAfterSeconds:0});await sink.warning('fixture warning');await sink.finish('timeout',{code:'timeout',message:'configured deadline exceeded'});
 const p=await snapshot(sink);assert.equal(p.phase,'timeout');assert.equal(p.error?.code,'timeout');assert.equal(p.cost?.inputTokens,0);assert.equal(p.reconnect?.retryAfterSeconds,0);
 await sink.progress({phase:'discovery',filesCompleted:4,filesTotal:4});assert.deepEqual(await snapshot(sink),p);
 const done=(await events(sink)).at(-1);assert.equal(done?.type,'scan.finished');if(done?.type==='scan.finished')assert.equal(done.data.status,'timeout');
}));
test('reused worker indexes attach later activities to the latest observed thread',async()=>fixture(async sink=>{
 for(const [thread,status] of [['old-thread','task_complete'],['new-thread','task_started']])await sink.session({threadId:thread!,parentThreadId:'parent',worker:1,event:{type:'event_msg',payload:{type:status,error:status==='task_complete'?{message:'old failure'}:undefined}}});
 await sink.activity({kind:'command',status:'running',description:'new work',worker:{index:1}});
 const p=await snapshot(sink);assert.equal(p.workers[0]?.worker.threadId,'old-thread');assert.equal(p.workers[0]?.status,'failed');assert.equal(p.workers[1]?.worker.threadId,'new-thread');assert.equal(p.workers[1]?.status,'running');assert.equal(p.workers[1]?.activity?.description,'new work');
}));
test('large activity paths stay bounded, and snapshot summaries point to complete structured event descriptions',async()=>fixture(async sink=>{
 const description='🙂'.repeat(18000);await sink.activity({id:'unicode-activity',kind:'tool',status:'running',description,paths:Array.from({length:256},()=> '路径'.repeat(2000))});
 const p=await snapshot(sink);assert.equal(p.activity?.truncated,true);assert.ok(Buffer.byteLength(JSON.stringify(p))<1024*1024-65536);
 const activity=(await events(sink)).find(e=>e.type==='scan.activity');assert.equal(activity?.type,'scan.activity');if(activity?.type==='scan.activity'){assert.equal(activity.data.truncated,true);assert.ok(activity.data.omittedPaths!>0);assert.ok(activity.data.description.length>p.activity!.description.length);assert.ok(Buffer.byteLength(JSON.stringify(activity))<2*1024*1024);}
}));
test('thread history is bounded in snapshots without changing independent-review counts or event history',async()=>fixture(async sink=>{
 await sink.deepProgress({completed:3,active:1,maximum:300});
 await Promise.all(Array.from({length:260},(_,n)=>sink.session({threadId:`fixture-thread-${n}`,parentThreadId:'parent',worker:n,event:{type:'event_msg',payload:{type:'task_started'}}})));
 const p=await snapshot(sink);assert.equal(p.workersTruncated,true);assert.ok(p.workers.length<=256);assert.ok(Buffer.byteLength(JSON.stringify(p))<1024*1024-65536);assert.equal(p.deep?.maximum,300);
 const workers=(await events(sink)).filter(e=>e.type==='scan.worker');assert.equal(workers.length,260);
}));
