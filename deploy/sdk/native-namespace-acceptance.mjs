// Fixtures / Mock only: native namespace routing against loopback; no model inference.
import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {mkdtemp,mkdir,writeFile,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
assert.equal(process.env.AXIOM_ISOLATED_NAMESPACE_FIXTURE,'1','Use the network-none runner');
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'../..'),scanner=path.join(root,'code-audit-scanner');
const {CodexSecurity,VERSION}=await import(pathToFileURL(path.join(scanner,'node_modules/@openai/codex-security/dist/index.js')));
const {buildCodexOverrides}=await import(pathToFileURL(path.join(scanner,'dist/provider/model-provider.js')));
const {runSDKOperation}=await import(pathToFileURL(path.join(scanner,'dist/engine/sdk-operation.js')));
const {ensureModelCatalog}=await import(pathToFileURL(path.join(scanner,'dist/engine/codex-security-engine.js')));
const {enterSDKWorkingDirectory}=await import(pathToFileURL(path.join(scanner,'dist/runtime/sdk-working-directory.js')));
const {startReliableUpstream}=await import(pathToFileURL(path.join(scanner,'dist/proxy/reliable-upstream.js')));
assert.equal(VERSION,'0.5.0');
const scratch=await mkdtemp(path.join(tmpdir(),'axiom-namespace-native-')),executablePath=process.env.PATH;
for(const key of Object.keys(process.env))delete process.env[key];
Object.assign(process.env,{PATH:executablePath,HOME:path.join(scratch,'home'),CUSTOM_API_KEY:'fixture-selected-not-real',AXIOM_VALIDATION_OUTPUT_MODE:'native_schema'});
await mkdir(process.env.HOME,{recursive:true,mode:0o700});const source=path.join(scratch,'source');await mkdir(path.join(source,'src'),{recursive:true});await writeFile(path.join(source,'src/query.ts'),'// Fixtures / Mock only\nexport const query = (name) => "SELECT " + name;\n');
const proofs=[];
try{for(const version of ['v1','v2']){
 const id=version==='v1'?'910':'911',execution={taskId:id,attempt:1},output=path.join(scratch,'scans',id,'attempt-1');await mkdir(output,{recursive:true,mode:0o700});
 process.env.CODEX_SECURITY_STATE_DIR=path.join(output,'runtime/sdk');process.env.CODEX_HOME=path.join(output,'runtime/sdk/codex-home');
 const report='## Fixtures / Mock namespace review\n\n`src/query.ts:2` is the synthetic source location. Namespace routing was exercised with a local tool result. No real model, application execution or vulnerability reproduction.';
 let calls=0,alias='',toolOutput='';
 const upstream=createServer(async(req,res)=>{
  let bytes='';for await(const part of req)bytes+=part;const body=JSON.parse(bytes);calls++;
  assert.equal(req.headers.authorization,'Bearer fixture-selected-not-real');assert.equal(body.text.format.type,'json_schema');assert.equal(body.text.format.strict,true);assert.ok(body.tools.every(t=>t.type!=='namespace'));
  assert.equal(body.client_metadata,undefined);assert.equal(body.reasoning?.summary,undefined);
  assert.ok(body.input.filter(v=>v.type==='message'&&['user','developer','system'].includes(v.role)).every(v=>v.id===undefined));
  const wanted=version==='v1'?'axns_multi_agent_v1_close_agent_':'axns_collaboration_list_agents_';
  const selected=body.tools.find(t=>t.name?.startsWith(wanted));assert.ok(selected,'Namespace child must remain exposed');alias ||= selected.name;assert.equal(selected.name,alias);
  if(calls===2){
   const call=body.input.find(v=>v.type==='function_call'&&v.call_id==='fixture-call');assert.ok(call);assert.equal(call.name,alias);assert.equal(call.namespace,undefined);
   const returned=body.input.find(v=>v.type==='function_call_output'&&v.call_id==='fixture-call');assert.ok(returned);toolOutput=typeof returned.output==='string'?returned.output:JSON.stringify(returned.output);
   if(version==='v1')assert.match(toolOutput,/invalid agent id/i);else{assert.match(toolOutput,/agents/i);assert.doesNotMatch(toolOutput,/unsupported|unknown function|invalid tool/i);}
  }
  assert.ok(calls<=2,'No retry or unexpected third turn');
  res.writeHead(200,{'content-type':'text/event-stream','connection':'close'});
  const send=v=>res.write('data: '+JSON.stringify(v)+'\n\n');
  send({type:'response.created',response:{id:'fixture-response-'+calls,status:'in_progress',output:[]}});
  if(calls===1){
   const args=version==='v1'?JSON.stringify({target:'fixture-missing-agent'}):'{}';
   const item={id:'fixture-item',type:'function_call',call_id:'fixture-call',name:alias,arguments:args,status:'completed'};
   send({type:'response.output_item.added',output_index:0,item:{...item,status:'in_progress',arguments:''}});
   send({type:'response.function_call_arguments.delta',output_index:0,item_id:item.id,delta:args});
   send({type:'response.function_call_arguments.done',output_index:0,item_id:item.id,name:alias,arguments:args});
   send({type:'response.output_item.done',output_index:0,item});send({type:'response.completed',response:{id:'fixture-response-1',status:'completed',output:[item],usage:{input_tokens:1,output_tokens:1,total_tokens:2}}});
  }else{
   const text=JSON.stringify({disposition:'deferred',report}),item={id:'fixture-message',type:'message',role:'assistant',status:'completed',content:[{type:'output_text',text,annotations:[]}]};
   send({type:'response.output_item.added',output_index:0,item:{...item,status:'in_progress',content:[]}});send({type:'response.content_part.added',output_index:0,item_id:item.id,content_index:0,part:{type:'output_text',text:'',annotations:[]}});
   send({type:'response.output_text.delta',output_index:0,item_id:item.id,content_index:0,delta:text});send({type:'response.output_text.done',output_index:0,item_id:item.id,content_index:0,text});send({type:'response.output_item.done',output_index:0,item});send({type:'response.completed',response:{id:'fixture-response-2',status:'completed',output:[item],usage:{input_tokens:1,output_tokens:1,total_tokens:2}}});
  }
  res.end();
 });
 await new Promise(r=>upstream.listen(0,'127.0.0.1',r));
 const task={taskId:id,executionAttempt:1,resultSchemaVersion:1,progressSchemaVersion:2,eventSchemaVersion:2,sourcePath:source,outputPath:output,provider:'custom',connectionType:'openai_compatible',credentialEnv:'CUSTOM_API_KEY',wireApi:'responses',baseUrl:'http://127.0.0.1:'+upstream.address().port+'/v1',model:'glm-5.3-flash',scanMode:'standard',timeoutMinutes:1,sdkOperation:{schemaVersion:1,execution,kind:'validate',origin:{taskId:'17',attempt:1},findingId:'1',finding:{title:'Fixtures / Mock namespace audit',reproduced:false},acknowledgeUnknownCost:true}};
 task.resolvedModel={providerId:'custom',connectionType:task.connectionType,model:task.model,wireApi:task.wireApi,baseUrl:task.baseUrl,credentialEnv:task.credentialEnv,configurationRevision:'fixture-only'};
 const restore=await enterSDKWorkingDirectory(output),controller=new AbortController(),timer=setTimeout(()=>controller.abort(),30000);let security,proxy;
 try{
  let result;
  if(version==='v1'){const value=await runSDKOperation(task,controller.signal);assert.equal(value.status,'completed',JSON.stringify(value.error));result=value.sdkOperation.validation;assert.equal(value.sdkOperation.cost.state,'unknown');}
  else{
   const overrides=buildCodexOverrides(task);overrides.model_catalog_json=await ensureModelCatalog(task);overrides.features={multi_agent_v2:{enabled:true,max_concurrent_threads_per_session:9}};
   proxy=await startReliableUpstream(task.baseUrl,path.join(output,'gate.sqlite'));overrides.model_providers.custom.base_url=proxy.baseUrl;
   security=new CodexSecurity({codexOverrides:overrides});result=await security.validate({repositoryPath:source,finding:task.sdkOperation.finding,outputDir:path.join(output,'raw/codex-security'),signal:controller.signal});
  }
  assert.equal(calls,2);assert.equal(result.report,report);assert.equal(result.disposition,'deferred');
  proofs.push({version,status:'passed',fixtureRequests:calls,namespaceRestoredByNativeSDK:true,historyReencoded:true,callIdPreserved:true,fullReportPreserved:true,handlerOutcome:version==='v1'?'native close_agent invalid-ID error preserved':'native list_agents result returned',spawnedAgents:0});
 }finally{clearTimeout(timer);await security?.close();await proxy?.close();restore();upstream.closeAllConnections();await new Promise(r=>upstream.close(r));}
}
 console.log(JSON.stringify({status:'passed',fixtureOnly:true,sdk:VERSION,wireAPI:'responses',outputMode:'native_schema',realModelCalls:0,realSDKLogins:0,spawnedAgents:0,proofs}));
}finally{await rm(scratch,{recursive:true,force:true});}
