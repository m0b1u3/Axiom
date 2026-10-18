import {test} from 'node:test';import assert from 'node:assert/strict';
import {validateScanTask} from '../src/contract/validate-task.js';import {selectedSDKAuth,persistTaskIdentity} from '../src/runtime/sdk-identity.js';import type {ScanTask} from '../src/contract/scan-task.js';
function task():ScanTask{return {taskId:'42',executionAttempt:1,sourcePath:'/fixture/source',outputPath:'/fixture/output',provider:'openai',model:'fixture-model',scanMode:'standard',timeoutMinutes:1,sdkIdentity:{schemaVersion:1,execution:{taskId:'42',attempt:1},identity:{id:'1',name:'Fixtures / Mock identity',agentId:'fixture-agent',generation:1,kind:'api_key'}}}}
test('validated identity metadata never becomes a credential field',()=>{const v=task();assert.doesNotThrow(()=>validateScanTask(v));assert.equal(selectedSDKAuth(v),'api-key');v.sdkIdentity!.identity.kind='chatgpt';assert.equal(selectedSDKAuth(v),'chatgpt');assert.doesNotThrow(()=>validateScanTask(v));Object.assign(v.sdkIdentity!.identity,{apiKey:'fixture-must-be-rejected'});assert.throws(()=>validateScanTask(v));});
test('SDK identity is bound to its exact execution and explicit generation',()=>{const v=task();v.sdkIdentity!.execution.attempt=2;assert.throws(()=>validateScanTask(v));v.sdkIdentity!.execution.attempt=1;v.sdkIdentity!.identity.generation=0;assert.throws(()=>validateScanTask(v));});

// Real public SDK file-copy helper only; no Codex process or network is started.
import {mkdtemp,mkdir,writeFile,readFile,rm,readdir,realpath} from 'node:fs/promises';
import path from 'node:path';import os from 'node:os';
test('refreshed ChatGPT credentials survive execution cleanup without copying the workbench',async()=>{
 const base=await mkdtemp(path.join(os.tmpdir(),'axiom-identity-fixture-')),home=await realpath(base),sdk=path.join(home,'sdk'),root=path.join(home,'executions/42-1/sdk');
 const previous={home:process.env.AXIOM_SDK_IDENTITY_HOME,state:process.env.CODEX_SECURITY_STATE_DIR};const v=task();v.sdkIdentity!.identity.kind='chatgpt';
 try{
  await mkdir(path.join(sdk,'codex-home'),{recursive:true,mode:0o700});await mkdir(path.join(root,'codex-home'),{recursive:true,mode:0o700});
  // Go and JS may serialize the same reference with different key order.
  await writeFile(path.join(home,'session.json'),JSON.stringify(Object.fromEntries(Object.entries(v.sdkIdentity!.identity).reverse())),{mode:0o600});
  await writeFile(path.join(sdk,'codex-home/auth.json'),'Fixtures / Mock old opaque session',{mode:0o600});
  await writeFile(path.join(root,'codex-home/auth.json'),'Fixtures / Mock refreshed opaque session',{mode:0o600});await writeFile(path.join(root,'workbench.sqlite3'),'Fixtures / Mock workbench',{mode:0o600});
  process.env.AXIOM_SDK_IDENTITY_HOME=home;process.env.CODEX_SECURITY_STATE_DIR=root;
  await persistTaskIdentity(v);await rm(path.join(home,'executions'),{recursive:true,force:true});
  assert.equal(await readFile(path.join(sdk,'codex-home/auth.json'),'utf8'),'Fixtures / Mock refreshed opaque session');assert.deepEqual(await readdir(sdk),['codex-home']);
  await assert.rejects(()=>readFile(path.join(sdk,'workbench.sqlite3')));
 }finally{if(previous.home===undefined)delete process.env.AXIOM_SDK_IDENTITY_HOME;else process.env.AXIOM_SDK_IDENTITY_HOME=previous.home;if(previous.state===undefined)delete process.env.CODEX_SECURITY_STATE_DIR;else process.env.CODEX_SECURITY_STATE_DIR=previous.state;await rm(home,{recursive:true,force:true})}
});
