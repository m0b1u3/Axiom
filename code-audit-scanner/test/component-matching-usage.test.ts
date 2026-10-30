import {test,type TestContext} from 'node:test';
import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {spawn} from 'node:child_process';
import {mkdtemp,mkdir,readFile,writeFile,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {decode,type ExecutionTokenUsage,type TokenCostEstimate} from '@axiom/contracts';
import {frozenTreeHash,componentCoverage} from '../src/runtime/component-plan.js';
import {componentMatchingEstimate} from '../src/runtime/component-usage.js';
import {sumExecutionUsage,undispatchedUsage} from '../src/runtime/execution-token-usage.js';

const root=fileURLToPath(new URL('../../',import.meta.url));
const execution={taskId:'42',attempt:1};
const schema=JSON.parse(await readFile(new URL('./fixtures/sdk050-matching-schema.json',import.meta.url),'utf8'));
async function run(t:TestContext,scenario:string){
 const scratch=await mkdtemp(path.join(tmpdir(),'axiom-matching-usage-'));t.after(()=>rm(scratch,{recursive:true,force:true}));
 const source=path.join(scratch,'source'),output=path.join(scratch,'output');
 for(const name of ['api/a.ts','web/b.ts','worker/c.ts']){await mkdir(path.dirname(path.join(source,name)),{recursive:true});await writeFile(path.join(source,name),'// Fixtures / Mock\n');}
 const requests:Array<Record<string,any>>=[];let matchingCount=0;
 const upstream=createServer(async(req,res)=>{
  const chunks:Buffer[]=[];for await(const chunk of req)chunks.push(chunk);
  const body=JSON.parse(Buffer.concat(chunks).toString());requests.push(body);
  assert.equal(req.url,'/responses');assert.equal(body.model,'deepseek-flash');
  assert.deepEqual(body.text.format,{type:'json_schema',name:'codex_output_schema',strict:true,schema});
  const matching=body.metadata.stage==='matching';if(matching)matchingCount++;
  if(matching&&scenario==='http-failure'&&matchingCount===2){res.writeHead(503,{'content-type':'application/json'});res.end('{"error":{"message":"Fixtures / Mock matching failure"}}');return;}
  const usage:any={input_tokens:matching?100:20,input_tokens_details:{cached_tokens:matching?60:8},output_tokens:matching?10:5,output_tokens_details:{reasoning_tokens:2},total_tokens:matching?110:25};
  if(scenario==='explicit-zero-write')usage.cache_write_input_tokens=0;
  if(matching&&scenario==='invalid-counts')usage.input_tokens_details.cached_tokens=101;
  const response:any={id:'fixture-response-'+requests.length,status:'completed',usage,output:[{type:'message',content:[{type:'output_text',text:'{"matches":[],"uncertain":[],"related":[],"request":null}'}]}]};
  if(matching&&(scenario==='missing-usage'||scenario==='partial-usage'&&matchingCount===2))delete response.usage;
  if(matching&&scenario==='change-price'){
   const changed=JSON.parse(await readFile(path.join(output,'runtime/token-budget.json'),'utf8'));changed.pricingSnapshot.pricing.inputPerMillion='99';changed.pricingSnapshot.revision=99;await writeFile(path.join(output,'runtime/token-budget.json'),JSON.stringify(changed));
  }
  if(body.stream){res.writeHead(200,{'content-type':'text/event-stream'});res.end('event: response.completed\ndata: '+JSON.stringify({type:'response.completed',response})+'\n\n');}
  else{res.writeHead(200,{'content-type':'application/json'});res.end(JSON.stringify(response));}
 });
 await new Promise<void>(resolve=>upstream.listen(0,'127.0.0.1',resolve));t.after(()=>new Promise<void>(resolve=>upstream.close(()=>resolve())));
 const baseUrl='http://127.0.0.1:'+(upstream.address() as import('node:net').AddressInfo).port;
 const job=decode('ScannerJobV8',await readFile(path.join(root,'contracts/fixtures/v1/job-component-scan.json')));
 job.execution=execution;job.source.path=source;job.output.path=output;job.source.contentHash=await frozenTreeHash(source);
 job.model={providerId:'deepseek',connectionType:'openai_compatible',baseUrl,model:'deepseek-flash',wireApi:'responses',credentialEnv:'DEEPSEEK_API_KEY',configurationRevision:'fixture'};
 const r=job.componentScan;r.execution=execution;r.planVersion.source.contentHash=job.source.contentHash;r.planVersion.document={components:['api','web','worker'].map(p=>({name:'Fixtures / Mock '+p,paths:[p]}))};r.planVersion.coverage=await componentCoverage(source,r.planVersion.document);r.selectedIndices=[0,1,2];r.grantMicros=6000000;r.componentLimitMicros=2000000;job.policy.maxCostUsd=6;
 const budget=decode('TokenBudgetAssignment',await readFile(path.join(root,'contracts/fixtures/v1/token-budget.json')));budget.execution=execution;budget.limitMicros=6000000;Object.assign(budget.pricingSnapshot,{execution,provider:'deepseek',model:'deepseek-flash',revision:1});
 budget.pricingSnapshot.pricing={schemaVersion:1,currency:'USD',unit:'per_million_tokens',cacheMode:'cache_read',inputPerMillion:'0.3',cacheReadPerMillion:'0.006',cacheWritePerMillion:null,outputPerMillion:'1.2',note:'Fixtures / Mock frozen price'};
 await mkdir(path.join(output,'runtime'),{recursive:true});await writeFile(path.join(output,'runtime/token-budget.json'),JSON.stringify(budget));
 const jobFile=path.join(scratch,'job.json');await writeFile(jobFile,JSON.stringify(job));
 const env={...process.env,NODE_OPTIONS:'--import='+path.join(root,'contracts/tests/input-sdk-register.mjs'),AXIOM_INPUT_TEST_CAPTURE:path.join(scratch,'capture.json'),AXIOM_COMPONENT_SCAN_FIXTURE:'1',AXIOM_COMPONENT_USAGE_FIXTURE:'1',AXIOM_COMPONENT_USAGE_STREAM:scenario==='json'?'json':'sse',DEEPSEEK_API_KEY:'fixture-only-not-real'};
 const child=spawn(process.execPath,['--import','tsx','src/main.ts','--job',jobFile,'--token-budget',path.join(output,'runtime/token-budget.json')],{cwd:path.join(root,'code-audit-scanner'),env});
 let stderr='';child.stderr.on('data',x=>stderr+=x);child.stdout.resume();const code=await new Promise<number|null>((resolve,reject)=>{child.once('error',reject);child.once('close',resolve)});
 assert.notEqual(code,10,stderr);
 const result=decode('ScanResult',await readFile(path.join(output,'normalized/result.json')),execution),progress=decode('ProgressSnapshotV2',await readFile(path.join(output,'state/progress.json')),execution);
 const matching=decode('TokenCostEstimate',await readFile(path.join(output,'raw/codex-security/component-matching-token-usage.json')),execution);
 assert.deepEqual(result.extensions!.componentMatchingEstimate,matching);assert.deepEqual(progress.extensions!.componentMatchingEstimate,matching);
 assert.deepEqual(result.extensions!.executionUsage,progress.extensions!.executionUsage);
 const usage=decode('ExecutionTokenUsage',JSON.stringify(result.extensions!.executionUsage),execution);
 assert.equal(requests.filter(x=>x.metadata.stage==='component').length,3);assert.equal(requests.filter(x=>x.metadata.stage==='matching').length,2,'no extra inference requests or proxy replay');
 assert.ok(result.artifacts.some(a=>a.relativePath==='raw/codex-security/component-matching-token-usage.json'));
 assert.equal(await frozenTreeHash(source),job.source.contentHash);
 return {result,matching,usage,budget,requests,code};
}

test('component matching uses disjoint Responses receipts, frozen prices and strict Schema; never native fee callbacks',async t=>{
 for(const scenario of ['sse','json','explicit-zero-write','change-price'])await t.test(scenario,async t=>{
  const f=await run(t,scenario);assert.equal(f.code,0);assert.equal(f.result.status,'completed');assert.equal(f.result.findings.length,3);
  assert.equal(f.matching.status,'estimated');assert.equal(f.matching.amountMicros,49);assert.equal(f.matching.pricingSnapshot.revision,1);assert.equal(f.matching.pricingSnapshot.pricing!.inputPerMillion,'0.3');
  assert.equal(f.matching.usage!.inputTokens,200);assert.equal(f.matching.usage!.cachedInputTokens,120);assert.equal(f.matching.usage!.outputTokens,20);assert.equal(f.matching.usage!.requestCount,2);
  assert.equal(f.matching.usage!.cacheWriteInputTokens,scenario==='explicit-zero-write'?0:null);
  assert.equal(f.usage.complete,true);assert.equal(f.usage.inputTokens,260);assert.equal(f.usage.cachedInputTokens,144);assert.equal(f.usage.outputTokens,35);assert.equal(f.usage.totalTokens,295);assert.equal(f.usage.requestCount,5);
  assert.equal(f.usage.cacheWriteInputTokens,scenario==='explicit-zero-write'?0:null);
 });
});

test('missing or failed matching receipts preserve findings and keep full fees unknown',async t=>{
 for(const scenario of ['missing-usage','partial-usage','http-failure','invalid-counts'])await t.test(scenario,async t=>{
  const f=await run(t,scenario);assert.equal(f.result.status,scenario==='http-failure'?'partial':'completed');assert.equal(f.result.findings.length,3);
  assert.equal(f.usage.complete,false);assert.notEqual(f.matching.status,'estimated');assert.equal(f.matching.usage!.cacheWriteInputTokens,null);
  assert.equal(f.matching.status,['partial-usage','http-failure'].includes(scenario)?'partial':'unknown');
  if(f.matching.status==='unknown')assert.equal(f.matching.amountMicros,null);else assert.equal(f.matching.amountMicros,25);
 });
});

test('scope aggregation rejects foreign execution and does not turn unknown fields or prices into zeros',async()=>{
 const zero=undispatchedUsage(execution);assert.equal(zero.requestCount,0);assert.equal(zero.cacheWriteInputTokens,null);
 const total=sumExecutionUsage(execution,[zero,undefined],true);assert.equal(total.complete,false);assert.equal(total.inputTokens,null);
 assert.throws(()=>sumExecutionUsage(execution,[{...zero,execution:{...execution,attempt:2}}],true));
 const budget=decode('TokenBudgetAssignment',await readFile(path.join(root,'contracts/fixtures/v1/token-budget.json')));
 const price=structuredClone(budget.pricingSnapshot);price.execution=execution;price.pricing=null;
 assert.equal(componentMatchingEstimate(price,zero).status,'unknown');assert.equal(componentMatchingEstimate(price,zero).amountMicros,null);
});
