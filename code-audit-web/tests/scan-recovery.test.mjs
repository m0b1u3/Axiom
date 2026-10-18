import {test} from 'node:test';import assert from 'node:assert/strict';import {readFile} from 'node:fs/promises';import ts from 'typescript';
const source=await readFile(new URL('../src/utils/scan-recovery.ts',import.meta.url),'utf8');const code=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText;const {recoveryView,recoveryAction}=await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
const fixture=JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/recovery-snapshot.json',import.meta.url),'utf8'));
test('only the observed task attempt and frozen recovery version can enable actions',()=>{
 assert.ok(recoveryView(fixture,17,1,'failed'));
 for(const mutate of [v=>v.schemaVersion++,v=>v.execution.taskId='18',v=>v.execution.attempt++,v=>v.taskStatus='scanning',v=>v.sdkVersion='0.1.32',v=>v.checkpointResume.supported=true,v=>v.committedResult.sha256=null,v=>v.interruption.allowed='yes']){const v=structuredClone(fixture);mutate(v);assert.equal(recoveryView(v,17,1,'failed'),null);}
});
test('restoring a result never acknowledges reexecution or becomes resume',()=>{
 const action=recoveryAction(fixture,'restore_result');assert.equal(action.action,'restore_result');assert.equal(action.resultSha256,'a'.repeat(64));assert.equal(action.acknowledgeReexecution,false);
 const restart=recoveryAction(fixture,'restart');assert.equal(restart.acknowledgeReexecution,true);assert.equal(restart.resultSha256,null);assert.equal(restart.expectedStatus,'failed');
});
test('unknown fees, incomplete results and active leases do not enable forbidden actions',()=>{
 const v=structuredClone(fixture);v.restart.allowed=false;v.committedResult.state='missing';v.committedResult.sha256=null;assert.equal(recoveryAction(v,'restart'),null);assert.equal(recoveryAction(v,'restore_result'),null);assert.equal(recoveryAction(v,'interrupt_expired'),null);
 v.interruption.allowed=true;const action=recoveryAction(v,'interrupt_expired');assert.equal(action.acknowledgeReexecution,false);assert.equal(action.resultSha256,null);
});

test('recovered prior completion evidence remains bound to the exact attempt',async()=>{
 const v=JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/recovery-previous-completion.json',import.meta.url),'utf8'));
 assert.ok(recoveryView(v,17,1,'failed'));v.recovery.previousCompletion.execution.attempt=2;assert.equal(recoveryView(v,17,1,'failed'),null);
 assert.equal(recoveryAction(fixture,'resume'),null);
});
