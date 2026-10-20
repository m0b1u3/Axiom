import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {createHash} from 'node:crypto';
const root=new URL('../../',import.meta.url),sdk=new URL('code-audit-scanner/node_modules/@openai/codex-security/',root);
const json=async p=>JSON.parse(await readFile(new URL(p,import.meta.url),'utf8'));
const sha=v=>createHash('sha256').update(v).digest('hex');
test('all seven 0.5 patches are version-bound, complete and preserve the legacy baseline',async()=>{
 const manifest=await json('manifest.json'),review=await json('patch-review-050.json'),old=await json('legacy/0.1.25/manifest.json');
 assert.equal(manifest.packages[0].version,'0.5.0');assert.equal(old.packages[0].version,'0.1.25');assert.equal(manifest.patches.length,7);assert.equal(old.patches.length,7);assert.equal(review.patches.length,7);
 for(const patch of manifest.patches){
  const entry=review.patches.find(e=>e.path===patch.path);assert.ok(entry);assert.equal(entry.upstreamSha256,patch.before);assert.equal(entry.patchedSha256,patch.after);
  assert.equal(sha(await readFile(new URL(patch.source,import.meta.url))),patch.after);
  assert.equal(entry.realProviderValidation,'not_run');
 }
 for(const patch of old.patches){
  assert.equal(sha(await readFile(new URL('legacy/0.1.25/'+patch.source,import.meta.url))),patch.after,`legacy replacement missing or changed: ${patch.path}`);
 }
});
test('delegation rebases do not restore removed old workflow helpers or silently change permissions',async()=>{
 const config=await readFile(new URL('dist/config.js',sdk),'utf8');assert.match(config,/multi_agent: true/);assert.doesNotMatch(config,/multi_agent_v2: Object\.freeze/);
 const ledger=await readFile(new URL('_bundled_plugin/skills/security-scan/references/scan-artifacts-and-ledger.md',sdk),'utf8');
 assert.match(ledger,/fork_context: false/);assert.doesNotMatch(ledger,/rank_candidates\.py|rank_worklist\.py/);
 const mcp=JSON.parse(await readFile(new URL('_bundled_plugin/.mcp.json',sdk),'utf8'));
 const vars=mcp.mcpServers['codex-security'].env_vars;
 for(const name of ['OPENAI_API_KEY','DEEPSEEK_API_KEY','DASHSCOPE_API_KEY','MOONSHOT_API_KEY','ZHIPU_API_KEY','CUSTOM_API_KEY'])assert.ok(vars.includes(name));
 assert.ok(!vars.includes('AXIOM_DATABASE_URL')&&!vars.includes('AXIOM_MASTER_KEY')&&!vars.includes('AXIOM_JWT_SECRET'));
 const custom=await readFile(new URL('dist/custom-validation-prompt.js',sdk),'utf8');
 const core=sha(await readFile(new URL('_bundled_plugin/references/core-scan.md',sdk)));
 assert.ok(custom.includes(core),'custom validation must guard the exact rebased workflow');
});
