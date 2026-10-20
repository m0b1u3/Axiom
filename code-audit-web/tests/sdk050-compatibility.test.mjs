import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import ts from 'typescript';
async function utility(name){const source=await readFile(new URL('../src/utils/'+name+'.ts',import.meta.url),'utf8');const code=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText;return import('data:text/javascript;base64,'+Buffer.from(code).toString('base64'));}
const {sdkUsageView}=await utility('sdk-usage');
const {coverageSections,coverageLabel}=await utility('coverage');
const {sdkNativeView,sdkCostEstimateView,sdkEstimateRange}=await utility('sdk-native');
const usage=JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/sdk050-usage.json',import.meta.url),'utf8'));
test('Web token observations retain zero and null, reject a foreign attempt, and never claim a bill',()=>{
 assert.deepEqual(sdkUsageView(usage,usage.execution),{value:usage,invalid:false});assert.equal(sdkUsageView(usage).value.outputTokens,0);assert.equal(sdkUsageView(usage).value.cacheWriteInputTokens,null);
 assert.deepEqual(sdkUsageView(undefined),{value:null,invalid:false});
 for(const change of [v=>v.sdkVersion='0.9.0',v=>v.outputTokens=-1,v=>delete v.inputTokens,v=>v.execution.attempt=3]){
  const v=structuredClone(usage);change(v);assert.deepEqual(sdkUsageView(v,usage.execution),{value:null,invalid:true});
 }
});
test('0.5 coverage retains review records; legacy completeness keeps its meaning',()=>{
 const doc={documentType:'codex-security.coverage',schemaVersion:'1.0',deferred:['Fixtures / Mock 未审查'],resolvedDeferred:['Fixtures / Mock 已审查'],openQuestions:['Fixtures / Mock 未决问题']};
 const v={sections:coverageSections(doc),label:coverageLabel(doc)};assert.match(JSON.stringify(v),/已完成的审查任务/);assert.match(JSON.stringify(v),/按审查记录核对覆盖范围/);assert.ok(!JSON.stringify(v).includes('完整覆盖'));
});
test('native result readers accept both SDK versions without accepting a mixed-version estimate',async()=>{
 for(const file of ['sdk-native-result-rich.json','sdk050-native-result.json']){
  const v=JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/'+file,import.meta.url),'utf8'));
  assert.equal(sdkNativeView(v,v.execution,[v.reports.markdown.artifact]).invalid,false);
  const bad=structuredClone(v);bad.usageEstimate.sdkVersion=v.sdkVersion==='0.1.25'?'0.5.0':'0.1.25';assert.equal(sdkNativeView(bad,v.execution,[v.reports.markdown.artifact]).invalid,true);
 }
});
test('SDK price ranges preserve an unknown upper bound and their complete raw pricing basis',async()=>{
 const v=JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/sdk050-estimate-range.json',import.meta.url),'utf8'));
 assert.equal(sdkCostEstimateView(v,v.execution).nativeEstimate.pricing.future.retained[0],null);
 assert.deepEqual(sdkEstimateRange(v),{minimum:0.00034,maximum:0.00051});v.nativeEstimate.estimatedUsdRange.max=null;assert.deepEqual(sdkEstimateRange(v),{minimum:0.00034,maximum:null});
 v.nativeEstimate.estimatedUsdRange.max=-1;assert.equal(sdkCostEstimateView(v,v.execution),null);
});
