import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {mkdtemp,rm,readFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {decode,validate} from '@axiom/contracts';
import {ResponseUsageReceipts,sdkExecutionUsage} from '../src/runtime/execution-token-usage.js';
import {startReliableUpstream} from '../src/proxy/reliable-upstream.js';
const execution={taskId:'42',attempt:1};
const response=(id:string,input=100)=>({id,status:'completed',usage:{input_tokens:input,input_tokens_details:{cached_tokens:50},output_tokens:20,output_tokens_details:{reasoning_tokens:10},total_tokens:input+20}});
test('SDK cumulative usage remains separate from pricing and missing counts remain unknown',()=>{
 const v=sdkExecutionUsage(execution,{input_tokens:100,output_tokens:20,cached_input_tokens:50,cache_write_input_tokens:0,cache_write_input_tokens_reported:false,reasoning_output_tokens:10,total_tokens:120},true)!;
 validate('ExecutionTokenUsage',v,execution);
 assert.equal(v.cacheWriteInputTokens,null);assert.equal(v.inputTokens,100);assert.equal(v.outputTokens,20);assert.equal(v.requestCount,null);
});
test('Responses receipts deduplicate identical response ids and count reasoning inside output',()=>{
 const tracker=new ResponseUsageReceipts();tracker.begin().observe(response('fixture-1'));tracker.begin().observe(response('fixture-1'));tracker.begin().observe(response('fixture-2',200));
 const v=tracker.summary(execution,true)!;validate('ExecutionTokenUsage',v,execution);
 assert.equal(v.inputTokens,300);assert.equal(v.outputTokens,40);assert.equal(v.cachedInputTokens,100);assert.equal(v.totalTokens,340);assert.equal(v.cacheWriteInputTokens,null);assert.equal(v.requestCount,3);assert.equal(v.complete,true);
});
test('missing, incomplete and conflicting receipts cannot settle a complete cost',()=>{
 const missing=new ResponseUsageReceipts();missing.begin().observe(response('fixture-1'));missing.begin();
 const a=missing.summary(execution,true)!;assert.equal(a.complete,false);assert.equal(a.inputTokens,100);
 const incomplete=new ResponseUsageReceipts();incomplete.begin().observe({...response('fixture-incomplete'),status:'incomplete'});assert.equal(incomplete.summary(execution,true)!.complete,false);
 const conflict=new ResponseUsageReceipts();conflict.begin().observe(response('fixture-same',100));conflict.begin().observe(response('fixture-same',200));const b=conflict.summary(execution,true)!;assert.equal(b.complete,false);assert.equal(b.inputTokens,null);assert.equal(b.outputTokens,null);
});
test('invalid optional token metadata becomes unknown without discarding a valid scan report',()=>{
 const invalid=sdkExecutionUsage(execution,{input_tokens:10,cached_input_tokens:20,output_tokens:1,total_tokens:11},true)!;
 validate('ExecutionTokenUsage',invalid,execution);assert.equal(invalid.complete,false);assert.equal(invalid.inputTokens,null);
 const tracker=new ResponseUsageReceipts();tracker.begin().observe({...response('fixture-invalid'),usage:{input_tokens:10,input_tokens_details:{cached_tokens:20},output_tokens:1,total_tokens:11}});
 const v=tracker.summary(execution,true)!;validate('ExecutionTokenUsage',v,execution);assert.equal(v.complete,false);assert.equal(v.inputTokens,null);
});
test('native Responses + strict Schema forwarding observes usage without changing requests or replaying failures',async()=>{
 const root=await mkdtemp(path.join(tmpdir(),'axiom-token-receipts-'));
 const native=JSON.parse(await readFile(new URL('../../contracts/fixtures/sdk050-standalone-validation-format.json',import.meta.url),'utf8'));
 const request={model:'fixture-deepseek',input:[{type:'message',role:'user',content:[{type:'input_text',text:'Fixtures / Mock only'}]}],stream:true,text:{format:native.format}};
 const received:unknown[]=[];let calls=0;
 const upstream=createServer(async(req,res)=>{
  const chunks:Buffer[]=[];for await(const chunk of req)chunks.push(chunk);received.push(JSON.parse(Buffer.concat(chunks).toString()));calls++;
  if(calls===3){res.writeHead(503,{'content-type':'application/json'});res.end('{"error":{"code":"fixture_error","message":"Fixtures / Mock outage"}}');return}
  res.writeHead(200,{'content-type':'text/event-stream'});const value={type:'response.completed',response:{...response('fixture-response-'+calls),output:[{type:'message',content:[{type:'output_text',text:JSON.stringify({disposition:'deferred',report:'Fixtures / Mock complete report'})}]}]}};
  res.end('event: response.completed\ndata: '+JSON.stringify(value)+'\n\n');
 });
 await new Promise<void>(resolve=>upstream.listen(0,'127.0.0.1',resolve));
 const proxy=await startReliableUpstream('http://127.0.0.1:'+(upstream.address() as import('node:net').AddressInfo).port,path.join(root,'gate.sqlite'));
 try{
  for(let n=0;n<2;n++){const result=await fetch(proxy.baseUrl+'/responses',{method:'POST',body:JSON.stringify(request)});assert.equal(result.status,200);await result.text()}
  assert.deepEqual(received,[request,request]);const complete=proxy.tokenUsage!(execution,true)!;validate('ExecutionTokenUsage',complete,execution);assert.equal(complete.complete,true);assert.equal(complete.inputTokens,200);assert.equal(complete.outputTokens,40);
  const failed=await fetch(proxy.baseUrl+'/responses',{method:'POST',body:JSON.stringify(request)});assert.equal(failed.status,503);await failed.text();
  const partial=proxy.tokenUsage!(execution,false)!;assert.equal(calls,3);assert.equal(partial.complete,false);assert.equal(partial.inputTokens,200);assert.equal(partial.requestCount,3);
  const snapshot=decode('ExecutionTokenUsage',JSON.stringify(partial),execution);assert.equal(snapshot.source,'responses_usage');
 }finally{await proxy.close();await new Promise<void>(resolve=>upstream.close(()=>resolve()));await rm(root,{recursive:true,force:true})}
});
