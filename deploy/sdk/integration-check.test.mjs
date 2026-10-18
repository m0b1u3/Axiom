import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile,mkdtemp,mkdir,writeFile,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {checkSdkIntegration} from './integration-check.mjs';

const root=fileURLToPath(new URL('../../',import.meta.url));
const baseline=JSON.parse(await readFile(new URL('./integration.json',import.meta.url),'utf8'));
test('locked SDK public surface matches the capability inventory without importing a runtime',async()=>{
  const result=await checkSdkIntegration();
  assert.equal(result.version,'0.1.25');assert.equal(result.capabilityGroups,25);
  assert.equal(result.modelCalls,0);assert.equal(result.externalWrites,0);
  assert.ok(result.publicExports.includes('runComponentScans'));
  assert.ok(result.serverExports.includes('startFindingsServer'));
  assert.equal(result.counts.not_public,2);
});
test('SDK upgrades cannot silently keep the previous integration baseline',async()=>{
  const manifest=structuredClone(baseline);manifest.sdk.version='0.1.26';
  await assert.rejects(checkSdkIntegration({manifest}),/version mismatch/);
});
test('declaration drift is rejected before a nonexistent capability can be advertised',async t=>{
  const dir=await mkdtemp(path.join(tmpdir(),'axiom-sdk-api-fixture-'));t.after(()=>rm(dir,{recursive:true,force:true}));
  const installed=path.join(root,'code-audit-scanner/node_modules/@openai/codex-security');
  await writeFile(path.join(dir,'package.json'),await readFile(path.join(installed,'package.json')));
  for(const name of Object.keys(baseline.sdk.declarations)){await mkdir(path.dirname(path.join(dir,name)),{recursive:true});await writeFile(path.join(dir,name),await readFile(path.join(installed,name)));}
  await writeFile(path.join(dir,'dist/api.d.ts'),'export declare class CodexSecurity { fix(): Promise<void>; }');
  await assert.rejects(checkSdkIntegration({sdkDirectory:dir}),/SDK declaration changed/);
});
test('fix and resume cannot be promoted to integrated public SDK methods',async()=>{
  for(const id of ['unsupported.fix','unsupported.resume']){
    const manifest=structuredClone(baseline),capability=manifest.capabilities.find(c=>c.id===id);capability.status='integrated';
    await assert.rejects(checkSdkIntegration({manifest}),/public API mismatch/);
  }
});
test('integration claims require actual implementation evidence and unique identities',async()=>{
  const evidence=structuredClone(baseline);evidence.capabilities[0].evidence=[];
  await assert.rejects(checkSdkIntegration({manifest:evidence}),/Missing implementation evidence/);
  const duplicate=structuredClone(baseline);duplicate.capabilities.push(duplicate.capabilities[0]);
  await assert.rejects(checkSdkIntegration({manifest:duplicate}),/Duplicate SDK capability/);
});

test('every public runtime export has a workflow or an explicit internal classification',async()=>{
  const manifest=structuredClone(baseline);delete manifest.nonWorkflowExports.AuthenticationRequiredError;
  await assert.rejects(checkSdkIntegration({manifest}),/Unclassified SDK export/);
});
