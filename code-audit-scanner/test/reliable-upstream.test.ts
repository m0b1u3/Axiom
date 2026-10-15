import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {mkdtempSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {startReliableUpstream} from '../src/proxy/reliable-upstream.js';
import {RequestGate,retryDelay,quotaExhausted} from '../src/proxy/request-gate.js';

test('Retry-After supports seconds and dates; quota is not temporary throttling',()=>{
 assert.equal(retryDelay('7',0),7000);assert.equal(retryDelay('Thu, 01 Jan 1970 00:00:10 GMT',0,0),10000);
 assert.equal(quotaExhausted('Free quota exhausted'),true);assert.equal(quotaExhausted('too many requests'),false);
});
test('separate gate instances share request slots and release on cancellation',async()=>{
 const dir=mkdtempSync(path.join(tmpdir(),'axiom-gate-'));const a=new RequestGate(path.join(dir,'g.sqlite'),1),b=new RequestGate(path.join(dir,'g.sqlite'),1);
 const controller=new AbortController();const release=await a.acquire('same',controller.signal);const pending=b.acquire('same',controller.signal);
 controller.abort();await assert.rejects(pending);release();const next=await b.acquire('same',new AbortController().signal);next();a.close();b.close();rmSync(dir,{recursive:true,force:true});
});
for(const mode of ['empty','success','truncated','quota','retry','broken'])test(`native Responses gateway ${mode}`,async()=>{
 const dir=mkdtempSync(path.join(tmpdir(),'axiom-gateway-'));let calls=0;
 const up=createServer((_req,res)=>{
  calls++;
  if(mode==='quota'||(mode==='retry'&&calls<2)){res.writeHead(429,{'content-type':'application/json','retry-after':'1'});res.end(JSON.stringify({error:{message:mode==='quota'?'Free quota exhausted':'too many requests'}}));return;}
  res.writeHead(200,{'content-type':'text/event-stream'});
  if(mode==='broken'){res.end('data: {broken}\n\n');return;}
  const output=mode==='success'||mode==='retry'?[{type:'message',content:[{type:'output_text',text:'done'}]}]:[];
  res.end(`data: ${JSON.stringify({type:'response.completed',response:{status:mode==='truncated'?'incomplete':'completed',output}})}\n\n`);
 });
 await new Promise<void>(r=>up.listen(0,'127.0.0.1',r));const port=(up.address() as any).port;
 const proxy=await startReliableUpstream(`http://127.0.0.1:${port}/v1`,path.join(dir,'gate.sqlite'));
 try{
  const res=await fetch(proxy.baseUrl+'/responses',{method:'POST',body:JSON.stringify({model:'fixture',input:'no real model'})});const text=await res.text();
  if(mode==='quota'){assert.equal(res.status,402);assert.equal(calls,1);assert.match(text,/quota_exhausted/);}
  else if(mode==='success'||mode==='retry'){assert.match(text,/response.completed/);assert.doesNotMatch(text,/response.failed/);if(mode==='retry')assert.equal(calls,2);}
  else{assert.match(text,/response.failed/);assert.doesNotMatch(text,/event: response.completed/);}
 }finally{await proxy.close();up.closeAllConnections();await new Promise<void>(r=>up.close(()=>r()));rmSync(dir,{recursive:true,force:true});}
});

test('persistent 429 has bounded retries',async()=>{
 const dir=mkdtempSync(path.join(tmpdir(),'axiom-429-'));let calls=0;
 const up=createServer((_req,res)=>{calls++;res.writeHead(429,{'retry-after':'1'});res.end('limited');});
 await new Promise<void>(r=>up.listen(0,'127.0.0.1',r));
 const proxy=await startReliableUpstream(`http://127.0.0.1:${(up.address() as any).port}`,path.join(dir,'gate.sqlite'));
 try{const response=await fetch(proxy.baseUrl+'/responses',{method:'POST',body:'{"model":"fixture"}'});assert.equal(response.status,429);await response.text();assert.equal(calls,3);}
 finally{await proxy.close();up.closeAllConnections();await new Promise<void>(r=>up.close(()=>r()));rmSync(dir,{recursive:true,force:true});}
});
