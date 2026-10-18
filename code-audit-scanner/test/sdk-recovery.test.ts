import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {CodexSecurity} from '@openai/codex-security';

test('pinned SDK recovery source audit has no model calls',async()=>{
 const {verifyRecoveryBaseline}=await import('../../deploy/sdk/recovery-check.mjs');
 const v=await verifyRecoveryBaseline();assert.equal(v.sdkVersion,'0.1.25');assert.equal(v.checkpointResume,false);assert.equal(v.sourceFiles,6);assert.equal(v.modelCalls,0);
});

test('SDK 0.1.25 exposes neither public nor internal interrupted-scan resume options',async()=>{
 assert.equal('resume' in CodexSecurity.prototype,false);
 const api=await readFile(new URL('../node_modules/@openai/codex-security/dist/api.d.ts',import.meta.url),'utf8');
 const options=api.slice(api.indexOf('export interface ScanOptions'),api.indexOf('export interface ValidationOptions'));
 assert.doesNotMatch(options,/resume|checkpoint/i);assert.match(options,/workflowId\?: string/);assert.match(options,/parentScanId\?: string/);
});

test('workflow and plugin coordinator recovery are not scan-session continuation',async()=>{
 const source=await readFile(new URL('../node_modules/@openai/codex-security/dist/api.js',import.meta.url),'utf8');
 const workflow=source.slice(source.indexOf('async #runWorkflow'),source.indexOf('async validate('));
 assert.match(workflow,/if \(completed\)/);assert.match(workflow,/await this\.#run\(repository, options\)/);
 assert.match(source,/const thread = codex\.startThread\(/);assert.doesNotMatch(source,/resumeThread|__resumeDeepScan/);
});
