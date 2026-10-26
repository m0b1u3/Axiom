// Fixtures / Mock only: disconnected container, no business mounts or keys.
import assert from 'node:assert/strict';
import {createServer,request} from 'node:http';
import {mkdtemp,rm} from 'node:fs/promises';
import path from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'../..');
const {startReliableUpstream}=await import(pathToFileURL(path.join(root,'code-audit-scanner/dist/proxy/reliable-upstream.js')));
assert.equal(process.env.AXIOM_ISOLATED_LONG_FIXTURE,'1');
const delay=310000,start=Date.now(),dir=await mkdtemp('/tmp/axiom-long-transport-'),notices=[],clients=[],timers=[];
let arrivals=0,ready;const reached=new Promise(resolve=>ready=resolve);
const upstream=createServer(async(req,res)=>{
 let raw='';for await(const chunk of req)raw+=chunk;const body=JSON.parse(raw),bodyDelay=body.model.endsWith('-body');
 arrivals++;console.log(JSON.stringify({event:'fixture-request-arrived',model:body.model,arrivals}));if(arrivals===2)ready();
 const open=()=>res.writeHead(200,{'content-type':'text/event-stream','connection':'close'});
 if(bodyDelay){open();res.write('data: {"type":"response.created","response":{"id":"fixture","status":"in_progress","output":[]}}\n\n');}
 timers.push(setTimeout(()=>{
  if(res.destroyed)return;if(!bodyDelay)open();
  res.end('data: {"type":"response.completed","response":{"id":"fixture","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Fixtures / Mock completed"}]}]}}\n\n');
 },delay));
});
upstream.requestTimeout=0;upstream.headersTimeout=0;upstream.timeout=0;
await new Promise(resolve=>upstream.listen(0,'127.0.0.1',resolve));
const reliable=await startReliableUpstream('http://127.0.0.1:'+upstream.address().port+'/v1',path.join(dir,'gate.sqlite'),v=>notices.push(v));
const pulse=setInterval(()=>console.log(JSON.stringify({event:'fixture-waiting',elapsedMs:Date.now()-start,arrivals})),45000);
function check(model,base){return new Promise((resolve,reject)=>{
 const started=Date.now();const req=request(base+'/responses',{method:'POST',headers:{'content-type':'application/json','authorization':'Bearer fixture-only-not-real'}},res=>{
  let text='';res.on('data',s=>text+=s);res.on('error',reject);res.on('end',()=>{try{assert.equal(res.statusCode,200,text);assert.match(text,/response.completed/);assert.doesNotMatch(text,/response.failed/);assert.ok(Date.now()-started>=delay);resolve({model,status:res.statusCode,elapsedMs:Date.now()-started});}catch(e){reject(e);}});
 });clients.push(req);req.on('error',reject);req.end(JSON.stringify({model,input:'Fixtures / Mock only',stream:true}));
});}
let cap;
const calls=[check('fixture-responses-headers',reliable.baseUrl),check('fixture-responses-body',reliable.baseUrl)];const settled=Promise.allSettled(calls);
try{
 await Promise.race([reached,new Promise((_,reject)=>{cap=setTimeout(()=>reject(new Error('Two fixture requests did not reach upstream concurrently')),5000);})]);clearTimeout(cap);
 const result=await Promise.race([settled,new Promise((_,reject)=>{cap=setTimeout(()=>reject(new Error('Long transport fixture deadline')),350000);})]);
 for(const r of result)if(r.status==='rejected')throw r.reason;
 assert.equal(arrivals,2);assert.deepEqual(notices,[]);
 console.log(JSON.stringify({status:'passed',fixtureOnly:true,SDK:'0.5.0',heldResponseMs:delay,elapsedMs:Date.now()-start,simultaneousRequests:arrivals,results:result.map(r=>r.value),notices,realModelCalls:0,realSDKLogins:0}));
}finally{
 clearTimeout(cap);clearInterval(pulse);for(const timer of timers)clearTimeout(timer);for(const client of clients)client.destroy();await settled;upstream.closeAllConnections();await reliable.close();await new Promise(resolve=>upstream.close(resolve));await rm(dir,{recursive:true,force:true});
}
