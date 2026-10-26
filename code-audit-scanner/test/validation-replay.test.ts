import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {createServer,type ServerResponse} from 'node:http';
import {mkdtemp,readFile,rm} from 'node:fs/promises';
import path from 'node:path';
import {tmpdir} from 'node:os';
import {startReliableUpstream} from '../src/proxy/reliable-upstream.js';
import {SseParser} from '../src/proxy/sse.js';
import {UpstreamTurnDiagnostic,ValidationTransportJournal,type TurnDiagnostic} from '../src/proxy/turn-diagnostic.js';
const digest=(v:string|Uint8Array)=>createHash('sha256').update(v).digest('hex');
const responses=[
 {name:'retained-review-11',disposition:'reportable',report:"## SQL Injection in `lookup()` (`a036597803b7bd848456a8ddfe314614ae901dbc6baad40631c55f7b77a44ae0` — 用户输入直接拼接进 SQL 语句)"},
 {name:'retained-review-9',disposition:'reportable',report:'## 静态发现评估：用户输入直接拼接进 SQL 语句（CWE-89）'},
 {name:'retained-review-10',disposition:'not_applicable',report:'## Static Finding Assessment — 用户输入直接拼接进 SQL 语句 (CWE-89)'},
 {name:'fixture-complete-report',disposition:'reportable',report:'## Fixtures / Mock assessment\n\n`src/query.ts:2-4` concatenates untrusted input.\n\n## Evidence\n\n'+('Fixtures / Mock evidence, not runtime reproduction. 中文证据 🧪\n'.repeat(300))},
];
const schema={type:'object',properties:{disposition:{type:'string',enum:['reportable','suppressed','not_applicable','deferred']},report:{type:'string',minLength:1}},required:['disposition','report'],additionalProperties:false};
function output(text:string){return {type:'message',id:'fixture-message',role:'assistant',status:'completed',content:[{type:'output_text',text,annotations:[]}]};}
function stream(text:string){
 const parts=[...text],half=Math.floor(parts.length/2),item=output(text);
 return [{type:'response.created',response:{id:'fixture-response',status:'in_progress',output:[]}},... [parts.slice(0,half).join(''),parts.slice(half).join('')].map(delta=>({type:'response.output_text.delta',item_id:'fixture-message',output_index:0,content_index:0,delta})),{type:'response.output_text.done',text},{type:'response.output_item.done',item},{type:'response.completed',response:{id:'fixture-response',status:'completed',output:[item]}}].map(v=>'data: '+JSON.stringify(v)+'\r\n\r\n').join('');
}
for(const fixture of responses)for(const mode of ['json','sse'] as const)test(`offline proxy replay preserves ${fixture.name} (${mode})`,async()=>{
 const scratch=await mkdtemp(path.join(tmpdir(),'axiom-validation-replay-')),receipts:TurnDiagnostic[]=[];
 const text=JSON.stringify({disposition:fixture.disposition,report:fixture.report}),value={id:'fixture-response',status:'completed',output:[output(text)]};
 const upstreamBytes=Buffer.from(mode==='sse'?stream(text):JSON.stringify(value));
 const request=JSON.stringify({model:'fixture-only',input:'Fixtures / Mock: use saved evidence, no real model',stream:mode==='sse',text:{format:{type:'json_schema',name:'finding_validation',strict:true,schema}}});
 let calls=0;
 const upstream=createServer(async(req,res)=>{calls++;let received='';for await(const chunk of req)received+=chunk;assert.equal(received,request,'Native Responses request bytes must remain unchanged');assert.equal(req.url,'/v1/responses');res.writeHead(200,{'content-type':mode==='sse'?'text/event-stream':'application/json'});for(let i=0;i<upstreamBytes.length;i+=137)res.write(upstreamBytes.subarray(i,i+137));res.end();});
 await new Promise<void>(r=>upstream.listen(0,'127.0.0.1',r));const proxy=await startReliableUpstream(`http://127.0.0.1:${(upstream.address() as any).port}/v1`,path.join(scratch,'gate.sqlite'),()=>{},v=>receipts.push(v));
 try{
  const response=await fetch(proxy.baseUrl+'/responses',{method:'POST',headers:{authorization:'Bearer fixture-private-not-real'},body:request});const raw=await response.text();let terminal:any;
  if(mode==='json')terminal=JSON.parse(raw);else{const parser=new SseParser((_event,payload)=>{const v=JSON.parse(payload);if(v.type==='response.completed')terminal=v.response;});parser.push(raw);parser.end();}
  assert.equal(terminal.output[0].content[0].text,text);assert.deepEqual(JSON.parse(terminal.output[0].content[0].text),{disposition:fixture.disposition,report:fixture.report});assert.equal(calls,1,'No diagnostic or quality check may replay accepted work');
  assert.equal(receipts.length,1);const receipt=receipts[0]!;assert.equal(receipt.request.sha256,digest(request));assert.equal(receipt.request.format,'json_schema');assert.equal(receipt.request.validationContract?.reportRequirement,'nonempty_string_only');assert.equal(receipt.request.validationContract?.reportDescriptionPresent,false);assert.equal(receipt.request.controls.outputTokenLimit,null);assert.equal(receipt.response.rawSha256,digest(upstreamBytes));assert.equal(receipt.response.terminalText.sha256,digest(text));assert.equal(receipt.response.terminalText.bytes,Buffer.byteLength(text));assert.equal(receipt.response.invalidFrames,0);
  if(mode==='sse'){assert.equal(receipt.response.deltaText.sha256,digest(text));assert.equal(receipt.response.itemText.sha256,digest(text));assert.equal(receipt.response.doneText.sha256,digest(text));}
  assert.doesNotMatch(JSON.stringify(receipt),/fixture-private|Fixtures|Static Finding|用户输入|Bearer|http:\/\//);
 }finally{await proxy.close();upstream.closeAllConnections();await new Promise<void>(r=>upstream.close(()=>r()));await rm(scratch,{recursive:true,force:true});}
});
test('diagnostic hashes survive split UTF-8 and escaped surrogate boundaries without retaining secrets',()=>{
 const text='Fixtures 🧪 中文',payload=stream(text),receipts:TurnDiagnostic[]=[];const recorder=new UpstreamTurnDiagnostic(Buffer.from('{"text":{"format":{"type":"json_schema"}}}'),'responses',v=>receipts.push(v));recorder.headers(200,'text/event-stream');for(const byte of Buffer.from(payload))recorder.chunk(Uint8Array.of(byte));recorder.finish(false);recorder.finish(false);assert.equal(receipts.length,1);assert.equal(receipts[0]!.response.deltaText.sha256,digest(text));
 const split=new UpstreamTurnDiagnostic(Buffer.from('{}'),'responses',v=>receipts.push(v));split.headers(200,'text/event-stream');split.chunk(Buffer.from('data: '+JSON.stringify({type:'response.output_text.delta',delta:'\ud83e'})+'\n\n'));split.chunk(Buffer.from('data: '+JSON.stringify({type:'response.output_text.delta',delta:'\uddea'})+'\n\n'));split.finish(false);assert.equal(receipts[1]!.response.deltaText.sha256,digest('🧪'));
});
test('bounded private receipts make omitted and failed writes explicit',async()=>{
 const scratch=await mkdtemp(path.join(tmpdir(),'axiom-validation-journal-'));
 try{
  const journal=new ValidationTransportJournal(scratch,2048);for(let i=0;i<5;i++){const recorder=new UpstreamTurnDiagnostic(Buffer.from('{}'),'responses',journal.record);recorder.finish(false);}const summary=journal.close();assert.equal(summary.state,'incomplete');assert.ok(summary.omitted>0);const raw=await readFile(path.join(scratch,'logs/validation-transport.jsonl'),'utf8');const rows=raw.trim().split('\n').map(v=>JSON.parse(v));assert.equal(rows.at(-1).summary.received,5);assert.ok(Buffer.byteLength(raw)<=2048);
  const blocked=new ValidationTransportJournal(scratch);assert.equal(blocked.close().state,'incomplete');
 }finally{await rm(scratch,{recursive:true,force:true});}
});

test('diagnostic callback faults do not alter output or replay a request',async()=>{
 const scratch=await mkdtemp(path.join(tmpdir(),'axiom-diagnostic-observer-fault-'));let calls=0;
 const text=JSON.stringify({disposition:'not_applicable',report:'## Assessment\nThe target does not contain the claimed file.'});
 const up=createServer((_req,res)=>{calls++;res.writeHead(200,{'content-type':'text/event-stream'});res.end(stream(text));});await new Promise<void>(resolve=>up.listen(0,'127.0.0.1',resolve));
 const proxy=await startReliableUpstream(`http://127.0.0.1:${(up.address() as any).port}/v1`,path.join(scratch,'gate.sqlite'),()=>{},()=>{throw new Error('Fixture write fault');});
 try{const response=await fetch(proxy.baseUrl+'/responses',{method:'POST',body:'{"model":"fixture","input":"Fixtures / Mock"}'});const raw=await response.text();assert.equal(calls,1);assert.match(raw,/response.completed/);assert.doesNotMatch(raw,/response.failed/);}
 finally{await proxy.close();up.closeAllConnections();await new Promise<void>(resolve=>up.close(()=>resolve()));await rm(scratch,{recursive:true,force:true});}
});
