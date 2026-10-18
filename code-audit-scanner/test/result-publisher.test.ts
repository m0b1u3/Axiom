import assert from 'node:assert/strict';
import {test} from 'node:test';
import {mkdtemp,mkdir,readFile,writeFile,rm} from 'node:fs/promises';
import {createHash} from 'node:crypto';
import path from 'node:path';
import os from 'node:os';
import {COMPATIBILITY,decode,type ScannerJobV2} from '@axiom/contracts';
import {scanTaskFromJob} from '../src/contract/scanner-job.js';
import {convertCodexResult} from '../src/converter/codex-finding-converter.js';
import {contractResult,publishResult,InvalidSDKResultError} from '../src/artifact/result-publisher.js';
import type {ScanResult} from '../src/contract/scan-result.js';

test('SDK rich result publishes a verified portable result and keeps native fields',async()=>{
 const dir=await mkdtemp(path.join(os.tmpdir(),'axiom-result-publish-'));
 try{
  const input=decode('ScannerJobV2',await readFile(new URL('../../contracts/fixtures/v1/job-v2-result.json',import.meta.url))) as ScannerJobV2;
  input.output.path=dir;const task=scanTaskFromJob(input);
  const native={fixtureOnly:true,findings:{future:[0,false,null,'中文']},turn:{model:'fixture-actual',usage:{cached_input_tokens:0,reasoning_output_tokens:5}},cost:{estimatedUsd:1.125,cacheWriteInputTokens:3}};
  const sdk={manifest:{scan:{status:'completed'}},pluginVersion:'fixture-plugin',coverage:{completeness:'complete',futureField:{retained:true}},cost:{estimatedUsd:1.125,inputTokens:0,outputTokens:17},findings:{findings:[{
   title:'Synthetic finding',summary:'Fixture only',severity:{level:'high',score:7.5},confidence:{level:'medium',rationale:'Fixture rationale'},findingId:'fixture-finding',occurrenceId:'fixture-occurrence',locations:[{path:'src/a,b.ts',startLine:1}],
   codeEvidence:[{id:'fixture-evidence',path:'src/a,b.ts',startLine:1,code:'// fixture',explanation:'Fixture explanation'}],validation:{observed:false},provenance:{fixtureOnly:true},extensions:{future:[0,null,false,'']},
  }]},toJSON:()=>native};
  const converted=convertCodexResult(task,sdk,{startedAt:'2026-10-13T00:00:00Z',finishedAt:'2026-10-13T00:01:00Z',rawArtifactDir:'raw/codex-security',normalizedResultPath:'normalized/result.json'});
  await mkdir(path.join(dir,'raw/codex-security'),{recursive:true});await writeFile(path.join(dir,'raw/codex-security/report.md'),'fixture report\n');
  const published=await publishResult(task,converted);
  const raw=await readFile(path.join(dir,'normalized/result.json'));
  const result=decode('ScanResult',raw,input.execution),commit=decode('ResultCommit',await readFile(path.join(dir,'normalized/result.commit.json')),input.execution);
  assert.deepEqual(result,published);assert.equal(commit.sizeBytes,raw.length);assert.equal(commit.sha256,createHash('sha256').update(raw).digest('hex'));
  assert.deepEqual(result.extensions?.sdk,native);assert.equal(result.model.configurationRevision,input.model.configurationRevision);
  assert.equal(result.cost?.inputTokens,0);assert.equal(result.cost?.estimatedUsd,1.125);
  assert.equal(result.findings[0]?.confidence?.level,'medium');assert.equal(result.findings[0]?.confidence?.value,undefined,'numeric confidence was invented');
  assert.equal(result.findings[0]?.confidence?.rationale,'Fixture rationale');assert.equal(result.findings[0]?.codeEvidence?.[0]?.explanation,'Fixture explanation');
  assert.equal(result.findings[0]?.engineOccurrenceId,'fixture-occurrence');assert.deepEqual(result.findings[0]?.extensions?.future,[0,null,false,'']);
  const evidence=decode('FindingEvidence',JSON.stringify(result.findings[0]?.extensions?.axiomEvidence),input.execution);assert.equal(evidence.findingFingerprint,result.findings[0]?.fingerprint);assert.equal(evidence.nodes[1]?.sdkEvidenceId,'fixture-evidence');
  assert.equal(result.artifacts.length,1);assert.equal(result.artifacts[0]?.kind,'report');
 }finally{await rm(dir,{recursive:true,force:true})}
});

test('optional evidence cannot crowd out a source result near the document limit',async()=>{
 const input=decode('ScannerJobV2',await readFile(new URL('../../contracts/fixtures/v1/job-v2-result.json',import.meta.url)));
 const task=scanTaskFromJob(input),source=JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/evidence-source.json',import.meta.url),'utf8'));
 const legacy:ScanResult={taskId:task.taskId,status:'completed',startedAt:'2026-10-13T00:00:00Z',finishedAt:'2026-10-13T00:01:00Z',engine:{name:'codex-security',version:'fixture'},model:{provider:task.provider,model:task.model},findings:[source],rawArtifactDir:'raw/codex-security',normalizedResultPath:'normalized/result.json',sdkRaw:{fixtureOnly:true,padding:''}};
 const enriched=contractResult(task,legacy),without=structuredClone(enriched);delete without.findings[0]!.extensions!.axiomEvidence;
 const baseline=Buffer.byteLength(JSON.stringify(without));legacy.sdkRaw!.padding='x'.repeat(COMPATIBILITY.limits.documentBytes-65536-baseline-1000);
 const result=contractResult(task,legacy);assert.equal(result.findings[0]!.extensions!.axiomEvidence,undefined);assert.match(String(result.findings[0]!.extensions!.axiomEvidenceUnavailable),/容量/);assert.deepEqual(result.findings[0]!.raw,source.raw);assert.equal((result.extensions!.sdk as any).padding.length,(legacy.sdkRaw!.padding as string).length);
});

test('unknown result cost remains null; zero is an observed value; unknown severity is rejected',async()=>{
 const input=decode('ScannerJobV2',await readFile(new URL('../../contracts/fixtures/v1/job-v2-result.json',import.meta.url)));
 const task=scanTaskFromJob(input);
 const legacy:ScanResult={taskId:task.taskId,status:'completed',startedAt:'2026-10-13T00:00:00Z',finishedAt:'2026-10-13T00:01:00Z',engine:{name:'codex-security',version:'fixture'},model:{provider:task.provider,model:task.model},findings:[],rawArtifactDir:'raw/codex-security',normalizedResultPath:'normalized/result.json'};
 assert.equal(contractResult(task,legacy).cost,null);
 assert.equal(contractResult(task,{...legacy,cost:{estimatedUsd:0,inputTokens:0}}).cost?.estimatedUsd,0);
 assert.throws(()=>convertCodexResult(task,{findings:{findings:[{title:'fixture',summary:'fixture',severity:{level:'urgent'}}]}},{startedAt:legacy.startedAt,finishedAt:legacy.finishedAt,rawArtifactDir:legacy.rawArtifactDir,normalizedResultPath:legacy.normalizedResultPath}),InvalidSDKResultError);
});
