import assert from 'node:assert/strict';
import {readFileSync,mkdirSync,writeFileSync} from 'node:fs';
import path from 'node:path';
import {pathToFileURL,fileURLToPath} from 'node:url';
import {spawnSync} from 'node:child_process';
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'../..'),scanner=path.join(root,'code-audit-scanner');
const sdk=path.join(scanner,'node_modules/@openai/codex-security');
const {DEFAULT_CODEX_CONFIG}=await import(pathToFileURL(path.join(sdk,'dist/config.js')));
const {resolveCodexCommand,bootstrapPlugin}=await import(pathToFileURL(path.join(sdk,'dist/runtime.js')));
assert.equal(DEFAULT_CODEX_CONFIG.features.multi_agent,true);
assert.equal(DEFAULT_CODEX_CONFIG.features.multi_agent_v2,undefined);
const command=resolveCodexCommand({}).command;
assert.ok(command.includes('/codex/node_modules/@openai/codex-linux-x64/'),'effective binary must retain the audited nested dependency');
const version=spawnSync(command,['--version'],{encoding:'utf8'});assert.equal(version.status,0);assert.match(version.stdout,/0\.149\.1/);
const plugin=path.join(sdk,'_bundled_plugin');
const credentialNames=['DEEPSEEK_API_KEY','DASHSCOPE_API_KEY','MOONSHOT_API_KEY','ZHIPU_API_KEY','CUSTOM_API_KEY'];
const forwarded=JSON.parse(readFileSync(path.join(plugin,'.mcp.json'),'utf8')).mcpServers['codex-security'].env_vars;
for(const name of credentialNames)assert.ok(forwarded.includes(name),`MCP must forward ${name}`);
for(const rel of ['references/core-scan.md','references/threat-model.md','skills/fix-finding/SKILL.md','skills/security-scan/references/scan-artifacts-and-ledger.md']){
 const text=readFileSync(path.join(plugin,rel),'utf8');assert.match(text,/fork_context: false/);assert.doesNotMatch(text,/fork_turns/);
}
assert.match(readFileSync(path.join(plugin,'scripts/deep_scan_config.py'),'utf8'),/DEFAULT_WORKERS = 4/);
assert.doesNotMatch(readFileSync(path.join(scanner,'src/main.ts'),'utf8'),/args\.workers \?\? 2/);
const fixture=path.join(root,'runtime-smoke/source'),output=path.join(root,'runtime-smoke/output');mkdirSync(fixture,{recursive:true});writeFileSync(path.join(fixture,'example.py'),'def hello():\n    return "hello"\n');
const env={PATH:process.env.PATH,HOME:process.env.HOME,TMPDIR:process.env.TMPDIR,CODEX_SECURITY_STATE_DIR:path.join(root,'runtime-smoke/sdk-state')};
const scan=spawnSync(process.execPath,['--import','tsx','src/main.ts','--mock','--source',fixture,'--output',output,'--task-id','1'],{cwd:scanner,env,encoding:'utf8',timeout:120000,maxBuffer:8<<20});
if(scan.status!==0){console.error(scan.stderr);throw new Error('Fresh SDK mock scan failed')}
const result=JSON.parse(readFileSync(path.join(output,'normalized/result.json'),'utf8'));assert.equal(result.status,'completed');
// Mock scans deliberately bypass plugin installation. Exercise the real installer separately.
const codexHome=path.join(root,'runtime-smoke/plugin-bootstrap-home');
mkdirSync(codexHome,{recursive:true,mode:0o700});
const installed=await bootstrapPlugin(codexHome,plugin,{codexCommand:{command},environment:env,signal:AbortSignal.timeout(60000)});
assert.ok(installed.installedRoot.startsWith(codexHome+path.sep));
for(const rel of ['.mcp.json','references/core-scan.md','references/threat-model.md','skills/fix-finding/SKILL.md','skills/security-scan/references/scan-artifacts-and-ledger.md']) {
 assert.equal(readFileSync(path.join(installed.installedRoot,rel),'utf8'),readFileSync(path.join(plugin,rel),'utf8'));
}
console.log('PASS: V1 config, four plugin instructions, default deep workers=4, effective Codex '+version.stdout.trim()+', fresh plugin bootstrap with provider credential forwarding and SDK mock scan');
