import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import ts from 'typescript';
const source=readFileSync(new URL('../src/utils/scan-result.ts',import.meta.url),'utf8');
const module=await import('data:text/javascript;base64,'+Buffer.from(ts.transpile(source,{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022})).toString('base64'));
test('unknown costs and reported model stay unknown while observed zero remains visible',()=>{
 assert.equal(module.estimatedCost(null),'未知');assert.equal(module.estimatedCost(undefined),'未知');assert.equal(module.estimatedCost(0),'$0.00');assert.equal(module.estimatedCost(1.125),'$1.125');
 assert.equal(module.reportedModel({turn:{model:'fixture-reported'}}),'fixture-reported');assert.equal(module.reportedModel({cost:null}),'未知');
});
test('process discrepancy compares the committed result, independently of execution cancellation',()=>{
 assert.equal(module.processMismatch({executionStatus:'completed',process:{code:null,signal:null},result:{status:'completed'}}),false);
 assert.equal(module.processMismatch({executionStatus:'canceled',process:{code:0,signal:null},result:{status:'completed'}}),false);
 assert.equal(module.processMismatch({process:{code:10,signal:null},result:{status:'completed'}}),true);
 assert.equal(module.processMismatch({process:{code:2,signal:null},result:{status:'partial'}}),false);
 assert.equal(module.processMismatch({process:{code:null,signal:'killed'},result:{status:'completed'}}),true);
});
test('all native cost and token fields are exposed, including observed zeros and future fields',()=>{
 const sections=module.nativeCostSections({extensions:{sdk:{cost:{cachedInputTokens:0,future:{retained:true}},turn:{usage:{reasoning_output_tokens:5,total_tokens:17}}}}});
 assert.equal(sections[0].rows[0].value,'0');assert.equal(sections[0].rows[1].value,'{"retained":true}');assert.equal(sections[1].rows[0].value,'5');
 assert.match(module.nativeCostSections({extensions:{lastObservedCost:{estimatedUsd:1}}})[0].title,/非最终账单/);
});
