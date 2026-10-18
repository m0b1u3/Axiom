import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import ts from 'typescript';
const source=await readFile(new URL('../src/utils/sdk-lifecycle.ts',import.meta.url),'utf8');
const code=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText;
const {sdkLifecycleView}=await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
const fixture=JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/sdk-lifecycle.json',import.meta.url),'utf8'));
test('Web preserves unverified authentication, denied trust and an explicit zero estimate',()=>{
 const result=sdkLifecycleView(fixture);assert.equal(result.invalid,false);assert.equal(result.value.authentication.verified,false);
 assert.equal(result.value.trustedAccess,'not_granted');assert.equal(result.value.budgetApproaching.estimatedUsd,0);
 assert.deepEqual(sdkLifecycleView(undefined),{value:null,invalid:false});
});
test('Web reports incompatible lifecycle data without losing other progress',()=>{
 for(const mutate of [v=>v.schemaVersion=2,v=>v.authentication.verified=true,v=>v.trustedAccess='approved',v=>v.budgetApproaching.action='raise_limit',v=>v.budgetApproaching.estimatedUsd=-1,v=>delete v.scanStarted]){
  const v=structuredClone(fixture);mutate(v);assert.deepEqual(sdkLifecycleView(v),{value:null,invalid:true});
 }
});
