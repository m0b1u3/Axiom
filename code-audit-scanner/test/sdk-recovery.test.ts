import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {CodexSecurity} from '@openai/codex-security';

test('pinned SDK recovery source audit has no model calls',async()=>{
 const {verifyRecoveryBaseline}=await import('../../deploy/sdk/recovery-check.mjs');
 const v=await verifyRecoveryBaseline();assert.equal(v.sdkVersion,'0.5.0');assert.equal(v.checkpointResume,false);assert.equal(v.sourceFiles,5);assert.equal(v.modelCalls,0);
});

test('0.5.0 internal CLI resume is absent from the public SDK contract',async()=>{
 assert.equal('resume' in CodexSecurity.prototype,false);
 const api=await readFile(new URL('../node_modules/@openai/codex-security/dist/api.d.ts',import.meta.url),'utf8');
 const settings=await readFile(new URL('../node_modules/@openai/codex-security/dist/scan-settings.d.ts',import.meta.url),'utf8');
 assert.doesNotMatch(api+settings,/^\s+resumeScanId\??:/m);assert.match(api,/workflowId\?: string/);assert.match(api,/parentScanId\?: string/);
 const baseline=JSON.parse(await readFile(new URL('../../deploy/sdk/legacy/0.1.25/recovery-baseline.json',import.meta.url),'utf8'));
 assert.equal(baseline.sdkVersion,'0.1.25');assert.equal(baseline.checkpointResume,false);
});

test('application recovery never forwards the internal CLI continuation option',async()=>{
 const source=await readFile(new URL('../node_modules/@openai/codex-security/dist/api.js',import.meta.url),'utf8');
 assert.match(source,/options\.resumeScanId/);
 const adapter=await readFile(new URL('../src/engine/codex-security-engine.ts',import.meta.url),'utf8');
 const input=await readFile(new URL('../src/contract/sdk-scan-input.ts',import.meta.url),'utf8');
 assert.doesNotMatch(adapter+input,/resumeScanId|resumeThread|__resumeDeepScan/);
});
