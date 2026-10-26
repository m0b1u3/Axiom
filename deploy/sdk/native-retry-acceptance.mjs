// Fixtures / Mock only. Isolated native Codex retry-policy comparison.
import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {mkdtemp,mkdir,writeFile,rm} from 'node:fs/promises';
import path from 'node:path';
import {pathToFileURL,fileURLToPath} from 'node:url';
assert.equal(process.env.AXIOM_ISOLATED_RETRY_FIXTURE,'1');
const scanner=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'../../code-audit-scanner');
const {createProviderProfile,createProfileCodex}=await import(pathToFileURL(scanner+'/node_modules/@openai/codex-security/dist/provider-profile.js'));
const {startReliableUpstream}=await import(pathToFileURL(scanner+'/dist/proxy/reliable-upstream.js'));
const {buildCodexOverrides,buildModelCatalog}=await import(pathToFileURL(scanner+'/dist/provider/model-provider.js'));
const scratch=await mkdtemp('/tmp/axiom-native-retry-');
const safePath=process.env.PATH;for(const k of Object.keys(process.env))delete process.env[k];
Object.assign(process.env,{PATH:safePath,HOME:scratch+'/home',CODEX_HOME:scratch+'/home/.codex',CUSTOM_API_KEY:'fixture-retry-no-real-key'});
await mkdir(process.env.CODEX_HOME,{recursive:true,mode:0o700});await mkdir(scratch+'/source');
const results=[];
function completed(res){
 res.writeHead(200,{'content-type':'text/event-stream'});
 const item={id:'fixture-msg',type:'message',role:'assistant',status:'completed',content:[{type:'output_text',text:'fixture completed',annotations:[]}]};
 for(const value of [{type:'response.created',response:{id:'fixture-response',status:'in_progress',output:[]}},{type:'response.output_item.added',output_index:0,item:{...item,status:'in_progress',content:[]}},{type:'response.output_text.delta',item_id:item.id,output_index:0,content_index:0,delta:'fixture completed'},{type:'response.output_item.done',output_index:0,item},{type:'response.completed',response:{id:'fixture-response',status:'completed',output:[item],usage:{input_tokens:1,output_tokens:1,total_tokens:2}}}])res.write('event: '+value.type+'\ndata: '+JSON.stringify(value)+'\n\n');
 res.end();
}
try{
 for(const fault of ['http503','http429','responseFailed','sseError','droppedStream'])for(const policy of ['zero','native']){
  let calls=0;const controller=new AbortController();const timer=setTimeout(()=>controller.abort(),25000);
  const up=createServer(async(req,res)=>{
   for await(const _ of req){}
   if(!req.url.endsWith('/responses')){res.writeHead(404);res.end();return;}
   assert.equal(req.headers.authorization,'Bearer fixture-retry-no-real-key');calls++;
   if(calls===1){
    if(fault==='http429'){res.writeHead(429,{'content-type':'application/json','retry-after':'1'});res.end('{"error":{"code":"rate_limit_exceeded","message":"Fixture temporary throttle"}}');return;}
    if(fault==='http503'){res.writeHead(503,{'content-type':'application/json'});res.end('{"error":{"code":"server_error","message":"Fixture temporary upstream failure"}}');return;}
    res.writeHead(200,{'content-type':'text/event-stream'});
    if(fault==='droppedStream'){res.write('data: {"type":"response.created","response":{"id":"fixture-partial","status":"in_progress"}}\n\n');setTimeout(()=>res.destroy(),10);return;}
    if(fault==='responseFailed')res.end('event: response.failed\ndata: {"type":"response.failed","response":{"id":"fixture","status":"failed","error":{"code":"server_error","message":"Upstream request failed"}}}\n\n');
    else res.end('event: error\ndata: {"type":"error","error":{"code":"server_error","message":"Upstream request failed"}}\n\n');
    return;
   }
   completed(res);
  });
  await new Promise(r=>up.listen(0,'127.0.0.1',r));
  const proxy=await startReliableUpstream('http://127.0.0.1:'+up.address().port,path.join(scratch,fault+'-'+policy+'.sqlite'));
  const task={provider:'custom',connectionType:'openai_compatible',credentialEnv:'CUSTOM_API_KEY',wireApi:'responses',model:'fixture-retry-model',baseUrl:proxy.baseUrl,sdkParameters:{parameters:{reasoningEffort:'high'}}};
  const config=buildCodexOverrides(task),catalog=scratch+'/'+fault+'-'+policy+'.json';await writeFile(catalog,JSON.stringify(buildModelCatalog(task)));config.model_catalog_json=catalog;
  if(policy==='zero')Object.assign(config.model_providers.custom,{request_max_retries:0,stream_max_retries:0});
  const profile=await createProviderProfile(process.env.CODEX_HOME,config);const {model_providers,...nativeConfig}=config;
  try{
   const codex=await createProfileCodex({config:nativeConfig,env:{...process.env}},profile.name);
   const thread=codex.startThread({workingDirectory:scratch+'/source',skipGitRepoCheck:true,sandboxMode:'read-only',approvalPolicy:'never',model:task.model,modelReasoningEffort:'high',webSearchMode:'disabled'});
   let failure=null,result=null;try{result=await thread.run('Fixtures / Mock only: return the fixture response, use no tools.',{signal:controller.signal});}catch(e){failure=String(e.message).slice(0,1000);}
   const row={fault,policy,calls,completed:result?.finalResponse==='fixture completed',failure};results.push(row);console.log(JSON.stringify(row));assert.equal(calls,policy==='zero'?1:2,JSON.stringify(row));assert.equal(row.completed,policy==='native',JSON.stringify(row));
  }finally{clearTimeout(timer);await profile.cleanup();await proxy.close();up.closeAllConnections();await new Promise(r=>up.close(r));}
 }
 console.log(JSON.stringify({status:'passed',fixtureOnly:true,SDK:'0.5.0',nativeRetryPolicy:true,proxyReplay:false,results,realModelCalls:0,realSDKLogins:0}));
}finally{await rm(scratch,{recursive:true,force:true});}
