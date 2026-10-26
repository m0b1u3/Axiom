import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {mkdtempSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {startReliableUpstream} from '../src/proxy/reliable-upstream.js';
import {RequestGate,retryDelay,quotaExhausted} from '../src/proxy/request-gate.js';
import {upstreamTransportDiagnostic,upstreamResponseDiagnostic} from '../src/proxy/transport-diagnostic.js';

test('Retry-After supports seconds and dates; quota is not temporary throttling',()=>{
 assert.equal(retryDelay('7',0),7000);assert.equal(retryDelay('Thu, 01 Jan 1970 00:00:10 GMT',0,0),10000);
 assert.equal(quotaExhausted('Free quota exhausted'),true);assert.equal(quotaExhausted('too many requests'),false);
});
test('provider cooldown is shared and cancellable without blocking other model keys',async()=>{
 const dir=mkdtempSync(path.join(tmpdir(),'axiom-gate-'));const a=new RequestGate(path.join(dir,'g.sqlite')),b=new RequestGate(path.join(dir,'g.sqlite'));
 try{
  a.cooldown('limited',60000);
  const controller=new AbortController();const pending=b.acquire('limited',controller.signal);
  const free=await b.acquire('other',new AbortController().signal);free();
  controller.abort();await assert.rejects(pending);
 }finally{a.close();b.close();rmSync(dir,{recursive:true,force:true});}
});
test('parallel SDK requests across proxy instances have no platform concurrency ceiling',async()=>{
 const dir=mkdtempSync(path.join(tmpdir(),'axiom-unbounded-gateway-'));const total=9,pending=new Set<import('node:http').ServerResponse>();let reached=0;
 let ready!:()=>void;const allReached=new Promise<void>(resolve=>ready=resolve);
 const up=createServer((_req,res)=>{pending.add(res);reached++;if(reached===total)ready();});
 await new Promise<void>(resolve=>up.listen(0,'127.0.0.1',resolve));
 const base=`http://127.0.0.1:${(up.address() as import('node:net').AddressInfo).port}`;
 const a=await startReliableUpstream(base,path.join(dir,'shared.sqlite')),b=await startReliableUpstream(base,path.join(dir,'shared.sqlite'));
 const controller=new AbortController();let timeout:NodeJS.Timeout|undefined;
 const calls=Array.from({length:total},(_,i)=>fetch((i%2?a:b).baseUrl+'/responses',{method:'POST',headers:{authorization:'Bearer fixture-same-model-key'},body:'{"model":"fixture-native-concurrency","input":"Fixtures / Mock"}',signal:controller.signal}).then(async response=>{assert.equal(response.status,200);assert.match(await response.text(),/response.completed/);}));
 const joined=Promise.allSettled(calls);
 try{
  await Promise.race([allReached,new Promise<never>((_,reject)=>{timeout=setTimeout(()=>reject(new Error(`Platform capped simultaneous requests at ${reached}/${total}`)),3000);})]);
  assert.equal(reached,total);
  for(const res of pending){res.writeHead(200,{'content-type':'text/event-stream'});res.end('data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Fixtures / Mock"}]}]}}\n\n');}
  for(const result of await joined)if(result.status==='rejected')throw result.reason;
 }finally{
  if(timeout)clearTimeout(timeout);controller.abort();await joined;for(const res of pending)res.destroy();up.closeAllConnections();await Promise.all([a.close(),b.close()]);await new Promise<void>(resolve=>up.close(()=>resolve()));rmSync(dir,{recursive:true,force:true});
 }
});
for(const mode of ['empty','success','truncated','quota','broken'])test(`native Responses gateway ${mode}`,async()=>{
 const dir=mkdtempSync(path.join(tmpdir(),'axiom-gateway-'));let calls=0;
 const up=createServer((_req,res)=>{
  calls++;
  if(mode==='quota'){res.writeHead(429,{'content-type':'application/json','retry-after':'1'});res.end(JSON.stringify({error:{message:mode==='quota'?'Free quota exhausted':'too many requests'}}));return;}
  res.writeHead(200,{'content-type':'text/event-stream'});
  if(mode==='broken'){res.end('data: {broken}\n\n');return;}
  const output=mode==='success'?[{type:'message',content:[{type:'output_text',text:'done'}]}]:[];
  res.end(`data: ${JSON.stringify({type:'response.completed',response:{status:mode==='truncated'?'incomplete':'completed',output}})}\n\n`);
 });
 await new Promise<void>(r=>up.listen(0,'127.0.0.1',r));const port=(up.address() as any).port;
 const proxy=await startReliableUpstream(`http://127.0.0.1:${port}/v1`,path.join(dir,'gate.sqlite'));
 try{
  const res=await fetch(proxy.baseUrl+'/responses',{method:'POST',body:JSON.stringify({model:'fixture',input:'no real model'})});const text=await res.text();
  if(mode==='quota'){assert.equal(res.status,402);assert.equal(calls,1);assert.match(text,/quota_exhausted/);}
  else if(mode==='success'){assert.match(text,/response.completed/);assert.doesNotMatch(text,/response.failed/);}
  else{assert.match(text,/response.failed/);assert.doesNotMatch(text,/event: response.completed/);}
 }finally{await proxy.close();up.closeAllConnections();await new Promise<void>(r=>up.close(()=>r()));rmSync(dir,{recursive:true,force:true});}
});

test('429 is forwarded once and native SDK owns retry policy',async()=>{
 const dir=mkdtempSync(path.join(tmpdir(),'axiom-429-'));let calls=0;
 const up=createServer((_req,res)=>{calls++;res.writeHead(429,{'retry-after':'1'});res.end('limited');});
 await new Promise<void>(r=>up.listen(0,'127.0.0.1',r));
 const proxy=await startReliableUpstream(`http://127.0.0.1:${(up.address() as any).port}`,path.join(dir,'gate.sqlite'));
 try{const response=await fetch(proxy.baseUrl+'/responses',{method:'POST',body:'{"model":"fixture","input":"Fixtures / Mock"}'});assert.equal(response.status,429);await response.text();assert.equal(calls,1);assert.equal(response.headers.get('retry-after'),'1');}
 finally{await proxy.close();up.closeAllConnections();await new Promise<void>(r=>up.close(()=>r()));rmSync(dir,{recursive:true,force:true});}
});

for(const phase of ['headers','stream'] as const)test(`interrupted ${phase} preserve a bounded diagnostic without replaying accepted work`,async()=>{
 const dir=mkdtempSync(path.join(tmpdir(),'axiom-transport-fault-'));const notices:string[]=[];let calls=0;
 const up=createServer((_req,res)=>{calls++;if(phase==='headers'){res.destroy();return;}res.writeHead(200,{'content-type':'text/event-stream'});res.flushHeaders();res.write('data: {"type":"response.created","response":{"id":"fixture"}}\n\n');setTimeout(()=>res.destroy(),25);});
 await new Promise<void>(resolve=>up.listen(0,'127.0.0.1',resolve));
 const proxy=await startReliableUpstream(`http://127.0.0.1:${(up.address() as import('node:net').AddressInfo).port}`,path.join(dir,'gate.sqlite'),notice=>notices.push(notice));
 try{
  const response=await fetch(proxy.baseUrl+'/responses',{method:'POST',headers:{authorization:'Bearer fixture-private-credential-never-log'},body:'{"model":"fixture-transport-error","input":"Fixtures / Mock"}'});const text=await response.text();
  assert.equal(calls,1,'A consumed turn must not be silently replayed');assert.equal(notices.length,1);assert.match(notices[0]!,new RegExp('"phase":"'+phase+'"'));assert.match(notices[0]!,/UND_ERR_SOCKET|ECONNRESET/);assert.doesNotMatch(notices[0]!,/fixture-private|Bearer|http:\/\//);
  if(phase==='headers'){assert.equal(response.status,502);assert.equal(JSON.parse(text).error.phase,'headers');}
  else{assert.equal(response.status,200);assert.match(text,/response.failed/);assert.doesNotMatch(text,/response.completed/);}
 }finally{await proxy.close();up.closeAllConnections();await new Promise<void>(resolve=>up.close(()=>resolve()));rmSync(dir,{recursive:true,force:true});}
});
test('caller cancellation closes a pending upstream connection without a transport-failure notice',async()=>{
 const dir=mkdtempSync(path.join(tmpdir(),'axiom-transport-cancel-'));const notices:string[]=[];let arrived!:()=>void,closed!:()=>void;
 const requestArrived=new Promise<void>(resolve=>arrived=resolve),requestClosed=new Promise<void>(resolve=>closed=resolve);
 const up=createServer((_req,res)=>{res.on('close',closed);arrived();});await new Promise<void>(resolve=>up.listen(0,'127.0.0.1',resolve));
 const proxy=await startReliableUpstream(`http://127.0.0.1:${(up.address() as import('node:net').AddressInfo).port}`,path.join(dir,'gate.sqlite'),notice=>notices.push(notice));
 const controller=new AbortController();const call=fetch(proxy.baseUrl+'/responses',{method:'POST',body:'{"model":"fixture-cancel","input":"Fixtures / Mock"}',signal:controller.signal});
 try{await Promise.race([requestArrived,call.then(()=>{throw new Error('Upstream request was not dispatched')})]);controller.abort();await assert.rejects(call);await requestClosed;assert.deepEqual(notices,[]);}
 finally{controller.abort();await proxy.close();up.closeAllConnections();await new Promise<void>(resolve=>up.close(()=>resolve()));rmSync(dir,{recursive:true,force:true});}
});
test('transport diagnostics reject secret-bearing messages and arbitrary error categories',()=>{
 const value=upstreamTransportDiagnostic({message:'Bearer secret-value https://credential.example?key=secret',code:'secret-value',cause:{code:'secret-value'}},'headers',Date.now());
 assert.equal(value.cause,'UNCLASSIFIED_TRANSPORT_ERROR');assert.doesNotMatch(JSON.stringify(value),/secret-value|credential\.example/);
 assert.equal(upstreamTransportDiagnostic({cause:{code:'UND_ERR_HEADERS_TIMEOUT'}},'headers',Date.now()).cause,'UND_ERR_HEADERS_TIMEOUT');
});

for(const kind of ['response.failed','response.incomplete','error'])test(`provider ${kind} is retained with secret-safe diagnostics and no proxy replay`,async()=>{
 const dir=mkdtempSync(path.join(tmpdir(),'axiom-upstream-sse-error-'));let calls=0;const notices:string[]=[];
 const error={code:'server_error',message:'Upstream request failed; Bearer fixture-private-key https://private.example?key=fixture-private-key'};
 const value=kind==='error'?{type:kind,error}:{type:kind,response:{id:'fixture-error',status:'failed',error}};
 const up=createServer((_req,res)=>{calls++;res.writeHead(200,{'content-type':'text/event-stream'});res.end('event: '+kind+'\ndata: '+JSON.stringify(value)+'\n\n');});
 await new Promise<void>(r=>up.listen(0,'127.0.0.1',r));
 const proxy=await startReliableUpstream(`http://127.0.0.1:${(up.address() as any).port}`,path.join(dir,'gate.sqlite'),v=>notices.push(v));
 try{const response=await fetch(proxy.baseUrl+'/responses',{method:'POST',body:'{"model":"fixture","input":"Fixtures / Mock"}'});const text=await response.text();assert.equal(calls,1);assert.match(text,/Upstream request failed/);assert.equal(notices.length,1);assert.match(notices[0]!,/server_error/);assert.doesNotMatch(notices[0]!,/fixture-private|Bearer|private.example/);}
 finally{await proxy.close();up.closeAllConnections();await new Promise<void>(r=>up.close(()=>r()));rmSync(dir,{recursive:true,force:true});}
});

test('provider diagnostic rejects arbitrary credential-bearing error codes and events',()=>{
 const secret='Bearer fixture-private-key https://private.example';
 const value=upstreamResponseDiagnostic({type:secret,response:{error:{code:secret,message:secret}}},secret,Date.now());
 assert.equal(value.code,'unclassified_upstream_error');assert.equal(value.event,'error');assert.doesNotMatch(JSON.stringify(value),/fixture-private|Bearer|private.example/);
});

test('retired Chat endpoints never reach upstream and Responses exposes every namespace function',async()=>{
 const dir=mkdtempSync(path.join(tmpdir(),'axiom-responses-only-'));let calls=0,received='';
 const upstream=createServer(async(req,res)=>{calls++;for await(const part of req)received+=part;res.writeHead(502,{'content-type':'application/json'});res.end('{"error":"Fixtures / Mock only"}');});
 await new Promise<void>(resolve=>upstream.listen(0,'127.0.0.1',resolve));
 const proxy=await startReliableUpstream(`http://127.0.0.1:${(upstream.address() as any).port}/v1`,path.join(dir,'gate.sqlite'));
 try{
  for(const route of ['/chat/completions','/v1/chat/completions']){const response=await fetch(proxy.baseUrl+route,{method:'POST',body:'{}'});assert.equal(response.status,404);await response.text();}
  assert.equal(calls,0);
  const raw=JSON.stringify({model:'fixture',input:'Fixtures / Mock',tools:[{type:'namespace',name:'multi_agent_v1',tools:[{type:'function',name:'spawn_agent',parameters:{type:'object'}}]}]});
  const response=await fetch(proxy.baseUrl+'/responses',{method:'POST',body:raw});assert.equal(response.status,502);await response.text();assert.equal(calls,1);const sent=JSON.parse(received);assert.equal(sent.tools.length,1);assert.equal(sent.tools[0].type,'function');assert.match(sent.tools[0].name,/^axns_multi_agent_v1_spawn_agent_/);assert.deepEqual(sent.tools[0].parameters,{type:'object'});
 }finally{await proxy.close();upstream.closeAllConnections();await new Promise<void>(resolve=>upstream.close(()=>resolve()));rmSync(dir,{recursive:true,force:true});}
});
