import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import ts from 'typescript';
const source=await readFile(new URL('../src/utils/model-pricing.ts',import.meta.url),'utf8');
const runtime=new URL('../../contracts/ts/dist/index.js',import.meta.url).href;
const code=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText.replace(/from ['"]\.\.\/contracts['"]/g,'from '+JSON.stringify(runtime));
const {emptyModelPricing,checkedModelPricing,tokenEstimateView,pricingMoney}=await import('data:text/javascript;base64,'+Buffer.from(code).toString('base64'));
const fixture=async name=>JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/'+name+'.json',import.meta.url),'utf8'));
test('blank prices remain unknown, explicit zero and exact decimal strings survive editing',()=>{
 const empty=emptyModelPricing();assert.equal(checkedModelPricing(empty).inputPerMillion,null);assert.equal(checkedModelPricing(null),null);
 const zero={...empty,inputPerMillion:'0',outputPerMillion:'0',cacheReadPerMillion:'0.000001'};
 assert.deepEqual(checkedModelPricing(zero),zero);assert.notEqual(checkedModelPricing(zero),zero);
 for(const value of ['-1','1e3','0.0000001',1])assert.throws(()=>checkedModelPricing({...zero,inputPerMillion:value}));
});
test('historical pricing and usage bind the exact attempt and currency',async()=>{
 const original=await fixture('token-cost-estimated');assert.equal(tokenEstimateView(original,42,1).amountMicros,122000);
 for(const change of [v=>v.execution.attempt++,v=>v.pricingSnapshot.execution.attempt++,v=>v.usage.execution.attempt++,v=>v.currency='CNY',v=>v.usage.complete=false]){
  const v=structuredClone(original);change(v);assert.equal(tokenEstimateView(v,42,1),null);
 }
 assert.equal(original.pricingSnapshot.revision,1);assert.equal(original.pricingSnapshot.pricing.inputPerMillion,'1');
});
test('currency display distinguishes unknown, known zero and sub-cent estimates',()=>{
 assert.equal(pricingMoney(null,'USD'),'未知');assert.equal(pricingMoney(0,'CNY'),'CNY 0');
 assert.equal(pricingMoney(1,'USD'),'USD 0.000001');assert.equal(pricingMoney(122000,'EUR'),'EUR 0.122');
});
