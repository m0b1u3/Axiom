import {test} from 'node:test';import assert from 'node:assert/strict';import {readFile} from 'node:fs/promises';import ts from 'typescript';
const source=await readFile(new URL('../src/utils/deep-resume.ts',import.meta.url),'utf8');const code=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText;const {deepResumeView,deepResumeAction}=await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
const fixture=JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/deep-resume-snapshot.json',import.meta.url),'utf8'));
test('only exact source, execution, native version and eligible original session enable Deep continuation',()=>{
 assert.ok(deepResumeView(fixture,17,1,'failed','a'.repeat(64)));
 for(const change of [v=>v.schemaVersion++,v=>v.execution.attempt++,v=>v.inspection.execution.taskId='18',v=>v.inspection.origin.attempt=2,v=>v.inspection.sdkVersion='0.6.0',v=>v.inspection.agentId='',v=>v.inspection.sourceHash='b'.repeat(64),v=>v.inspection.proofHash=null,v=>v.inspection.baseline.estimatedUsd=-1,v=>v.inspection.baseline.usage.inputTokens=-1,v=>v.inspection.baseline.usage={},v=>v.canCheck=false,v=>v.state='blocked']){const v=structuredClone(fixture);change(v);assert.equal(deepResumeView(v,17,1,'failed','a'.repeat(64)),null)}
 assert.equal(deepResumeView(fixture,17,1,'scanning','a'.repeat(64)),null);assert.equal(deepResumeView(fixture,17,1,'canceled','a'.repeat(64)),null);
});
test('unknown cost remains unknown and action never masquerades as full restart',()=>{
 const v=structuredClone(fixture);v.inspection.baseline={estimatedUsd:null,usage:null};assert.ok(deepResumeView(v,17,1,'timeout','a'.repeat(64)));const a=deepResumeAction(v);assert.equal(a.acknowledgeAdditionalCost,true);assert.equal(a.probeId,'fixture-probe');assert.equal('action' in a,false);v.canResume=false;assert.equal(deepResumeAction(v),null);
});
test('continued history and pending admission retain origin and previous attempt identity',async()=>{
 const a=JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/deep-resume-assignment.json',import.meta.url),'utf8'));
 const v={...structuredClone(fixture),execution:{taskId:'17',attempt:2},state:'used',canResume:false,canCheck:false,inspection:null,continuation:a};assert.ok(deepResumeView(v,17,2,'completed','a'.repeat(64)));v.continuation.previous.taskId='18';assert.equal(deepResumeView(v,17,2,'completed','a'.repeat(64)),null);
});
