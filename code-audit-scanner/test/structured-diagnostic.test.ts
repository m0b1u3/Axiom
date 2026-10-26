import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {readFile} from 'node:fs/promises';
import {execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {structuredDiagnosticCatalog,runStructuredDiagnostic,STRUCTURED_DIAGNOSTIC_CATALOG_HASH} from '@axiom/contracts';
import {standaloneValidationContract,structuredOutputControls} from '../src/proxy/validation-output-contract.js';
const catalog=structuredDiagnosticCatalog(),sha=(s:string)=>createHash('sha256').update(s).digest('hex');
test('fixed templates retain the pinned native SDK prompt, format hash and 914-byte fixture',()=>{
 const root=fileURLToPath(new URL('../',import.meta.url));
 execFileSync(process.execPath,['tools/structured-output-catalog.mjs'],{cwd:root,stdio:'pipe'});
 assert.equal(catalog.sdkVersion,'0.5.0');assert.equal(catalog.fixture.totalBytes,914);assert.equal(catalog.requests.length,3);assert.equal(catalog.nativeFormatSha256,'146d5f8d876052c923acf7d97fdcfeb2cc5aee884e23736a42310a4ebe1b5d23');
 for(const f of catalog.fixture.files)assert.equal(sha(f.content),f.sha256);
});
test('all three requests share one prompt; only formatting changes, no model credentials or limits are loaded',()=>{
 assert.equal(new Set(catalog.requests.map(p=>p.promptSha256)).size,1);
 for(const p of catalog.requests){
  assert.equal(sha(p.bodyJson),p.bodySha256);const b=JSON.parse(p.bodyJson);
  assert.equal(b.model,'$MODEL');assert.equal(b.stream,true);assert.ok(!('tools' in b));
  assert.deepEqual(structuredOutputControls(b),{outputTokenLimitField:null,outputTokenLimit:null,verbosity:null,stopSpecified:false});
  assert.ok(!/203\.195\.180\.138|pydog|fixtures-only-never-log/i.test(p.bodyJson));
  if(p.profile==='native_schema'){const summary=standaloneValidationContract(p.protocol==='responses'?b.text.format:b.response_format)!;assert.equal(summary.reportDescriptionPresent,false);assert.equal(summary.reportRequirement,'nonempty_string_only');}
  if(p.profile==='described_schema'){const summary=standaloneValidationContract(p.protocol==='responses'?b.text.format:b.response_format)!;assert.equal(summary.reportDescriptionPresent,true);assert.equal(summary.reportRequirement,'nonempty_string_only');}
 }
});
test('offline comparisons do not infer provider support, reproduction or costs from a preset response',()=>{
 for(const protocol of ['responses'] as const)for(const scenario of catalog.scenarios){
  const r=runStructuredDiagnostic({schemaVersion:1,mode:'offline',catalogSha256:STRUCTURED_DIAGNOSTIC_CATALOG_HASH,protocol,scenario:scenario.id},'1'.repeat(32),'2026-10-22T08:00:00.000Z');
  assert.equal(r.results.length,3);assert.equal(r.upstreamContacted,false);assert.equal(r.credentialLoaded,false);assert.equal(r.providerCompatibility,'not_tested');assert.equal(r.inputTokens+r.outputTokens,0);
  if(scenario.id==='title_only')for(const v of r.results){assert.equal(v.schemaValid,true);assert.equal(v.bodyState,'title_only');assert.equal(v.errorCode,'report_body_incomplete');}
  if(scenario.id==='complete')for(const v of r.results){assert.equal(v.schemaValid,true);assert.equal(v.bodyState,'body_present');assert.equal(v.errorCode,null);}
  const serialized=JSON.stringify(r);assert.ok(!serialized.includes('responseJson'));assert.ok(!serialized.includes('bodyJson'));assert.ok(!serialized.includes('## SQL'));
 }
});
test('no diagnostic runtime enters the SDK operation or the scan/job transport',async()=>{
 const source=await readFile(new URL('../../contracts/ts/src/structured-output.ts',import.meta.url),'utf8');assert.ok(!/fetch\(|CodexSecurity|process\.env|node:fs/.test(source));
});
