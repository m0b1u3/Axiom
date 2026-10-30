import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,mkdir,readFile,writeFile,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
import {sdkRuntimeId,bindSDKRuntime,sdkForTask,sdkPackageRoot,type SDKRuntimeId} from '../src/runtime/sdk-runtime.js';
import {buildModelCatalog} from '../src/provider/model-provider.js';
import type {ScanTask} from '../src/contract/scan-task.js';
import {mapEngineError} from '../src/engine/error-mapping.js';

const task=(extra:Partial<ScanTask>={}):ScanTask=>({taskId:1,executionAttempt:1,sourcePath:'/fixture',outputPath:'/fixture-output',provider:'openai',model:'fixture-only',scanMode:'standard',timeoutMinutes:1,...extra});
test('native connections cannot inherit V1; model names never select a compatibility runtime',()=>{
 const env={AXIOM_SDK_LEGACY_CONNECTIONS:JSON.stringify([{provider:'openai',baseUrl:'https://api.openai.com/v1'}])};
 assert.equal(sdkRuntimeId(task({connectionType:'openai',baseUrl:'https://api.openai.com/v1'}),env),'official-v2');
 assert.equal(sdkRuntimeId(task({connectionType:'openai_compatible',baseUrl:'https://fixture.invalid',model:'gpt-6.1-terra'}),{}),'official-v2');
 assert.equal(sdkRuntimeId(task({provider:'deepseek'}),{}),'official-v2');
 assert.equal((buildModelCatalog(task({provider:'deepseek'})).models as any[])[0].multi_agent_version,'v2');
 assert.equal(sdkRuntimeId(task({provider:'deepseek',baseUrl:'https://other.invalid'}),{}),'official-v2');
 assert.equal(sdkRuntimeId(task({provider:'zhipu'}),{}),'official-v2');
});
test('legacy selection requires the exact configured endpoint and optional model',()=>{
 const current=task({provider:'custom',connectionType:'openai_compatible',baseUrl:'https://fixture.invalid/v1',model:'fixture-model'});
 const env={AXIOM_SDK_LEGACY_CONNECTIONS:JSON.stringify([{provider:'custom',baseUrl:'https://fixture.invalid/v1/',model:'fixture-model'}])};
 assert.equal(sdkRuntimeId(current,env),'legacy-v1');
 assert.equal(sdkRuntimeId({...current,model:'another'},env),'official-v2');
 assert.equal(sdkRuntimeId({...current,baseUrl:'https://other.invalid/v1'},env),'official-v2');
 assert.equal(sdkRuntimeId(task({provider:'deepseek'}),{AXIOM_SDK_LEGACY_CONNECTIONS:'[{"provider":"deepseek","baseUrl":"https://api.deepseek.com"}]'}),'legacy-v1');
 assert.throws(()=>sdkRuntimeId(current,{AXIOM_SDK_LEGACY_CONNECTIONS:'{}'}),/数组/);
 assert.throws(()=>sdkRuntimeId(current,{AXIOM_SDK_LEGACY_CONNECTIONS:'[{"provider":"custom"}]'}),/无效/);
 const entry=(buildModelCatalog(task({provider:'custom',baseUrl:'https://fixture.invalid'})).models as any[])[0];
 assert.equal(entry.multi_agent_version,'v2');
});
test('both immutable runtimes retain distinct configs, instructions, seals and execution provenance', {timeout:120000},async t=>{
 const root=await mkdtemp(path.join(tmpdir(),'axiom-sdk-isolated-'));t.after(()=>rm(root,{recursive:true,force:true}));
 const source=path.join(root,'source');await mkdir(source);await writeFile(path.join(source,'fixture.py'),'# Fixtures / Mock only\n');
 const descriptors=JSON.parse(await readFile(new URL('../node_modules/.axiom-sdk-runtimes.json',import.meta.url),'utf8')).runtimes;
 const keys=['CODEX_SECURITY_STATE_DIR','CODEX_HOME','HOME','OPENAI_API_KEY','CODEX_API_KEY','CHATGPT_ACCESS_TOKEN','CHATGPT_ACCOUNT_ID'];
 const before=keys.map(k=>process.env[k]);t.after(()=>keys.forEach((k,i)=>before[i]===undefined?delete process.env[k]:process.env[k]=before[i]));
 for(const id of ['official-v2','legacy-v1'] as SDKRuntimeId[]){
  const current=task({sourcePath:source,outputPath:path.join(root,id),mock:true,sdkRuntime:{id,fingerprint:descriptors[id].fingerprint}});
  for(const key of keys)delete process.env[key];
  process.env.CODEX_SECURITY_STATE_DIR=path.join(root,id,'state');process.env.CODEX_HOME=path.join(root,id,'home');process.env.HOME=path.join(root,id,'empty-home');
  const library=await sdkForTask(current),packageRoot=sdkPackageRoot(current);
  assert.equal(mapEngineError(new library.IncompleteScanError('fixture incomplete')).code,'incomplete');
  assert.equal(mapEngineError(new library.AuthenticationRequiredError('fixture auth')).code,'auth_failed');
  assert.equal(mapEngineError(new library.ScanInterruptedError('fixture interruption',current.outputPath)).status,'canceled');
  const {DEFAULT_CODEX_CONFIG}=await import(pathToFileURL(path.join(packageRoot,'dist/config.js')).href);
  assert.equal(DEFAULT_CODEX_CONFIG.features.multi_agent_v2?.enabled,id==='official-v2'?true:undefined);
  assert.equal(DEFAULT_CODEX_CONFIG.features.multi_agent,id==='legacy-v1'?true:undefined);
  const instructions=await readFile(path.join(packageRoot,'_bundled_plugin/references/core-scan.md'),'utf8');
  assert.ok(instructions.includes(id==='official-v2'?'fork_turns: "none"':'fork_context: false'));
  const security=new library.CodexSecurity();
  try{
   const result=await security.run(source,{mock:true,outputDir:path.join(current.outputPath,'raw/codex-security')});
   assert.equal(result.manifest.scan.status,'completed');
   const loaded=await library.loadContract(path.join(current.outputPath,'raw/codex-security'),{pluginRoot:await library.bundledPluginRoot()});
   assert.equal(loaded.manifest.scan.id,result.manifest.scan.id);
  }finally{await security.close();}
  const receipt=JSON.parse(await readFile(path.join(current.outputPath,'logs/sdk-runtime.json'),'utf8'));
  assert.equal(receipt.id,id);assert.equal(receipt.fingerprint,descriptors[id].fingerprint);assert.equal(receipt.sdkVersion,'0.5.0');assert.equal(receipt.codexVersion,'0.163.0');
  await assert.rejects(bindSDKRuntime({...current,model:'different-model'}),/禁止覆盖/);
  await assert.rejects(bindSDKRuntime({...current,sdkRuntime:{id,fingerprint:'0'.repeat(64)}}),/基线发生变化/);
 }
 assert.notEqual(descriptors['official-v2'].fingerprint,descriptors['legacy-v1'].fingerprint);
});

test('old Deep attempts remain on their original runtime; receipt tampering prevents continuation',async t=>{
 const root=await mkdtemp(path.join(tmpdir(),'axiom-sdk-old-deep-'));t.after(()=>rm(root,{recursive:true,force:true}));
 const base=task({outputPath:path.join(root,'attempt-2'),deepResume:{inspection:{origin:{attempt:1}}} as any});
 const old=await bindSDKRuntime(base);assert.equal(old.id,'legacy-v1');
 const fresh=await bindSDKRuntime(task({outputPath:path.join(root,'fresh')}));assert.equal(fresh.id,'official-v2');
 await mkdir(path.join(root,'attempt-1/logs'),{recursive:true});await writeFile(path.join(root,'attempt-1/logs/sdk-runtime.json'),JSON.stringify({id:'legacy-v1',fingerprint:'0'.repeat(64)}));
 await assert.rejects(bindSDKRuntime(task({outputPath:path.join(root,'attempt-3'),deepResume:base.deepResume!})),/不能迁移续扫/);
});
