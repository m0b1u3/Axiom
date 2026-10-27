import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createServer,type Server} from 'node:http';
import {readFile,mkdtemp,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {validationSourceCitations,validationSourceCitationInstructions} from '../src/proxy/validation-source-citations.js';
import {ResponsesInputError} from '../src/proxy/responses-input.js';
import {startReliableUpstream} from '../src/proxy/reliable-upstream.js';

const {format}=JSON.parse(await readFile(new URL('../../contracts/fixtures/sdk050-standalone-validation-format.json',import.meta.url),'utf8'));
const request={model:'fixture-only',input:[{role:'user',content:'Fixtures / Mock source review'}],text:{format},tools:[{type:'function',name:'read_fixture',parameters:{type:'object'}}],reasoning:{effort:'high'},stream:true};
const encode=(value:unknown)=>Buffer.from(JSON.stringify(value));

test('frozen review adds only the tested trusted instructions and preserves existing instructions',()=>{
  for(const instructions of [undefined,'Original security boundary.']){
    const original={...structuredClone(request),...(instructions!==undefined?{instructions}:{})};
    const raw=encode(original),result=JSON.parse(validationSourceCitations(raw,true).toString());
    assert.equal(result.instructions,[instructions,validationSourceCitationInstructions].filter(Boolean).join('\n\n'));
    delete result.instructions;
    const {instructions:_,...rest}=original as typeof original & {instructions?:string};
    assert.deepEqual(result,rest,'input, schema, tool history and generation controls must stay unchanged');
    assert.deepEqual(JSON.parse(raw.toString()),original,'the original request stays intact');
  }
});

test('other operations, tool turns and unrecognized or non-strict schemas remain untouched',()=>{
  const raw=encode(request);assert.equal(validationSourceCitations(raw,false),raw);
  for(const value of [{input:'Fixtures / Mock tool turn'},...[
    {type:'text'}, {type:'json_object'}, {...format,strict:false},
    {type:'json_schema',strict:true,schema:{type:'object'}},
  ].map(format=>({...request,text:{format}}))]){
    const raw=encode(value);assert.equal(validationSourceCitations(raw,true),raw);
  }
});

test('invalid existing instructions fail with the actual field name',()=>{
  for(const instructions of [null,{},[]])assert.throws(()=>validationSourceCitations(encode({...request,instructions}),true),error=>error instanceof ResponsesInputError&&error.path==='instructions');
});

async function close(server:Server){server.closeAllConnections();await new Promise<void>(resolve=>server.close(()=>resolve()));}

test('isolated Responses forwarding retains strict schema and exact report output without replay',async()=>{
  const scratch=await mkdtemp(path.join(tmpdir(),'axiom-source-citations-'));
  const report='## Fixtures / Mock\n\n`nested/query.ts:3` contains static evidence; no runtime reproduction.';
  const content=JSON.stringify({disposition:'suppressed',report});let calls=0;let forwarded:unknown;
  const upstream=createServer(async(req,res)=>{
    calls++;const chunks:Buffer[]=[];for await(const part of req)chunks.push(part);forwarded=JSON.parse(Buffer.concat(chunks).toString());
    res.writeHead(200,{'content-type':'text/event-stream'});
    res.end('data: '+JSON.stringify({type:'response.completed',response:{status:'completed',output:[{type:'message',content:[{type:'output_text',text:content}]}]}})+'\n\n');
  });
  await new Promise<void>(resolve=>upstream.listen(0,'127.0.0.1',resolve));
  const proxy=await startReliableUpstream(`http://127.0.0.1:${(upstream.address() as any).port}`,path.join(scratch,'gate.sqlite'),()=>{},undefined,'native_schema',true);
  try{
    const response=await fetch(proxy.baseUrl+'/responses',{method:'POST',body:JSON.stringify(request)});
    const frame=(await response.text()).split('\n').find(line=>line.startsWith('data: '))!;
    assert.equal(response.status,200);assert.equal(calls,1);
    assert.deepEqual(forwarded,{...request,instructions:validationSourceCitationInstructions});
    assert.equal(JSON.parse(frame.slice(6)).response.output[0].content[0].text,content);
  }finally{await proxy.close();await close(upstream);await rm(scratch,{recursive:true,force:true});}
});

test('citation policy cannot mask empty input, and compaction receives no report instructions',async()=>{
  const scratch=await mkdtemp(path.join(tmpdir(),'axiom-source-citations-boundary-'));let calls=0;let forwarded:unknown;
  const upstream=createServer(async(req,res)=>{
    calls++;const chunks:Buffer[]=[];for await(const part of req)chunks.push(part);forwarded=JSON.parse(Buffer.concat(chunks).toString());
    res.writeHead(200,{'content-type':'application/json'});res.end('{"output":[]}');
  });
  await new Promise<void>(resolve=>upstream.listen(0,'127.0.0.1',resolve));
  const proxy=await startReliableUpstream(`http://127.0.0.1:${(upstream.address() as any).port}`,path.join(scratch,'gate.sqlite'),()=>{},undefined,'native_schema',true);
  try{
    const response=await fetch(proxy.baseUrl+'/responses',{method:'POST',body:JSON.stringify({...request,input:[]})});
    assert.equal(response.status,400);assert.equal((await response.json() as any).error.code,'responses_input_empty');assert.equal(calls,0);
    const compact=await fetch(proxy.baseUrl+'/responses/compact',{method:'POST',body:JSON.stringify(request)});
    await compact.text();assert.equal(compact.status,200);assert.equal(calls,1);assert.deepEqual(forwarded,request);
  }finally{await proxy.close();await close(upstream);await rm(scratch,{recursive:true,force:true});}
});
