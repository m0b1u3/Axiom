import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,readFile,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {decode,type SDKLifecycle} from '@axiom/contracts';
import {sdkLifecycleCallbacks} from '../src/runtime/sdk-lifecycle.js';
import {ContractProgressSink} from '../src/runtime/contract-progress.js';

test('public lifecycle callbacks retain approved budget and do not disclose credentials or host paths',async t=>{
 const dir=await mkdtemp(path.join(tmpdir(),'axiom-lifecycle-'));t.after(()=>rm(dir,{recursive:true,force:true}));
 const sink=new ContractProgressSink(dir,{taskId:'777',attempt:2});await sink.init();
 const writes:Promise<void>[]=[],states:SDKLifecycle[]=[];
 const callbacks=sdkLifecycleCallbacks((state,message,warning)=>{states.push(state);writes.push(sink.lifecycle(state,message,warning));});
 callbacks.onAuthentication!({method:'stored_credentials',credentialType:'chatgpt',verified:false});
 callbacks.onTrustedAccessStatus!('not_granted');
 callbacks.onOutputArchived!('/private/secret/previous');callbacks.onOutputDirReady!('/private/secret/new');callbacks.onScanStarted!();
 const cost={model:'fixture',estimatedUsd:0,inputTokens:0,cachedInputTokens:0,cacheWriteInputTokens:0,outputTokens:0};
 assert.equal(await callbacks.onBudgetApproaching!({maxCostUsd:2,cost,signal:new AbortController().signal}),undefined);
 await Promise.all(writes);await sink.finish('completed');await sink.drain();
 const raw=await readFile(sink.progressPath,'utf8'),snapshot=decode('ProgressSnapshotV2',raw);
 const lifecycle=decode('SDKLifecycle',JSON.stringify(snapshot.extensions!.sdkLifecycle));
 assert.equal(lifecycle.authentication!.verified,false);assert.equal(lifecycle.trustedAccess,'not_granted');
 assert.equal(lifecycle.scanStarted,true);assert.equal(lifecycle.outputReady,true);assert.equal(lifecycle.outputArchived,true);
 assert.equal(lifecycle.budgetApproaching!.estimatedUsd,0);assert.equal(lifecycle.budgetApproaching!.action,'retain_limit');
 assert.equal(states[0]!.scanStarted,false,'earlier observations must not mutate');
 const journal=await readFile(sink.eventsPath,'utf8');assert.doesNotMatch(raw+journal,/private\/secret/);
 const events=journal.trim().split('\n').map(line=>decode('ScanEventV2',line));
 assert.ok(events.some(e=>e.type==='scan.warning'&&e.message.includes('保持已批准')));
 assert.equal(snapshot.phase,'completed','lifecycle callbacks must not invent scan phases');
});
test('missing callbacks and invalid estimates never imply success or zero cost',async()=>{
 const states:SDKLifecycle[]=[];const callbacks=sdkLifecycleCallbacks(state=>states.push(state));
 callbacks.onTrustedAccessStatus!('unknown');
 assert.equal(states[0]!.authentication,null);assert.equal(states[0]!.scanStarted,false);
 await callbacks.onBudgetApproaching!({maxCostUsd:1,cost:{model:'fixture',estimatedUsd:NaN,inputTokens:0,cachedInputTokens:0,cacheWriteInputTokens:0,outputTokens:0},signal:new AbortController().signal});
 assert.equal(states.at(-1)!.budgetApproaching!.estimatedUsd,null);
});
