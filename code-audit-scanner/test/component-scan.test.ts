import {test,type TestContext} from 'node:test';import assert from 'node:assert/strict';
import {mkdtemp,mkdir,readFile,writeFile,rm} from 'node:fs/promises';import {tmpdir} from 'node:os';import path from 'node:path';import {spawn} from 'node:child_process';import {fileURLToPath} from 'node:url';
import {decode,componentAllocations} from '@axiom/contracts';
import {componentCoverage,frozenTreeHash} from '../src/runtime/component-plan.js';import {componentCostMicros,componentLimitUSD,runComponentAudit} from '../src/engine/component-scan.js';import {scanTaskFromJob} from '../src/contract/scanner-job.js';
const root=fileURLToPath(new URL('../../',import.meta.url));
async function fixture(t:TestContext){
 const scratch=await mkdtemp(path.join(tmpdir(),'axiom-component-scan-'));t.after(()=>rm(scratch,{recursive:true,force:true}));
 const source=path.join(scratch,'source'),output=path.join(scratch,'output');
 for(const name of ['api/a.ts','web/b.ts','worker/c.ts']){await mkdir(path.dirname(path.join(source,name)),{recursive:true});await writeFile(path.join(source,name),'// Fixtures / Mock\n')}
 const job=decode('ScannerJobV8',await readFile(path.join(root,'contracts/fixtures/v1/job-component-scan.json')));job.source.path=source;job.output.path=output;job.source.contentHash=await frozenTreeHash(source);job.execution={taskId:'42',attempt:1};job.model={providerId:'openai',connectionType:'openai',model:'fixture-only-model',wireApi:'responses',credentialEnv:'OPENAI_API_KEY',configurationRevision:'fixture-component-scan'};
 const r=job.componentScan;r.execution=job.execution;r.planVersion.source.contentHash=job.source.contentHash;r.planVersion.document={components:['api','web','worker'].map(p=>({name:'Fixtures / Mock '+p,paths:[p]}))};r.planVersion.coverage=await componentCoverage(source,r.planVersion.document);r.selectedIndices=[0,1,2];r.grantMicros=6000000;r.componentLimitMicros=2000000;job.policy.maxCostUsd=6;
 return {scratch,source,output,job};
}
async function run(t:TestContext,scenario:string){
 const f=await fixture(t),jobFile=path.join(f.scratch,'job.json'),capture=path.join(f.scratch,'capture.json');await writeFile(jobFile,JSON.stringify(f.job));
 const child=spawn(process.execPath,['--import','tsx','src/main.ts','--job',jobFile],{cwd:path.join(root,'code-audit-scanner'),env:{...process.env,NODE_OPTIONS:'--import='+path.join(root,'contracts/tests/input-sdk-register.mjs'),OPENAI_API_KEY:'fixture-key-not-real',AXIOM_INPUT_TEST_CAPTURE:capture,AXIOM_COMPONENT_SCAN_FIXTURE:'1',AXIOM_COMPONENT_SCAN_SCENARIO:scenario}});
 let stdout='',stderr='';child.stdout.on('data',x=>stdout+=x);child.stderr.on('data',x=>stderr+=x);const code=await new Promise<number|null>((resolve,reject)=>{child.once('error',reject);child.once('close',resolve)});
 const result=decode('ScanResult',await readFile(path.join(f.output,'normalized/result.json')),f.job.execution),out=decode('ComponentScanResult',JSON.stringify(result.extensions!.componentScan),f.job.execution);
 return {...f,result,out,capture,code,stdout,stderr};
}
test('real pinned component scheduler reserves distinct budgets, binds evidence and preserves matching uncertainty',async t=>{
 const f=await run(t,'normal');assert.equal(f.code,0,f.stderr+JSON.stringify(f.out));assert.equal(f.result.status,'completed');assert.equal(f.result.findings.length,3);assert.equal(f.result.cost,null);assert.equal(f.out.snapshot.matching.costState,'unknown');
 assert.deepEqual(f.out.snapshot.components.map(r=>r.finalMicros),[125000,125000,125000]);assert.equal(f.out.snapshot.observedMicros,375000);assert.equal(f.out.snapshot.reservedMicros,0);
 const calls=(await readFile(f.capture+'.component-calls','utf8')).trim().split('\n').map(x=>JSON.parse(x));assert.equal(calls.length,3);assert.ok(calls.every(c=>c.options.mode==='standard'&&c.options.maxCostUsd===2));assert.equal(new Set(calls.map(c=>c.source)).size,3);assert.ok(calls.every(c=>c.source!==f.source));
 assert.equal(new Set(f.result.findings.map(x=>x.fingerprint)).size,3);for(const finding of f.result.findings){const binding=decode('ComponentFindingBinding',JSON.stringify(finding.extensions!.axiomComponent),f.job.execution);assert.equal(binding.versionSha256,f.job.componentScan.planVersion.sha256);assert.equal(binding.source.contentHash,f.job.source.contentHash);assert.ok(f.result.artifacts.some(a=>a.sha256===binding.resultArtifact.sha256))}
 assert.match(JSON.stringify(f.out.native),/uncertain|confidence|sources/);assert.equal(await frozenTreeHash(f.source),f.job.source.contentHash);
});
test('component failure retains other results and never settles unknown fees as zero',async t=>{
 const f=await run(t,'partial');assert.equal(f.code,2,f.stderr);assert.equal(f.result.status,'partial');assert.deepEqual(f.out.snapshot.components.map(r=>r.status),['completed','failed','completed']);assert.equal(f.out.snapshot.components[1]!.finalMicros,null);assert.equal(f.result.cost,null);assert.equal(f.result.findings.length,2);
});
test('unknown component cost stops subsequent dispatch and keeps fees unknown',async t=>{
 const f=await run(t,'unknown');assert.equal(f.result.status,'partial');assert.equal(f.out.snapshot.components[0]!.finalMicros,null);assert.equal(f.result.cost,null);assert.ok(f.out.snapshot.components.some(r=>r.error));assert.equal(f.out.snapshot.reservedMicros,0);
});
test('SDK private-source mutation cannot become immutable finding evidence',async t=>{
 const f=await run(t,'mutate');assert.equal(f.result.status,'partial');assert.equal(f.out.snapshot.components[0]!.resultArtifact,null);assert.equal(f.out.snapshot.components[0]!.status,'failed');assert.equal(await frozenTreeHash(f.source),f.job.source.contentHash);assert.equal(f.result.findings.some(x=>(x.extensions!.axiomComponent as {componentIndex:number}).componentIndex===0),false);
});
test('matching failure preserves independent findings and reports partial result with unknown matching cost',async t=>{
 const f=await run(t,'matching-failure');assert.equal(f.result.status,'partial');assert.equal(f.result.findings.length,3);assert.equal(f.out.snapshot.matching.status,'incomplete');assert.equal(f.out.snapshot.matching.costState,'unknown');
});
test('mock mode never invokes paid component execution or automatic matching',async t=>{
 const {job,output}=await fixture(t);const task={...scanTaskFromJob(job),mock:true};const value=await runComponentAudit(task,new AbortController().signal);assert.equal(value.status,'failed');assert.equal(value.cost?.estimatedUsd,0);assert.match(value.error!.message,/没有调用模型/);decode('ScanResult',await readFile(path.join(output,'normalized/result.json')),job.execution);
});
test('micro-dollar reservation and upward accounting remain safe at currency boundaries',()=>{
 assert.equal(componentCostMicros(0.0000001),1);assert.equal(componentCostMicros(0),0);assert.equal(componentCostMicros(null),null);assert.equal(componentCostMicros(Infinity),null);assert.equal(componentCostMicros(1e20),null);for(const n of [1,3,125000,9007199254740989,9007199254740991])assert.ok(componentCostMicros(componentLimitUSD(n))!<=n);
 const base={selectedIndices:[0,1,2],grantMicros:5,componentLimitMicros:9} as Parameters<typeof componentAllocations>[0];assert.deepEqual(componentAllocations(base),[2,2,1]);assert.deepEqual(componentAllocations({...base,componentLimitMicros:1}),[1,1,1]);
});
