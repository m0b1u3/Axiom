import assert from 'node:assert/strict';
import {readFileSync,mkdirSync,writeFileSync,mkdtempSync} from 'node:fs';
import path from 'node:path';
import {tmpdir} from 'node:os';
import {pathToFileURL,fileURLToPath} from 'node:url';
import {spawnSync} from 'node:child_process';
import {createHash} from 'node:crypto';
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'../..'),scanner=path.join(root,'code-audit-scanner');
const sdk=path.join(scanner,'node_modules/@openai/codex-security');
const {DEFAULT_CODEX_CONFIG}=await import(pathToFileURL(path.join(sdk,'dist/config.js')));
const {resolveCodexCommand,bootstrapPlugin}=await import(pathToFileURL(path.join(sdk,'dist/runtime.js')));
assert.equal(DEFAULT_CODEX_CONFIG.features.multi_agent,undefined);
assert.equal(DEFAULT_CODEX_CONFIG.features.multi_agent_v2.enabled,true);
const command=resolveCodexCommand({}).command;
assert.ok(command.includes('/@openai/codex-linux-x64/'),'effective binary must be the pinned Linux dependency');
const version=spawnSync(command,['--version'],{encoding:'utf8'});assert.equal(version.status,0);assert.match(version.stdout,/0\.163\.0/);
const plugin=path.join(sdk,'_bundled_plugin');
const credentialNames=['DEEPSEEK_API_KEY','DASHSCOPE_API_KEY','MOONSHOT_API_KEY','ZHIPU_API_KEY','CUSTOM_API_KEY'];
const forwarded=JSON.parse(readFileSync(path.join(plugin,'.mcp.json'),'utf8')).mcpServers['codex-security'].env_vars;
for(const name of credentialNames)assert.ok(!forwarded.includes(name),`native MCP must not inherit compatibility env ${name}`);
for(const rel of ['references/core-scan.md','references/threat-model.md','skills/fix-finding/SKILL.md','skills/security-scan/references/scan-artifacts-and-ledger.md']){
 const text=readFileSync(path.join(plugin,rel),'utf8');assert.match(text,/fork_turns/);assert.doesNotMatch(text,/fork_context: false/);
}
assert.equal(JSON.parse(readFileSync(path.join(plugin,'scripts/deep_scan_defaults.json'),'utf8')).workers,4);
assert.doesNotMatch(readFileSync(path.join(scanner,'src/main.ts'),'utf8'),/args\.workers \?\? 2/);
const scratch=mkdtempSync(path.join(tmpdir(),'axiom-sdk050-runtime-'));const fixture=path.join(scratch,'source'),output=path.join(scratch,'output');mkdirSync(fixture,{recursive:true});writeFileSync(path.join(fixture,'example.py'),'def hello():\n    return "hello"\n');
const env={PATH:process.env.PATH,HOME:path.join(scratch,'home'),TMPDIR:path.join(scratch,'tmp'),CODEX_HOME:path.join(scratch,'codex-home'),CODEX_SECURITY_STATE_DIR:path.join(scratch,'sdk-state')};
for(const key of ['HOME','TMPDIR','CODEX_HOME','CODEX_SECURITY_STATE_DIR'])mkdirSync(env[key],{recursive:true,mode:0o700});
const features=spawnSync(command,['-c','features.multi_agent=true','features','list'],{env,encoding:'utf8',timeout:20000});
assert.equal(features.status,0,features.stderr);
assert.match(features.stdout,/^multi_agent\s+stable\s+true$/m,'the pinned native binary must still recognize V1 delegation');
const hash=value=>createHash('sha256').update(value).digest('hex');
const bytes=readFileSync(path.join(fixture,'example.py'));
const sourceHash=hash(`${bytes.length}\texample.py\t${hash(bytes)}\n`);
const job={schemaVersion:3,execution:{taskId:'1',attempt:1},source:{path:fixture,snapshotId:'1',contentHash:sourceHash},output:{path:output},model:{providerId:'openai',connectionType:'openai',model:'fixture-native-mock',wireApi:'responses',credentialEnv:'OPENAI_API_KEY',configurationRevision:'fixtures-sdk050-native'},policy:{mode:'standard',timeBudget:{scanSeconds:120,finalizationGraceSeconds:0},includePaths:[],focus:[],validation:{mode:'default'}},resultSchemaVersion:1,progressSchemaVersion:2,eventSchemaVersion:2};
mkdirSync(path.join(output,'runtime'),{recursive:true,mode:0o700});const jobFile=path.join(output,'runtime/job.json');writeFileSync(jobFile,JSON.stringify(job),{mode:0o600});
const scan=spawnSync(process.execPath,['--import','tsx','src/main.ts','--mock','--job',jobFile],{cwd:scanner,env,encoding:'utf8',timeout:120000,maxBuffer:8<<20});
if(scan.status!==0){console.error(JSON.stringify({status:scan.status,signal:scan.signal,error:scan.error?.message,stdout:scan.stdout?.slice(-6000),stderr:scan.stderr?.slice(-6000)}));throw new Error('Fresh SDK mock scan failed')}
const {decode}=await import(pathToFileURL(path.join(root,'contracts/ts/dist/index.js')));
const result=decode('ScanResult',readFileSync(path.join(output,'normalized/result.json')),job.execution);assert.equal(result.status,'completed');assert.equal(result.engine.version,'0.5.0');assert.equal(result.extensions.sdkNative.sdkVersion,'0.5.0');assert.equal(result.extensions.adapter.version,2);
for(const [key,value] of Object.entries(result.extensions.sdk.coverage))assert.deepEqual(result.coverage.details[key],value);
const commit=decode('ResultCommit',readFileSync(path.join(output,'normalized/result.commit.json')),job.execution);assert.equal(commit.sha256,hash(readFileSync(path.join(output,'normalized/result.json'))));
// Mock scans deliberately bypass plugin installation. Exercise the real installer separately.
const codexHome=path.join(scratch,'plugin-bootstrap-home');
mkdirSync(codexHome,{recursive:true,mode:0o700});
const installed=await bootstrapPlugin(codexHome,plugin,{codexCommand:{command},environment:env,signal:AbortSignal.timeout(60000)});
assert.ok(installed.installedRoot.startsWith(codexHome+path.sep));
for(const rel of ['references/core-scan.md','references/threat-model.md','skills/fix-finding/SKILL.md','skills/security-scan/references/scan-artifacts-and-ledger.md']) {
 assert.equal(readFileSync(path.join(installed.installedRoot,rel),'utf8'),readFileSync(path.join(plugin,rel),'utf8'));
}
// 0.5.0 intentionally projects SDK launcher shims during local installation.
// Permit exactly that upstream transform; all server settings/credential names
// and every patched instruction must still match the source baseline.
const expectedMcp=JSON.parse(readFileSync(path.join(plugin,'.mcp.json'),'utf8'));
expectedMcp.mcpServers['codex-security'].command='./scripts/launch_codex_security_mcp_sdk';
assert.deepEqual(JSON.parse(readFileSync(path.join(installed.installedRoot,'.mcp.json'),'utf8')),expectedMcp);
assert.equal(readFileSync(path.join(installed.installedRoot,'scripts/launch_codex_security_mcp_sdk'),'utf8'),
 '#!/bin/sh\nif [ -n "${CODEX_SECURITY_PLUGIN_ROOT:-}" ]; then\n  exec "$CODEX_SECURITY_PLUGIN_ROOT/scripts/launch_codex_security_mcp" "$@"\nfi\nexec "$(dirname "$0")/launch_codex_security_mcp" "$@"\n');
console.log('PASS: upstream V2 config, four original plugin instructions, default deep workers=4, effective Codex '+version.stdout.trim()+', fresh upstream plugin bootstrap and native SDK 0.5.0 mock scan; real model calls=0');
