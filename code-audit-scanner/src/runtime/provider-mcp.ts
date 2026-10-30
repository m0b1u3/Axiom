import {readFile} from 'node:fs/promises';
import path from 'node:path';
import type {JsonObject} from '@openai/codex-security';
import type {ScanTask} from '../contract/scan-task.js';
import {nativeOpenAI,resolveTaskProvider} from '../provider/model-provider.js';
import {sdkForTask} from './sdk-runtime.js';

/** Extend the native per-invocation MCP configuration with the selected key's
 * name. The SDK's bundled manifest only forwards its built-in provider keys;
 * Deep workers need the same command-auth environment as their coordinator.
 * Neither the bundled plugin nor any credential value is written to config. */
export async function providerMcpOverrides(task:ScanTask,config:JsonObject|undefined):Promise<JsonObject|undefined>{
 if(!config||task.mock||nativeOpenAI(task))return config;
 const name=task.credentialEnv??resolveTaskProvider(task).credentialEnv;
 if(!/^[A-Z][A-Z0-9_]{0,127}$/.test(name))throw new Error('模型凭据环境变量名称无效');
 const sdk=await sdkForTask(task),root=await sdk.bundledPluginRoot();
 const manifest=JSON.parse(await readFile(path.join(root,'.mcp.json'),'utf8'));
 const server=manifest.mcpServers?.['codex-security'];
 if(!server||!Array.isArray(server.env_vars)||server.env_vars.some((v:unknown)=>typeof v!=='string'))throw new Error('SDK MCP 环境变量合同无效');
 if(server.env_vars.includes(name))return config;
 // SDK 0.5.0 customValidationConfig uses this same native override mechanism.
 // An explicit entry takes precedence over the installed plugin's server.
 return {...config,mcp_servers:{'codex-security':{
  ...server,command:path.resolve(root,server.command),cwd:root,
  env_vars:[...server.env_vars,name],
 }}};
}
