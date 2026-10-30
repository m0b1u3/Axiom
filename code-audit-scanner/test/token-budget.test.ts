import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,mkdir,writeFile,rm,symlink} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {createServer,type Server} from 'node:http';
import type {ExecutionTokenUsage,ModelPricing,TokenBudgetAssignment} from '@axiom/contracts';
import {TokenBudget,budgetMicros,loadTokenBudget} from '../src/runtime/token-budget.js';
import {startReliableUpstream} from '../src/proxy/reliable-upstream.js';
import type {ScanTask} from '../src/contract/scan-task.js';
import {mapEngineError} from '../src/engine/error-mapping.js';

const execution={taskId:'42',attempt:1};
const price:ModelPricing={schemaVersion:1,currency:'USD',unit:'per_million_tokens',cacheMode:'input_only',inputPerMillion:'1',outputPerMillion:'4',cacheReadPerMillion:null,cacheWritePerMillion:null,note:'Fixtures / Mock'};
function assignment(limitMicros=1,p=price):TokenBudgetAssignment{return {schemaVersion:1,execution,limitMicros,pricingSnapshot:{schemaVersion:1,execution,modelConfigId:2,revision:7,capturedAt:'2026-10-27T00:00:00Z',provider:'deepseek',model:'deepseek-flash',pricing:p,reason:''}}}
function usage(overrides:Partial<ExecutionTokenUsage>={}):ExecutionTokenUsage{return {schemaVersion:1,execution,source:'responses_usage',complete:true,inputTokens:1,outputTokens:0,cachedInputTokens:null,cacheWriteInputTokens:null,reasoningOutputTokens:null,totalTokens:1,requestCount:1,...overrides}}
async function scratch(t:{after:(f:()=>Promise<void>)=>void}){const root=await mkdtemp(path.join(tmpdir(),'axiom-token-budget-'));t.after(()=>rm(root,{recursive:true,force:true}));return root}
async function listen(server:Server){await new Promise<void>(r=>server.listen(0,'127.0.0.1',r));const a=server.address();assert.ok(a&&typeof a==='object');return `http://127.0.0.1:${a.port}`}

test('configured prices use exact decimals, cache rules, zero prices and one upward rounding',()=>{
 assert.equal(budgetMicros(price,usage({inputTokens:1000,outputTokens:200,totalTokens:1200})),1800n);
 const cached={...price,cacheMode:'cache_read_write' as const,inputPerMillion:'2',outputPerMillion:'4',cacheReadPerMillion:'0.1',cacheWritePerMillion:'3'};
 assert.equal(budgetMicros(cached,usage({inputTokens:1000,cachedInputTokens:400,cacheWriteInputTokens:100,outputTokens:200,totalTokens:1200,reasoningOutputTokens:50})),2140n);
 assert.equal(budgetMicros({...price,inputPerMillion:'0.000001'},usage()),1n);
 assert.equal(budgetMicros({...price,inputPerMillion:'0',outputPerMillion:'0'},usage()),0n);
 assert.throws(()=>new TokenBudget(assignment(1,{...price,currency:'CNY'})),/USD/);
 assert.throws(()=>new TokenBudget(assignment(1,{...price,outputPerMillion:null})),/USD/);
 assert.throws(()=>budgetMicros(cached,usage()),/Token/);
 assert.throws(()=>budgetMicros(price,usage({totalTokens:3})),/Token/);
});

test('budget stops at the limit and unknown usage stops further requests; price snapshot stays fixed',()=>{
 const budget=new TokenBudget(assignment(2));budget.observe(usage());assert.equal(budget.error,undefined);
 budget.observe(usage({inputTokens:2,totalTokens:2}));assert.equal(budget.error?.code,'budget_exceeded');assert.throws(()=>budget.check(),/预算/);
 const missing=new TokenBudget(assignment(100));missing.observe(usage(),usage({complete:false,inputTokens:null}));assert.equal(missing.error?.code,'budget_usage_unknown');
 const frozen=structuredClone(assignment(100));const guard=new TokenBudget(frozen);assert.equal(guard.assignment.pricingSnapshot.revision,7);
 frozen.pricingSnapshot.pricing!.inputPerMillion='999';assert.equal(guard.pricing.inputPerMillion,'1');
 const regressed=new TokenBudget(assignment(100));regressed.observe(usage({inputTokens:2,totalTokens:2}));regressed.observe(usage());assert.equal(regressed.error?.code,'budget_usage_unknown');
 const uniform=new TokenBudget(assignment(100));uniform.observe(usage({cachedInputTokens:0,cacheWriteInputTokens:0}));uniform.observe(usage({inputTokens:2,totalTokens:2}));assert.equal(uniform.error,undefined);
});

for(const streaming of [false,true])for(const missing of [false,true])test(`loopback Responses ${streaming?'SSE':'JSON'} ${missing?'missing usage':'limit reached'} never forwards a second model request`,async t=>{
 const root=await scratch(t);let calls=0,received:any;
 const upstream=createServer((req,res)=>{void(async()=>{calls++;const parts=[];for await(const c of req)parts.push(c);received=JSON.parse(Buffer.concat(parts).toString());const response={id:'resp_fixture_1',status:'completed',output:[{type:'message',content:[{type:'output_text',text:'Fixtures / Mock'}]}],...(missing?{}:{usage:{input_tokens:1,output_tokens:0,total_tokens:1}})};
 if(streaming){res.writeHead(200,{'content-type':'text/event-stream'});res.end(`event: response.completed\ndata: ${JSON.stringify({type:'response.completed',response})}\n\n`)}else{res.writeHead(200,{'content-type':'application/json'});res.end(JSON.stringify(response))}})()});
 const url=await listen(upstream);t.after(()=>new Promise<void>(r=>upstream.close(()=>r())));
 const guard=new TokenBudget(assignment(1));const proxy=await startReliableUpstream(url,path.join(root,'gate.sqlite'),()=>{},undefined,'native_schema',false,guard);t.after(()=>proxy.close());
 const format={type:'json_schema',name:'fixture_report',strict:true,schema:{type:'object',properties:{report:{type:'string'}},required:['report'],additionalProperties:false}};
 const body=JSON.stringify({model:'deepseek-flash',input:'Fixtures / Mock only',text:{format}});
 const first=await fetch(proxy.baseUrl+'/responses',{method:'POST',headers:{'content-type':'application/json'},body});assert.equal(first.status,200);await first.text();
 assert.equal(guard.error?.code,missing?'budget_usage_unknown':'budget_exceeded');assert.deepEqual(received.text.format,format);
 const second=await fetch(proxy.baseUrl+'/responses',{method:'POST',headers:{'content-type':'application/json'},body});assert.equal(second.status,402);assert.match(await second.text(),/budget_/);assert.equal(calls,1);
 const totals=proxy.tokenUsage!(execution,true)!;assert.equal(totals.inputTokens,missing?null:1);assert.equal(totals.complete,!missing);
});

test('budget file binds to execution, model and private runtime directory',async t=>{
 const root=await scratch(t),runtime=path.join(root,'runtime');await mkdir(runtime);const file=path.join(runtime,'token-budget.json');await writeFile(file,JSON.stringify(assignment(10)));
 const task:ScanTask={taskId:'42',executionAttempt:1,provider:'deepseek',model:'deepseek-flash',scanMode:'standard',timeoutMinutes:1,sourcePath:root,outputPath:root,maxCostUsd:0.00001};
 await loadTokenBudget(file,task);assert.equal(task.tokenBudget!.pricingSnapshot.revision,7);
 await assert.rejects(loadTokenBudget(file,{...task,model:'another-model'}),/USD/);
 await assert.rejects(loadTokenBudget(file,{...task,executionAttempt:2}));
 const linked=path.join(root,'linked.json');await symlink(file,linked);await assert.rejects(loadTokenBudget(linked,task),/本次执行/);
});

test('an upstream rejection stays visible and is never retried behind an unknown-fee budget',async t=>{
 const root=await scratch(t);let calls=0;
 const upstream=createServer((_req,res)=>{calls++;res.writeHead(400,{'content-type':'application/json'});res.end(JSON.stringify({error:{code:'fixture_invalid',param:'input',message:'Fixtures / Mock rejection'}}))});
 const url=await listen(upstream);t.after(()=>new Promise<void>(r=>upstream.close(()=>r())));
 const guard=new TokenBudget(assignment(100));const proxy=await startReliableUpstream(url,path.join(root,'gate.sqlite'),()=>{},undefined,'native_schema',false,guard);t.after(()=>proxy.close());
 const request={method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({model:'deepseek-flash',input:'Fixtures / Mock'})};
 const first=await fetch(proxy.baseUrl+'/responses',request);assert.equal(first.status,400);assert.equal((await first.json()).error.param,'input');
 const error=mapEngineError(guard.error);assert.equal(error.status,'failed');assert.equal(error.code,'provider_error');assert.match(error.message,/HTTP 400/);
 const second=await fetch(proxy.baseUrl+'/responses',request);assert.equal(second.status,402);await second.text();assert.equal(calls,1);
});
