import assert from 'node:assert/strict';
import {test} from 'node:test';
import {createServer} from 'node:http';
import {mkdtempSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {prepareResponsesInput,ResponsesInputError,responsesInputSummary} from '../src/proxy/responses-input.js';
import {startReliableUpstream} from '../src/proxy/reliable-upstream.js';
import type {TurnDiagnostic} from '../src/proxy/turn-diagnostic.js';

const bytes=(v:unknown)=>Buffer.from(JSON.stringify(v));
const format={type:'json_schema',name:'fixture',strict:true,schema:{type:'object',properties:{report:{type:'string',minLength:1}},required:['report'],additionalProperties:false}};
const message=(role:string,text:string,id:string)=>({type:'message',role,id,content:[{type:'input_text',text}]});

test('SDK client IDs and telemetry are removed while exact texts, roles, schema and source objects remain intact',()=>{
 const body={model:'fixture',input:[message('developer','保持全部指令\n\nline 2','msg-local-1'),message('user','源码😃\r\nline 2','msg-local-2')],text:{format},tools:[{type:'function',name:'read',parameters:{type:'object'}}],reasoning:{effort:'high'},client_metadata:{private:'never forward'}};
 const original=structuredClone(body),raw=bytes(body),sent=JSON.parse(prepareResponsesInput(raw).toString());
 assert.deepEqual(body,original);
 assert.equal(sent.client_metadata,undefined);
 for(const [i,item] of sent.input.entries()){
  assert.equal(item.id,undefined);assert.equal(item.role,body.input[i]!.role);assert.deepEqual(item.content,body.input[i]!.content);
 }
 assert.deepEqual(sent.text,body.text);assert.deepEqual(sent.tools,body.tools);assert.deepEqual(sent.reasoning,body.reasoning);
 assert.deepEqual(responsesInputSummary(sent),responsesInputSummary(body));
 const normalized=prepareResponsesInput(raw);assert.strictEqual(prepareResponsesInput(normalized),normalized);
});

test('server IDs, references, tool call links, arguments and opaque replay payloads survive unchanged',()=>{
 const input=[message('user','Fixture source','msg-local'),{type:'message',role:'assistant',id:'msg-server',content:[{type:'output_text',text:'continue'}]},{type:'item_reference',id:'msg-existing-server'},{id:'msg-another-existing-server'},{type:'function_call',id:'fc-item',call_id:'call-stable',name:'read',arguments:'{"literal":"msg-local"}'},{type:'function_call_output',call_id:'call-stable',output:'Raw source\nunchanged'},{type:'reasoning',id:'rs-server',encrypted_content:'opaque-must-not-touch'}];
 const body={input,store:false,include:['reasoning.encrypted_content'],text:{format}};
 const result=JSON.parse(prepareResponsesInput(bytes(body)).toString());
 assert.deepEqual(result.input.slice(1),input.slice(1));assert.deepEqual(result.store,body.store);assert.deepEqual(result.include,body.include);
 assert.throws(()=>prepareResponsesInput(bytes({input:[input[0],{type:'item_reference',id:'msg-local'}]})),(e:unknown)=>e instanceof ResponsesInputError&&e.code==='responses_input_reference');
});

test('ordinary public inputs and continuation-only tool results remain byte-identical',()=>{
 for(const body of [{input:'Fixture text'},{input:[{role:'user',content:'Fixture text'}]},{input:[{type:'function_call_output',call_id:'call-1',output:''}]},{input:[],previous_response_id:'resp-server'},{conversation:'conv-server'},{instructions:'Fixture system context'}]){
  const raw=bytes(body);assert.strictEqual(prepareResponsesInput(raw),raw);
 }
});

test('multimodal content is preserved without manufacturing placeholder text',()=>{
 const content=[{type:'input_image',image_url:'data:image/png;base64,fixture'},{type:'input_text',text:''}];
 const result=JSON.parse(prepareResponsesInput(bytes({input:[{type:'message',role:'user',id:'msg-local',content}]})).toString());
 assert.deepEqual(result.input[0].content,content);
});

test('empty or malformed inputs stop locally with a precise field path and no text in the error',()=>{
 for(const [input,expected] of [[undefined,'input'],[' \n','input'],[[],'input'],[null,'input'],[[{role:'user',content:[]}],'input[0].content'],[[{role:'user',content:[{type:'input_text',text:'\n\t'}]}],'input[0].content'],[[{role:'user',content:[{type:'input_text',text:null}]}],'input[0].content[0]']] as const){
  assert.throws(()=>prepareResponsesInput(bytes({input})),(e:unknown)=>e instanceof ResponsesInputError&&e.path===expected&&!e.message.includes('Bearer'));
 }
});

test('input diagnostics contain bounded counts and hashes, never prompt text, IDs, URLs or metadata',()=>{
 const summary=responsesInputSummary({input:[message('user','secret fixture https://private.example/','msg-private')],client_metadata:{token:'fixture-secret'}});
 assert.equal(summary.messages,1);assert.equal(summary.nonemptyTextParts,1);assert.equal(summary.roles.user,1);
 assert.doesNotMatch(JSON.stringify(summary),/secret|https:|private|msg-/);
});

test('real HTTP transport sends normalized input once, records its shape, and preserves an upstream 400 without retries',async()=>{
 const folder=mkdtempSync(path.join(tmpdir(),'axiom-responses-input-'));let calls=0,sent:any;const receipts:TurnDiagnostic[]=[];
 const upstream=createServer(async(req,res)=>{let raw='';for await(const chunk of req)raw+=chunk;sent=JSON.parse(raw);calls++;res.writeHead(400,{'content-type':'application/json'});res.end('{"error":{"code":"1214","message":"输入不能为空","type":"upstream_error"}}');});
 await new Promise<void>(resolve=>upstream.listen(0,'127.0.0.1',resolve));
 const proxy=await startReliableUpstream(`http://127.0.0.1:${(upstream.address() as any).port}/v1`,path.join(folder,'gate.sqlite'),()=>{},r=>receipts.push(r));
 try{
  const response=await fetch(proxy.baseUrl+'/responses',{method:'POST',body:JSON.stringify({model:'fixture',input:[message('user','Fixtures / Mock source','msg-client')],text:{format},client_metadata:{private:'fixture-only'}})});
  assert.equal(response.status,400);assert.equal((await response.json() as any).error.code,'1214');assert.equal(calls,1);
  assert.equal(sent.input[0].id,undefined);assert.equal(sent.client_metadata,undefined);assert.deepEqual(sent.text,{format});
  assert.equal(receipts[0]!.request.input?.nonemptyTextParts,1);
  const rejected=await fetch(proxy.baseUrl+'/responses',{method:'POST',body:JSON.stringify({model:'fixture',input:[{role:'user',content:'  '} ]})});
  assert.equal(rejected.status,400);const error=(await rejected.json() as any).error;assert.equal(error.code,'responses_input_empty');assert.equal(error.param,'input[0].content');assert.equal(calls,1);
 }finally{await proxy.close();upstream.closeAllConnections();await new Promise<void>(resolve=>upstream.close(()=>resolve()));rmSync(folder,{recursive:true,force:true});}
});
