import {test} from 'node:test';import assert from 'node:assert/strict';
import {mkdtemp,mkdir,readFile,writeFile,rm,access} from 'node:fs/promises';import path from 'node:path';import {tmpdir} from 'node:os';import {createHash} from 'node:crypto';
import {decode} from '@axiom/contracts';import {CodexSecurityEngine} from '../src/engine/codex-security-engine.js';import type {ScanTask} from '../src/contract/scan-task.js';import {restoreSdkParentState,exportSdkParentState} from '../src/runtime/sdk-parent-state.js';
import {CodexSecurity} from '@openai/codex-security';
import {cp} from 'node:fs/promises';
import {loadRawCodexResult} from '../src/artifact/load-raw-result.js';

test('pinned public SDK mock supports copied parent state without credentials or model calls',{timeout:120000},async t=>{
 const root=await mkdtemp(path.join(tmpdir(),'axiom-sdk-parent-mock-'));t.after(()=>rm(root,{recursive:true,force:true}));const source=path.join(root,'source');await mkdir(source);await writeFile(path.join(source,'fixture.py'),'# Synthetic SDK mock fixture only\n');
 const previous=new Map<string,string|undefined>();for(const name of ['CODEX_SECURITY_STATE_DIR','OPENAI_API_KEY','CODEX_API_KEY','CHATGPT_ACCESS_TOKEN','CHATGPT_ACCOUNT_ID','CODEX_HOME']){previous.set(name,process.env[name]);delete process.env[name];}t.after(()=>{for(const [name,value] of previous){if(value===undefined)delete process.env[name];else process.env[name]=value;}});
 const parent:ScanTask={taskId:'901',executionAttempt:1,resultSchemaVersion:1,sourcePath:source,sourceContentHash:'a'.repeat(64),outputPath:path.join(root,'parent'),provider:'openai',connectionType:'openai',model:'fixture-mock-model',scanMode:'standard',timeoutMinutes:2,mock:true,resolvedModel:{providerId:'openai',connectionType:'openai',model:'fixture-mock-model',wireApi:'responses',credentialEnv:'OPENAI_API_KEY',configurationRevision:'fixture-mock'}};
 // A raw child-session observation cannot override the completed native result.
 // The SDK owns child reconciliation and coverage; the host keeps the event.
 const nativeRun=CodexSecurity.prototype.run;t.after(()=>{CodexSecurity.prototype.run=nativeRun});
 CodexSecurity.prototype.run=async function(repository,options){const result=await nativeRun.call(this,repository,options);options?.onSessionEvent?.({threadId:'fixture-empty-worker',parentThreadId:'fixture-parent',event:{type:'event_msg',payload:{type:'task_complete',last_agent_message:null}}});return result;};
 process.env.CODEX_SECURITY_STATE_DIR=path.join(parent.outputPath,'runtime/sdk');const first=await new CodexSecurityEngine().scan(parent);assert.equal(first.status,'completed',first.error?.message);
 CodexSecurity.prototype.run=nativeRun;
 assert.match(await readFile(path.join(parent.outputPath,'logs/sdk-session-events.jsonl'),'utf8'),/fixture-empty-worker/);
 const recovered=await loadRawCodexResult(path.join(parent.outputPath,'raw/codex-security'));assert.ok(recovered);assert.equal(recovered.toJSON!().recoveryValidation,'sdk_load_contract');assert.deepEqual(recovered.findings,first.sdkRaw!.findings);assert.deepEqual(recovered.toJSON!().manifest,first.sdkRaw!.manifest);
 const corrupt=path.join(root,'corrupt-native');await cp(path.join(parent.outputPath,'raw/codex-security'),corrupt,{recursive:true});
 const findingsPath=path.join(corrupt,'findings.json'),changed=JSON.parse(await readFile(findingsPath,'utf8'));changed.findings[0].summary='Fixtures / Mock altered after native seal';await writeFile(findingsPath,JSON.stringify(changed));
 assert.equal(await loadRawCodexResult(corrupt),undefined,'native rejection of a broken seal must not fall back to parsing');
 await mkdir(path.join(parent.outputPath,'runtime/sdk/codex-home'),{recursive:true});await writeFile(path.join(parent.outputPath,'runtime/sdk/codex-home/auth.json'),'fixture-parent-secret');await exportSdkParentState(parent,(first.sdkRaw!.manifest as {scan:{id:string}}).scan.id);
 const portable=decode('ScanResult',await readFile(path.join(parent.outputPath,'normalized/result.json'),'utf8'));const native=decode('SDKNativeResult',JSON.stringify(portable.extensions!.sdkNative),portable.execution);assert.equal(native.reports.markdown.status,'available',native.reports.markdown.reason??'official mock report unavailable');assert.notEqual(native.reports.sarif.status,'invalid');assert.equal(native.scanId,(first.sdkRaw!.manifest as {scan:{id:string}}).scan.id);
 const state=decode('SDKParentState',await readFile(path.join(parent.outputPath,'normalized/parent-state.json'),'utf8'));assert.match(state.scanId,/^[a-f0-9-]{36}$/);assert.equal(Buffer.from(state.data,'base64').includes(Buffer.from('fixture-parent-secret')),false);
 assert.equal(state.runtime?.id,'official-v2');
 const knowledge='# 合成参考资料\n没有真实审计或模型调用。';
 const child:ScanTask={...parent,taskId:'902',outputPath:path.join(root,'child'),scanInput:{schemaVersion:1,knowledge:[{versionId:'27',title:'合成文档',content:knowledge,sha256:createHash('sha256').update(knowledge).digest('hex')}],parentScanId:state.scanId,workflowId:'fixture-public-sdk-mock',expectedPluginVersion:'0.1.95'},parentState:state};await mkdir(path.join(child.outputPath,'runtime/knowledge'),{recursive:true});await writeFile(path.join(child.outputPath,'runtime/knowledge/27.md'),knowledge);
 const {runtime:ignored,...unbound}=state;
 await assert.rejects(restoreSdkParentState({...child,parentState:unbound}),/runtime differs/);
 await assert.rejects(restoreSdkParentState({...child,parentState:{...state,runtime:{id:'legacy-v1',fingerprint:'0'.repeat(64)}}}),/runtime differs/);
 process.env.CODEX_SECURITY_STATE_DIR=path.join(child.outputPath,'runtime/sdk');const second=await new CodexSecurityEngine().scan(child);assert.equal(second.status,'completed',second.error?.message);assert.notEqual((second.sdkRaw!.manifest as {scan:{id:string}}).scan.id,state.scanId);await assert.rejects(access(path.join(child.outputPath,'runtime/sdk/codex-home/auth.json')));
 const altered={...child,outputPath:path.join(root,'tampered'),parentState:{...state,sha256:'0'.repeat(64)}};await mkdir(altered.outputPath);await assert.rejects(restoreSdkParentState(altered),/invalid or changed/);
});
