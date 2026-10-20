import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import ts from 'typescript';
const js=ts.transpileModule(await readFile(new URL('../src/utils/scan-input.ts',import.meta.url),'utf8'),{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText;
const {scanInput,scopePaths,optionalBudget}=await import('data:text/javascript;base64,'+Buffer.from(js).toString('base64'));
const form={snapshotId:7,modelConfigId:42,projectId:3,mode:'standard',timeoutMinutes:47,budget:'4.25',workers:'',includeText:'src/a,b.ts\r\n源码/测试.ts',focus:['authz'],validationMode:'exploitability',businessContext:'普通用户可注册\n第二行背景',deepMaxDiscoveryRuns:'',deepStopAfterNoNew:''};
test('Web input keeps model, budget, Unicode scope, commas and multiline context',()=>{
 assert.deepEqual(scopePaths(form.includeText),['src/a,b.ts','源码/测试.ts']);
 assert.deepEqual(scanInput(form),{sourceSnapshotId:7,modelConfigId:42,projectId:3,scanMode:'standard',timeoutMinutes:47,maxCostUsd:4.25,includePaths:['src/a,b.ts','源码/测试.ts'],scanFocus:['authz'],validationMode:'exploitability',businessContext:form.businessContext});
});
test('unset budget stays null; unset worker and deep counts stay absent',()=>{
 const input=scanInput({...form,mode:'deep',validationMode:'default',budget:''});
 assert.equal(input.maxCostUsd,null);
 for(const key of ['workers','deepMaxDiscoveryRuns','deepStopAfterNoNew','businessContext'])assert.equal(Object.hasOwn(input,key),false);
 assert.deepEqual(scanInput({...form,mode:'deep',validationMode:'default',workers:'3',deepMaxDiscoveryRuns:'8',deepStopAfterNoNew:'2'}).workers,3);
});
test('invalid budgets, workers and scopes fail before a Web request is made',()=>{
 for(const budget of ['0','-1','NaN','Infinity','1e400'])assert.throws(()=>optionalBudget(budget),/预算/);
 assert.throws(()=>optionalBudget(0),/预算/);assert.equal(optionalBudget(4.25),4.25);
 for(const workers of ['0','-1','1.5','9007199254740992','Infinity','NaN'])assert.throws(()=>scanInput({...form,mode:'deep',validationMode:'default',workers}),/Worker/);
 for(const includeText of ['/etc/passwd','C:\\repo','../outside','src/../outside','a\na'])assert.throws(()=>scanInput({...form,includeText}),/范围/);
});
test('task budget is separate from the single-run cap and rejects unknown/oversized precision',()=>{
 const input=scanInput({...form,totalBudget:'5'});assert.equal(input.maxCostUsd,4.25);assert.equal(input.maxTotalCostUsd,5);
 assert.equal(Object.hasOwn(scanInput({...form,totalBudget:''}),'maxTotalCostUsd'),false);
 for(const totalBudget of ['0','-1','Infinity','NaN','0.0000001','1e-200','1.1234567','9007199255'])assert.throws(()=>scanInput({...form,totalBudget}),/任务总预算/);
 assert.throws(()=>scanInput({...form,totalBudget:'5',budget:'0.00000001'}),/单次上限/);
 assert.equal(scanInput({...form,totalBudget:'0.000001',budget:''}).maxTotalCostUsd,0.000001);
});
test('SDK native safe integers replace platform Worker and discovery ceilings',()=>{
 for(const value of ['65','1001','9007199254740991']){
  const v=scanInput({...form,mode:'deep',validationMode:'default',workers:value,deepMaxDiscoveryRuns:value,deepStopAfterNoNew:value});
  assert.equal(v.workers,Number(value));assert.equal(v.deepMaxDiscoveryRuns,Number(value));assert.equal(v.deepStopAfterNoNew,Number(value));
  assert.equal(v.timeoutMinutes,47);assert.equal(v.maxCostUsd,4.25);assert.deepEqual(v.includePaths,['src/a,b.ts','源码/测试.ts']);
 }
 for(const value of ['0','-1','1.5','9007199254740992','Infinity','NaN'])for(const key of ['deepMaxDiscoveryRuns','deepStopAfterNoNew'])assert.throws(()=>scanInput({...form,mode:'deep',validationMode:'default',[key]:value}));
});
