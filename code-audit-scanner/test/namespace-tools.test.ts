import {test} from 'node:test';
import assert from 'node:assert/strict';
import {NamespaceTools,NamespaceToolError} from '../src/proxy/namespace-tools.js';
import {startReliableUpstream} from '../src/proxy/reliable-upstream.js';
import {createServer} from 'node:http';
import {mkdtemp,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
const format={type:'json_schema',name:'fixture',strict:true,schema:{type:'object',properties:{report:{type:'string'}},required:['report'],additionalProperties:false}};
const tool={type:'function',name:'run',description:'Fixtures / Mock function',strict:true,parameters:{type:'object',properties:{text:{type:'string'}},required:['text'],additionalProperties:false}};
const group=(name='one',nested:unknown[]=[tool])=>({type:'namespace',name,description:'Fixtures / Mock group',tools:nested});
const raw=(v:unknown)=>Buffer.from(JSON.stringify(v));
const request=(tools:unknown[]=[group()],input:unknown[]=[])=>({model:'fixture-only',tools,input,text:{format},stream:true,store:false,parallel_tool_calls:true});

test('all namespace functions and custom tools remain exposed with stable collision-safe names',()=>{
 const mapper=new NamespaceTools(),body=request([tool,group(),group('two'),group('free',[{type:'custom',name:'run',format:{type:'grammar',syntax:'regex',definition:'[a-z]+'}}])]);
 const result=JSON.parse(mapper.prepare(raw(body)).body.toString());
 assert.equal(result.tools.length,4);assert.deepEqual(result.tools[0],tool);
 const names=result.tools.map((t:any)=>t.name);assert.equal(new Set(names).size,4);assert.ok(names.every((n:string)=>n.length<=64&&/^[A-Za-z0-9_-]+$/.test(n)));
 assert.equal(result.tools[3].type,'custom');assert.deepEqual(result.tools[3].format,body.tools[3].tools[0].format);
 assert.deepEqual(result.text,body.text);assert.deepEqual(result.tools[1].parameters,tool.parameters);assert.equal(result.tools[1].strict,true);
 assert.deepEqual(JSON.parse(mapper.prepare(raw(body)).body.toString()),result);
});

for(const kind of ['function','custom'] as const)test(`${kind} calls restore across streamed, terminal and JSON shapes without touching payloads`,()=>{
 const mapper=new NamespaceTools(),plan=mapper.prepare(raw(request([group('one',[{...tool,type:kind}])]))),name=JSON.parse(plan.body.toString()).tools[0].name;
 const item={type:kind==='function'?'function_call':'custom_tool_call',id:'item-1',call_id:'call-1',name,arguments:'{"name":"'+name+'"}',input:name,status:'completed'};
 const expected={...item,name:'run',namespace:'one'};
 for(const type of ['response.output_item.added','response.output_item.done'])assert.deepEqual(plan.restore({type,item}),{type,item:expected});
 assert.deepEqual(plan.restore({type:'response.completed',response:{output:[item]}}),{type:'response.completed',response:{output:[expected]}});
 assert.deepEqual(plan.restore({output:[item]}),{output:[expected]});
 const event=kind==='function'?'response.function_call_arguments.done':'response.custom_tool_call_input.done';
 assert.deepEqual(plan.restore({...item,type:event}),{...expected,type:event});
});

test('subsequent turns preserve call IDs, arguments, outputs and opaque reasoning; selectors use the same aliases',()=>{
 const mapper=new NamespaceTools(),first=mapper.prepare(raw(request())),alias=JSON.parse(first.body.toString()).tools[0].name;
 const input=[{type:'function_call',name:'run',namespace:'one',call_id:'call-1',arguments:'{"namespace":"one"}'},{type:'function_call_output',name:'run',namespace:'one',call_id:'call-1',output:'Fixtures / Mock result'},{type:'function_call_output',namespace:'one',call_id:'call-1',output:'raw output'},{type:'reasoning',encrypted_content:'opaque-untouched'},{type:'message',content:[{type:'input_text',text:'one.run'}]}];
 const body={...request(undefined,input),tool_choice:{type:'allowed_tools',mode:'required',tools:[{type:'function',name:'run',namespace:'one'}]}};
 const sent=JSON.parse(mapper.prepare(raw(body)).body.toString());
 assert.equal(sent.input[0].name,alias);assert.equal(sent.input[1].name,alias);assert.equal(sent.input[0].call_id,'call-1');assert.equal(sent.input[0].arguments,input[0].arguments);assert.equal(sent.input[1].output,input[1].output);assert.equal(sent.input[2].namespace,undefined);assert.deepEqual(sent.input.slice(3),input.slice(3));assert.equal(sent.tool_choice.tools[0].name,alias);assert.equal(sent.tool_choice.tools[0].namespace,undefined);
 const compact=mapper.prepare(raw({input:[input[0]]}),true);assert.deepEqual(compact.restore({output:[{type:'function_call',name:alias,call_id:'call-1',arguments:'{}'}]}),{output:[{type:'function_call',name:'run',namespace:'one',call_id:'call-1',arguments:'{}'}]});
 const ordinary=mapper.prepare(raw({input:[input[0]]}));assert.throws(()=>ordinary.restore({output:[{type:'function_call',name:alias,call_id:'call-1',arguments:'{}'}]}),NamespaceToolError);
});

test('response scopes remain isolated across concurrent requests and removed tools cannot be dispatched',()=>{
 const mapper=new NamespaceTools(),a=mapper.prepare(raw(request([group('one')]))),b=mapper.prepare(raw(request([group('two')])));
 const alias=JSON.parse(a.body.toString()).tools[0].name,item={type:'function_call',name:alias,call_id:'1',arguments:'{}'};
 assert.throws(()=>b.restore({output:[item]}),NamespaceToolError);
 assert.throws(()=>b.restore({output:[{...item,name:'run',namespace:'one'}]}),NamespaceToolError);
 assert.equal((b.restore({output:[{...item,name:'run',namespace:'two'}]}) as any).output[0].namespace,'two');
 assert.equal((a.restore({output:[item]}) as any).output[0].namespace,'one');
});

test('conflicting names, duplicate tools, unknown nested types and unknown histories fail before forwarding',()=>{
 const mapper=new NamespaceTools(),alias=JSON.parse(mapper.prepare(raw(request())).body.toString()).tools[0].name;
 for(const body of [request([group(),{...tool,name:alias}]),request([group(),group()]),request([group('bad',[{type:'web_search'}])]),request([group('empty',[])]),request([], [{type:'function_call',name:'run',namespace:'unknown'}])])assert.throws(()=>mapper.prepare(raw(body)),NamespaceToolError);
});

test('ordinary tool-free requests are byte-preserved',()=>{
 const bytes=Buffer.from('{ "model": "fixture", "input": "keep exact bytes" }');assert.strictEqual(new NamespaceTools().prepare(bytes).body,bytes);
});

for(const streaming of [true,false])test(`Responses ${streaming?'SSE':'JSON'} namespace round trip and next-turn history through the real proxy`,async()=>{
 const directory=await mkdtemp(path.join(tmpdir(),'axiom-namespace-fixture-'));let calls=0,alias='';
 const upstream=createServer(async(req,res)=>{
  const chunks=[];for await(const c of req)chunks.push(c);const body=JSON.parse(Buffer.concat(chunks).toString());calls++;
  assert.equal(body.text.format.type,'json_schema');assert.equal(body.text.format.strict,true);assert.ok(body.tools.every((t:any)=>t.type!=='namespace'));
  if(calls===1){alias=body.tools[0].name;}else{assert.equal(body.input[0].name,alias);assert.equal(body.input[0].namespace,undefined);assert.equal(body.input[1].call_id,'call-1');}
  const item=calls===1?{type:'function_call',id:'item-1',call_id:'call-1',name:alias,arguments:'{"text":"fixture"}',status:'completed'}:{type:'message',role:'assistant',content:[{type:'output_text',text:'{"report":"Fixtures / Mock complete report"}'}]};
  const response={id:'resp-fixture',status:'completed',output:[item]};
  if(streaming){res.writeHead(200,{'content-type':'text/event-stream'});const events=[{type:'response.output_item.added',item},{type:'response.output_item.done',item},{type:'response.completed',response}];for(const v of events){const bytes=Buffer.from('data: '+JSON.stringify(v)+'\r\n\r\n');for(let i=0;i<bytes.length;i+=7)res.write(bytes.subarray(i,i+7));}res.end();}
  else{res.writeHead(200,{'content-type':'application/json'});res.end(JSON.stringify(response));}
 });
 await new Promise<void>(r=>upstream.listen(0,'127.0.0.1',r));const proxy=await startReliableUpstream(`http://127.0.0.1:${(upstream.address() as any).port}/v1`,path.join(directory,'gate.sqlite'));
 const send=async(body:unknown)=>{const r=await fetch(proxy.baseUrl+'/responses',{method:'POST',body:JSON.stringify(body)});assert.equal(r.status,200);const text=await r.text();return streaming?JSON.parse(text.trim().split('\n\n').at(-1)!.split('\ndata: ').at(-1)!).response:JSON.parse(text)};
 try{const first=await send({...request(),stream:streaming});assert.equal(first.output[0].name,'run');assert.equal(first.output[0].namespace,'one');const next=await send({...request(undefined,[first.output[0],{type:'function_call_output',call_id:'call-1',output:'Fixtures / Mock return'}]),stream:streaming});assert.equal(next.output[0].type,'message');assert.equal(calls,2);}
 finally{await proxy.close();upstream.closeAllConnections();await new Promise<void>(r=>upstream.close(()=>r()));await rm(directory,{recursive:true,force:true});}
});
