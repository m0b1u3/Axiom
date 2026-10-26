import {test} from 'node:test';
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
const helper=fileURLToPath(new URL('../runtime/provider-auth.mjs',import.meta.url));
test('native provider helper returns only the selected bound key and rejects missing, invalid or arbitrary environment names',()=>{
 const env={PATH:process.env.PATH,CUSTOM_API_KEY:'fixture-custom-key',OPENAI_API_KEY:'fixture-unrelated-key',CODEX_API_KEY:'fixture-other-key'};
 const ok=spawnSync(process.execPath,[helper,'CUSTOM_API_KEY'],{env,encoding:'utf8'});
 assert.equal(ok.status,0);assert.equal(ok.stdout,'fixture-custom-key\n');assert.equal(ok.stderr,'');
 for(const name of ['MISSING_KEY','CUSTOM_API_KEY;env','CUSTOM_API_KEY\n']){
  const bad=spawnSync(process.execPath,[helper,name],{env,encoding:'utf8'});assert.equal(bad.status,1);assert.equal(bad.stdout,'');assert.ok(!bad.stderr.includes('fixture-'));
 }
 const bad=spawnSync(process.execPath,[helper,'CUSTOM_API_KEY'],{env:{...env,CUSTOM_API_KEY:'private\r\nvalue'},encoding:'utf8'});assert.equal(bad.status,1);assert.equal(bad.stdout,'');assert.ok(!bad.stderr.includes('private'));
});
