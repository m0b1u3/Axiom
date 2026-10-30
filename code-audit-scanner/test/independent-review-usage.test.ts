import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {mkdtemp,mkdir,writeFile,readFile,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {CodexSecurity} from '@openai/codex-security';
import {decode} from '@axiom/contracts';
import type {ExecutionTokenUsage} from '@axiom/contracts';
import type {ScanTask} from '../src/contract/scan-task.js';
import {runSDKOperation} from '../src/engine/sdk-operation.js';

const history=JSON.parse(await readFile(new URL('../../contracts/fixtures/deepseek-review-usage-20261027.json',import.meta.url),'utf8'));
const {format}=JSON.parse(await readFile(new URL('../../contracts/fixtures/sdk050-standalone-validation-format.json',import.meta.url),'utf8'));
const report='## Static Finding Assessment\n\nFixtures / Mock only. This is an offline transport test.\n\n### Evidence\n\nNo vulnerability has been verified or reproduced.';

test('independent review archives real-format Responses usage separately from cost, including failure paths',async t=>{
 const previous=process.env.AXIOM_VALIDATION_OUTPUT_MODE;process.env.AXIOM_VALIDATION_OUTPUT_MODE='native_schema';
 t.after(()=>{if(previous===undefined)delete process.env.AXIOM_VALIDATION_OUTPUT_MODE;else process.env.AXIOM_VALIDATION_OUTPUT_MODE=previous;});
 for(const scenario of ['sse','json','explicit-zero-write','partially-missing-write','missing-usage','http-failure','incomplete-report','invalid-counts'])await t.test(scenario,async t=>{
  const root=await mkdtemp(path.join(tmpdir(),'axiom-review-usage-'));t.after(()=>rm(root,{recursive:true,force:true}));
  const source=path.join(root,'source'),output=path.join(root,'output');await mkdir(source);await writeFile(path.join(source,'fixture.py'),'# Fixtures / Mock only\n');
  let calls=0;const received:Array<Record<string,unknown>>=[];
  const upstream=createServer(async(req,res)=>{
   const chunks:Buffer[]=[];for await(const chunk of req)chunks.push(chunk);received.push(JSON.parse(Buffer.concat(chunks).toString('utf8')));calls++;
   if(scenario==='http-failure'&&calls===2){res.writeHead(503,{'content-type':'application/json'});res.end('{"error":{"code":"fixture_unavailable","message":"Fixtures / Mock only"}}');return;}
   const usage=structuredClone(history.requests[calls-1].usage);
   if(scenario==='explicit-zero-write'||(scenario==='partially-missing-write'&&calls===1))usage.cache_write_input_tokens=0;
   if(scenario==='invalid-counts')usage.input_tokens_details.cached_tokens=usage.input_tokens+1;
   const response={id:'fixture-review-'+calls,status:'completed',...(scenario==='missing-usage'&&calls===2?{}:{usage}),output:[{type:'message',content:[{type:'output_text',text:JSON.stringify({disposition:'deferred',report})}]}]};
   if(scenario==='json'){res.writeHead(200,{'content-type':'application/json'});res.end(JSON.stringify(response));}
   else{res.writeHead(200,{'content-type':'text/event-stream'});res.end('event: response.completed\ndata: '+JSON.stringify({type:'response.completed',response})+'\n\n');}
  });
  await new Promise<void>(resolve=>upstream.listen(0,'127.0.0.1',resolve));
  t.after(()=>new Promise<void>(resolve=>upstream.close(()=>resolve())));
  // Replace only the SDK inference boundary. The real adapter, receipt parser,
  // strict report checks, progress sink and atomic result publisher still run.
  t.mock.method(CodexSecurity.prototype,'validate',async function(this:CodexSecurity,options:Parameters<CodexSecurity['validate']>[0]){
   const config=(this as unknown as {config:{codexOverrides:Record<string,any>}}).config.codexOverrides;
   const base=config.model_providers[config.model_provider].base_url;
   assert.equal(new URL(base).hostname,'127.0.0.1');
   for(let i=0;i<2;i++){
    const reply=await fetch(base+'/responses',{method:'POST',signal:options.signal,body:JSON.stringify({model:'deepseek-flash',input:[{role:'user',content:'Fixtures / Mock only'}],stream:scenario!=='json',text:{format}})});
    await reply.text();if(!reply.ok)throw new Error('Fixtures / Mock HTTP '+reply.status);
   }
   return {disposition:'deferred' as const,report:scenario==='incomplete-report'?'## Static Finding Assessment — Fixtures / Mock':report,threadId:'fixture-review',outputDir:options.outputDir!};
  });
  const baseUrl='http://127.0.0.1:'+(upstream.address() as import('node:net').AddressInfo).port;
  const task:ScanTask={taskId:'43',executionAttempt:1,resultSchemaVersion:1,progressSchemaVersion:2,eventSchemaVersion:2,sourcePath:source,outputPath:output,provider:'deepseek',connectionType:'openai_compatible',baseUrl,model:'deepseek-flash',scanMode:'standard',timeoutMinutes:1,resolvedModel:{providerId:'deepseek',connectionType:'openai_compatible',baseUrl,model:'deepseek-flash',wireApi:'responses',credentialEnv:'DEEPSEEK_API_KEY',configurationRevision:'fixture'},sdkOperation:{schemaVersion:1,execution:{taskId:'43',attempt:1},kind:'validate',origin:{taskId:'42',attempt:1},findingId:'1',finding:{title:'Fixtures / Mock only'},acknowledgeUnknownCost:true}};
  const result=await runSDKOperation(task,new AbortController().signal);
  assert.equal(calls,2,'each fixture request is sent once; failures are not replayed');
  assert.equal(result.status,scenario==='http-failure'?'failed':scenario==='incomplete-report'?'partial':'completed',result.error?.message);
  for(const req of received){assert.equal(req.model,'deepseek-flash');assert.deepEqual((req.text as {format:unknown}).format,format);assert.equal(Object.hasOwn(req,'response_format'),false);}
  const archived=decode('ScanResult',await readFile(path.join(output,'normalized/result.json')),task.sdkOperation!.execution);
  const progress=decode('ProgressSnapshotV2',await readFile(path.join(output,'state/progress.json')),task.sdkOperation!.execution);
  const usage=decode('ExecutionTokenUsage',JSON.stringify(archived.extensions!.executionUsage),task.sdkOperation!.execution);
  assert.deepEqual(progress.extensions!.executionUsage,usage);
  assert.deepEqual(result.executionUsage,usage);
  assert.equal(usage.source,'responses_usage');assert.equal(usage.requestCount,2);
  assert.equal(usage.complete,!['missing-usage','http-failure','incomplete-report','invalid-counts'].includes(scenario));
  const expected:Partial<ExecutionTokenUsage>=scenario==='invalid-counts'?{inputTokens:null,cachedInputTokens:null,outputTokens:null,totalTokens:null}:['missing-usage','http-failure'].includes(scenario)?{inputTokens:10742,cachedInputTokens:4352,outputTokens:478,reasoningOutputTokens:40,totalTokens:11220}:{inputTokens:24126,cachedInputTokens:15488,outputTokens:3436,reasoningOutputTokens:1729,totalTokens:27562};
  for(const [key,value] of Object.entries(expected))assert.equal(usage[key as keyof ExecutionTokenUsage],value,key);
  assert.equal(usage.cacheWriteInputTokens,scenario==='explicit-zero-write'?0:null);
  assert.equal(archived.cost,null,'usage never fabricates an SDK cost or a zero');
  assert.equal(result.sdkOperation!.cost.estimatedUsd,null);
  const mode=JSON.parse(await readFile(path.join(output,'logs/validation-output-mode.json'),'utf8'));assert.equal(mode.mode,'native_schema');assert.equal(mode.automaticFallback,false);
 });
});
