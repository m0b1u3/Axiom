import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {mkdtemp,rm,readFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {createHash} from 'node:crypto';
import {validationJSONRequest,validationOutputMode} from '../src/proxy/validation-output-mode.js';
import {startReliableUpstream} from '../src/proxy/reliable-upstream.js';
import type {TurnDiagnostic} from '../src/proxy/turn-diagnostic.js';
const {format}=JSON.parse(await readFile(new URL('../../contracts/fixtures/sdk050-standalone-validation-format.json',import.meta.url),'utf8'));
const request={model:'fixture-only',instructions:'Original security boundary.',input:[{role:'user',content:'Fixtures / Mock assessment'}],text:{format,verbosity:'low'},tools:[{type:'function',name:'read_fixture',parameters:{type:'object'}}],max_output_tokens:4096,reasoning:{effort:'high'},stream:true};
const digest=(raw:string|Buffer)=>createHash('sha256').update(raw).digest('hex');
test('output mode requires an explicit recognized value; native remains the default',()=>{
 assert.equal(validationOutputMode(undefined),'native_schema');assert.equal(validationOutputMode('native_schema'),'native_schema');assert.equal(validationOutputMode('json_object'),'json_object');
 for(const v of ['', 'auto','JSON','json','typo'])assert.throws(()=>validationOutputMode(v),/AXIOM_VALIDATION_OUTPUT_MODE/);
 const raw=Buffer.from(JSON.stringify(request,null,2));assert.equal(validationJSONRequest(raw,'native_schema'),raw);
});
for(const protocol of ['responses'] as const)test(`${protocol}: compatibility changes only format and JSON instructions`,()=>{
 const original=structuredClone(request);
 const raw=Buffer.from(JSON.stringify(original)),result=JSON.parse(validationJSONRequest(raw,'json_object').toString());
 assert.deepEqual(result.text.format,{type:'json_object'});
 const prompt=result.instructions;assert.ok(prompt.includes(JSON.stringify(format.schema)));assert.match(prompt,/full Markdown/);assert.match(prompt,/\\n/);
 assert.ok(prompt.startsWith(original.instructions));result.instructions=original.instructions;result.text.format=original.text.format;
 assert.deepEqual(result,original);assert.deepEqual(JSON.parse(raw.toString()),original);
});
test('unknown or strengthened schemas fail closed; tool turns without format pass unchanged',()=>{
 for(const bad of [{...format,strict:false},{type:'json_object'},{...format,schema:{...format.schema,properties:{...format.schema.properties,report:{type:'string',minLength:2}}}},{...format,schema:{...format.schema,pattern:'unassessed'}}])assert.throws(()=>validationJSONRequest(Buffer.from(JSON.stringify({text:{format:bad}})),'json_object'),/recognized/);
 for(const request of [{input:[]},{text:{format:{type:'text'}},input:[]}]){const raw=Buffer.from(JSON.stringify(request));assert.equal(validationJSONRequest(raw,'json_object'),raw);}
});
for(const protocol of ['responses'] as const)for(const output of ['complete','collapsed','empty','missing','extra','wrong_disposition','invalid_json','interrupted','http_error'] as const)test(`isolated ${protocol} gateway: ${output}, no replay or output repair`,async()=>{
 const scratch=await mkdtemp(path.join(tmpdir(),'axiom-json-mode-')),receipts:TurnDiagnostic[]=[];let calls=0,wire='';
 const report=output==='collapsed'?'## Assessment Rationale: body glued to heading':output==='empty'?'   ':'## Fixtures / Mock\n\n`src/query.ts:2` contains static evidence. No runtime reproduction. 中文 \\"quoted\\"';
 const content=output==='invalid_json'?'{':JSON.stringify({...output==='missing'?{}:{report},disposition:output==='wrong_disposition'?'verified':'reportable',...output==='extra'?{extra:true}:{}});
 const upstream=createServer(async(req,res)=>{calls++;for await(const part of req)wire+=part;const body=JSON.parse(wire);assert.deepEqual(body.text.format,{type:'json_object'});
  if(output==='http_error'){res.writeHead(502,{'content-type':'application/json'});res.end('{"error":{"message":"Fixtures / Mock failure"}}');return;}
  res.writeHead(200,{'content-type':'text/event-stream'});
  {res.write('data: '+JSON.stringify({type:'response.output_text.delta',delta:content})+'\n\n');if(output!=='interrupted')res.write('data: '+JSON.stringify({type:'response.completed',response:{id:'fixture',status:'completed',output:[{type:'message',role:'assistant',content:[{type:'output_text',text:content}]}]}})+'\n\n');}
  res.end();
 });await new Promise<void>(r=>upstream.listen(0,'127.0.0.1',r));
 const proxy=await startReliableUpstream(`http://127.0.0.1:${(upstream.address() as any).port}/v1`,path.join(scratch,'gate.sqlite'),()=>{},v=>receipts.push(v),'json_object');

 try{const response=await fetch(proxy.baseUrl+'/responses',{method:'POST',body:JSON.stringify(request)}),raw=await response.text();assert.equal(calls,1);
  if(output==='http_error'){assert.equal(response.status,502);}else if(output==='interrupted'){assert.match(raw,/response.failed/);assert.doesNotMatch(raw,/response.completed/);}else{const completed=raw.split('\n').filter(s=>s.startsWith('data: ')).map(s=>JSON.parse(s.slice(6))).find(v=>v.type==='response.completed');assert.equal(completed.response.output[0].content[0].text,content,'SDK must receive the exact raw result for strict local validation');}
  assert.equal(receipts.length,1);assert.equal(receipts[0]!.request.format,'json_object');assert.equal(receipts[0]!.request.sha256,digest(wire));assert.doesNotMatch(JSON.stringify(receipts),/Original security|Fixtures|quoted/);
 }finally{await proxy.close();upstream.closeAllConnections();await new Promise<void>(r=>upstream.close(()=>r()));await rm(scratch,{recursive:true,force:true});}
});
test('unsupported schema is rejected before contacting any upstream',async()=>{
 const scratch=await mkdtemp(path.join(tmpdir(),'axiom-json-reject-'));let calls=0;
 const up=createServer((_req,res)=>{calls++;res.end();});await new Promise<void>(r=>up.listen(0,'127.0.0.1',r));const proxy=await startReliableUpstream(`http://127.0.0.1:${(up.address() as any).port}`,path.join(scratch,'gate.sqlite'),()=>{},undefined,'json_object');
 try{const response=await fetch(proxy.baseUrl+'/responses',{method:'POST',body:JSON.stringify({text:{format:{type:'json_schema',schema:{type:'object'}}}})});assert.equal(response.status,400);assert.equal((await response.json() as any).error.code,'validation_output_contract_unsupported');assert.equal(calls,0);}finally{await proxy.close();await new Promise<void>(r=>up.close(()=>r()));await rm(scratch,{recursive:true,force:true});}
});
