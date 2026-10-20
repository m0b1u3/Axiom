import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,mkdir,writeFile,rm,symlink} from 'node:fs/promises';
import {tmpdir} from 'node:os';import path from 'node:path';
import {loadSDKParameters} from '../src/runtime/sdk-parameters.js';
import {buildCodexOverrides,buildModelCatalog} from '../src/provider/model-provider.js';
import {convertResponsesRequestToChat} from '../src/proxy/chat-shim.js';
import {validateScanTask} from '../src/contract/validate-task.js';
import type {ScanTask} from '../src/contract/scan-task.js';

const base:ScanTask={taskId:'42',executionAttempt:2,sourcePath:'/fixture/source',outputPath:'/fixture/output',provider:'openai',model:'fixture-model',scanMode:'standard',timeoutMinutes:1};
test('explicit effort reaches native and compatible SDK configuration; unset retains historical defaults',()=>{
 assert.deepEqual(buildCodexOverrides(base),{model:'fixture-model'});
 for(const reasoningEffort of ['none','minimal','low','medium','high','xhigh'] as const){
  const task={...base,sdkParameters:{schemaVersion:1 as const,execution:{taskId:'42',attempt:2},parameters:{schemaVersion:1 as const,reasoningEffort}}};
  validateScanTask(task);assert.equal(buildCodexOverrides(task)?.model_reasoning_effort,reasoningEffort);
  const compatible={...task,provider:'custom',connectionType:'openai_compatible' as const,baseUrl:'https://fixture.invalid/v1',wireApi:'chat' as const,credentialEnv:'CUSTOM_API_KEY'};
  assert.equal(buildCodexOverrides(compatible)?.model_reasoning_effort,reasoningEffort);
  assert.equal((buildModelCatalog(compatible).models as Array<Record<string,unknown>>)[0]?.default_reasoning_level,reasoningEffort);
 }
 assert.equal(convertResponsesRequestToChat({model:'fixture',reasoning:{effort:'high'}}).reasoning_effort,undefined);
 assert.equal(convertResponsesRequestToChat({model:'fixture'},'medium').reasoning_effort,'medium');
 assert.equal(convertResponsesRequestToChat({model:'fixture',reasoning:{effort:'low'}},'medium').reasoning_effort,'low');
});
test('sidecar rejects foreign execution, unknown settings, misplaced files and component inputs on ordinary scans before the SDK',async t=>{
 const root=await mkdtemp(path.join(tmpdir(),'axiom-sdk-parameters-'));t.after(()=>rm(root,{recursive:true,force:true}));
 const task={...base,outputPath:path.join(root,'output')};await mkdir(path.join(task.outputPath,'runtime'),{recursive:true});
 const file=path.join(task.outputPath,'runtime/sdk-parameters.json');
 const valid={schemaVersion:1,execution:{taskId:'42',attempt:2},parameters:{schemaVersion:1,reasoningEffort:'medium'}};
 await writeFile(file,JSON.stringify(valid));await loadSDKParameters(file,task);assert.equal(task.sdkParameters?.parameters.reasoningEffort,'medium');
 for(const value of [{...valid,execution:{taskId:'42',attempt:3}},{...valid,parameters:{schemaVersion:1,reasoningEffort:'invented'}},{...valid,parameters:{schemaVersion:1,componentInput:{schemaVersion:1,knowledge:[],scanPrompt:'fixture'}}}]){await writeFile(file,JSON.stringify(value));await assert.rejects(loadSDKParameters(file,task));}
 await writeFile(path.join(root,'foreign.json'),JSON.stringify(valid));await assert.rejects(loadSDKParameters(path.join(root,'foreign.json'),task));
 await rm(file);await symlink(path.join(root,'foreign.json'),file);await assert.rejects(loadSDKParameters(file,task));
});
