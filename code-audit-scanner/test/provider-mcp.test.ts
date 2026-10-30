import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,readFile,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {providerMcpOverrides} from '../src/runtime/provider-mcp.js';
import {buildCodexOverrides} from '../src/provider/model-provider.js';
import {sdkPackageRoot} from '../src/runtime/sdk-runtime.js';
import type {ScanTask} from '../src/contract/scan-task.js';

test('native MCP config forwards only the selected additional provider name without modifying the plugin',async t=>{
 const root=await mkdtemp(path.join(tmpdir(),'axiom-mcp-env-'));t.after(()=>rm(root,{recursive:true,force:true}));
 const task:ScanTask={taskId:1,executionAttempt:1,sourcePath:'/fixture',outputPath:root,provider:'deepseek',model:'fixture',scanMode:'deep',timeoutMinutes:1};
 const file=path.join(sdkPackageRoot(task),'_bundled_plugin/.mcp.json'),before=await readFile(file,'utf8');
 const config=buildCodexOverrides(task),result=await providerMcpOverrides(task,config);
 const server=(result!.mcp_servers as any)['codex-security'];
 const original=JSON.parse(before).mcpServers['codex-security'];
 assert.deepEqual(server.env_vars,[...original.env_vars,'DEEPSEEK_API_KEY']);
 assert.equal(path.isAbsolute(server.command),true);assert.equal(path.isAbsolute(server.cwd),true);
 assert.equal(server.tool_timeout_sec,original.tool_timeout_sec);
 assert.deepEqual(result!.model_providers,config!.model_providers);
 assert.equal(result!.features,undefined); // SDK keeps native V2 defaults.
 assert.equal(await readFile(file,'utf8'),before);
 const custom={...task,outputPath:path.join(root,'custom'),provider:'custom',baseUrl:'https://fixture.invalid',credentialEnv:'FIXTURE_SELECTED_KEY',wireApi:'responses'} as ScanTask;
 const selected=(await providerMcpOverrides(custom,buildCodexOverrides(custom)))!.mcp_servers as any;
 assert.deepEqual(selected['codex-security'].env_vars,[...original.env_vars,'FIXTURE_SELECTED_KEY']);
 assert.ok(!selected['codex-security'].env_vars.includes('DEEPSEEK_API_KEY'));
});

test('native OpenAI and mock configuration remain unchanged',async()=>{
 const task={provider:'openai',model:'fixture',scanMode:'deep',timeoutMinutes:1} as ScanTask;
 const config=buildCodexOverrides(task);assert.equal(await providerMcpOverrides(task,config),config);
 assert.equal(await providerMcpOverrides({...task,mock:true},undefined),undefined);
});

test('resumed audit workspace retains origin paths and binds only the new execution receipt',async t=>{
 const {prepareAuditWorkspace}=await import('../src/runtime/audit-bootstrap.js');
 const {bindSDKRuntime}=await import('../src/runtime/sdk-runtime.js');
 const {mkdir,writeFile}=await import('node:fs/promises');
 const root=await mkdtemp(path.join(tmpdir(),'axiom-resume-workspace-'));t.after(()=>rm(root,{recursive:true,force:true}));
 const source=path.join(root,'source');await mkdir(source);await writeFile(path.join(source,'fixture.py'),'# Fixtures / Mock\n');
 const origin=path.join(root,'attempt-1'),task:ScanTask={taskId:42,executionAttempt:1,sourcePath:source,outputPath:origin,provider:'deepseek',model:'fixture',scanMode:'deep',timeoutMinutes:1};
 await bindSDKRuntime(task);const receipt=path.join(origin,'logs/sdk-runtime.json'),before=await readFile(receipt,'utf8');
 const current={...task,executionAttempt:2,outputPath:path.join(root,'attempt-2')};
 const workspace=await prepareAuditWorkspace(current,origin);
 try{
  assert.ok(workspace.prompt.includes(origin));assert.ok(!workspace.prompt.includes(current.outputPath));
  assert.equal(await readFile(receipt,'utf8'),before);
  assert.equal(JSON.parse(await readFile(path.join(current.outputPath,'logs/sdk-runtime.json'),'utf8')).execution.attempt,2);
 }finally{workspace.restore()}
});
