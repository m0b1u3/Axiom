import {test} from 'node:test';import assert from 'node:assert/strict';
import {mkdtemp,mkdir,writeFile,readFile,rm} from 'node:fs/promises';import {tmpdir} from 'node:os';import path from 'node:path';import {createHash} from 'node:crypto';
import {decode,validate,type ArtifactRef} from '@axiom/contracts';
import {scanTaskFromJob} from '../src/contract/scanner-job.js';
import {buildSDKNativeResult,sdkUsageEstimate} from '../src/artifact/sdk-native-result.js';
import {SDKDiagnosticTracker} from '../src/runtime/sdk-diagnostics.js';
import type {ScanResult} from '../src/contract/scan-result.js';
const execution={taskId:'42',attempt:2};
test('public SDK estimate uses observed usage, preserves zeros and refuses unsupported pricing or invalid usage',()=>{
 const estimate=(model:unknown,usage:unknown)=>sdkUsageEstimate(execution,{turn:{model,usage}});
 assert.equal(estimate('gpt-5.6',{input_tokens:0,output_tokens:0}).estimatedUsd,0);
 const known=estimate('gpt-5.6',{input_tokens:100,cached_input_tokens:20,cache_write_input_tokens:10,output_tokens:5,reasoning_output_tokens:1});
 assert.equal(known.estimatedUsd,0.0005725);assert.equal(known.scope,'returned_turn_usage');assert.equal(known.cacheWriteInputTokens,10);
 assert.equal(estimate(undefined,{input_tokens:1,output_tokens:1}).status,'missing_model');
 assert.equal(estimate('gpt-5.6',null).status,'missing_usage');
 for(const [model,usage] of [['fixture-unknown',{input_tokens:1,output_tokens:1}],['gpt-5.6',{input_tokens:1,cached_input_tokens:2,output_tokens:1}]]){
  const v=estimate(model,usage);assert.equal(v.estimatedUsd,null);assert.equal(v.status,'unsupported_model_or_usage');
 }
});
test('diagnostics keep bounded snapshot, complete ordered JSONL and redact credentials without changing terminal status',async t=>{
 const root=await mkdtemp(path.join(tmpdir(),'axiom-native-diagnostics-'));t.after(()=>rm(root,{recursive:true,force:true}));
 const tracker=new SDKDiagnosticTracker(execution,root);
 for(let i=0;i<40;i++)tracker.warning('合成警告 '+i,{kind:'target_changed'});
 const entry=tracker.observer('onProgress',Object.assign(new Error('fixture Bearer synthetic-not-a-real-token'),{code:'fixture_observer'}));
 await tracker.drain();const state=tracker.snapshot();validate('SDKDiagnostics',state,execution);
 assert.equal(state.warningCount,40);assert.equal(state.observerErrorCount,1);assert.equal(state.omittedCount,9);assert.equal(state.entries[0]!.sequence,10);
 assert.equal(entry.source,'observer_error');assert.equal(entry.error!.code,'fixture_observer');assert.doesNotMatch(entry.message,/synthetic-not-a-real-token/);
 const raw=await readFile(path.join(root,'logs/sdk-diagnostics.jsonl'),'utf8');const rows=raw.trimEnd().split('\n');assert.equal(rows.length,41);assert.doesNotMatch(raw,/synthetic-not-a-real-token/);for(const [i,line] of rows.entries())assert.equal(decode('SDKDiagnostic',line,execution).sequence,i+1);
 const huge=tracker.warning('中'.repeat(10000));assert.equal(huge.truncated,true);assert.equal(huge.message.length,2048);await tracker.drain();
});
async function nativeFixture(t:{after:(f:()=>Promise<void>)=>void}){
 const root=await mkdtemp(path.join(tmpdir(),'axiom-native-result-'));t.after(()=>rm(root,{recursive:true,force:true}));
 const task=scanTaskFromJob(decode('ScannerJobV2',await readFile(new URL('../../contracts/fixtures/v1/job-v2-result.json',import.meta.url),'utf8')));task.outputPath=root;
 const result:ScanResult={taskId:task.taskId,status:'completed',startedAt:'2026-10-15T00:00:00Z',finishedAt:'2026-10-15T00:01:00Z',engine:{name:'codex-security',version:'fixture'},model:{provider:'openai',model:'fixture'},findings:[],rawArtifactDir:'raw/codex-security',normalizedResultPath:'normalized/result.json',sdkRaw:{manifest:{scan:{id:'native-fixture',target:{targetId:'fixture-target'}}},repositoryFindings:[],turn:{model:'gpt-5.6',usage:{input_tokens:0,output_tokens:0}}}};
 const refs:ArtifactRef[]=[];
 async function artifact(name:string,content:string){const relativePath='raw/codex-security/'+name;await mkdir(path.dirname(path.join(root,relativePath)),{recursive:true});await writeFile(path.join(root,relativePath),content);refs.push({kind:name.endsWith('.sarif')?'sarif':'report',relativePath,sizeBytes:Buffer.byteLength(content),sha256:createHash('sha256').update(content).digest('hex')});}
 return {task,result,refs,artifact};
}
test('native Markdown and SARIF are exact archived bytes; malformed and escaping exports do not discard core results',async t=>{
 const f=await nativeFixture(t);await f.artifact('report.md','# 合成报告\n<script>fixture-only</script>');await f.artifact('exports/results.sarif',JSON.stringify({version:'2.1.0',runs:[]}));
 let native=await buildSDKNativeResult(f.task,f.result,f.refs);assert.equal(native.reports.markdown.status,'available');assert.equal(native.reports.sarif.status,'available');assert.equal(native.targetId,'fixture-target');assert.equal(native.usageEstimate.estimatedUsd,0);assert.equal(native.repositoryHistory.state,'reported');assert.deepEqual(native.repositoryHistory.records,[]);
 await writeFile(path.join(f.task.outputPath,'raw/codex-security/exports/results.sarif'),'{');native=await buildSDKNativeResult(f.task,f.result,f.refs);assert.equal(native.reports.sarif.status,'invalid');assert.equal(native.reports.markdown.status,'available');
 f.result.sdkRaw!.reportPath='/fixture/outside/report.md';native=await buildSDKNativeResult(f.task,f.result,f.refs);assert.equal(native.reports.markdown.status,'invalid');assert.equal(native.reports.markdown.artifact,null);
});
test('repository history preserves native identities and closed/unconfirmed semantics; malformed history stays raw',async t=>{
 const f=await nativeFixture(t);const row={findingId:'f1',occurrenceId:'o1',scanId:'old-native',targetId:'target1',title:'fixture',summary:'fixture only',severity:{level:'high',future:false},status:'closed',confirmedInLatestScan:false,knownSince:'2026-10-15T00:00:00Z',knownScanIds:['old-native','new-native'],matchedFindingIds:[]};
 f.result.sdkRaw!.repositoryFindings=[row];let native=await buildSDKNativeResult(f.task,f.result,f.refs);assert.deepEqual(native.repositoryHistory.records[0],row);
 f.result.sdkRaw!.repositoryFindings=[{...row,status:'resolved'}];native=await buildSDKNativeResult(f.task,f.result,f.refs);assert.equal(native.repositoryHistory.state,'invalid');assert.equal((f.result.sdkRaw!.repositoryFindings as any[])[0].status,'resolved');
 delete f.result.sdkRaw!.repositoryFindings;native=await buildSDKNativeResult(f.task,f.result,f.refs);assert.equal(native.repositoryHistory.state,'not_reported');
});

test('large optional history projection never discards a valid full native JSON result',async t=>{
 const {publishResult}=await import('../src/artifact/result-publisher.js');const f=await nativeFixture(t);
 f.result.sdkRaw!.repositoryFindings=Array.from({length:100},(_,i)=>({findingId:'f'+i,occurrenceId:'o'+i,scanId:'s',targetId:'t',title:'synthetic',summary:'x'.repeat(180000),severity:{level:'low'},status:'open',confirmedInLatestScan:false}));
 const result=await publishResult(f.task,f.result);const native=decode('SDKNativeResult',JSON.stringify(result.extensions!.sdkNative),result.execution);
 assert.equal(native.repositoryHistory.state,'invalid');assert.match(native.repositoryHistory.reason!,/完整原生历史/);
 assert.equal((result.extensions!.sdk as any).repositoryFindings.length,100);assert.equal((result.extensions!.sdk as any).repositoryFindings[99].summary.length,180000);
});
