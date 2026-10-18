import {test,type TestContext} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,mkdir,writeFile,readFile,rm,cp} from 'node:fs/promises';
import {execFileSync} from 'node:child_process';
import {tmpdir} from 'node:os';
import {createHash} from 'node:crypto';
import path from 'node:path';
import {decode} from '@axiom/contracts';
import {scanTaskFromJob} from '../src/contract/scanner-job.js';
import {sdkScanTarget,gitDiffArgs,gitEvidenceBinding,assertGitSDKResult} from '../src/runtime/git-target.js';
import {runSDKOperation} from '../src/engine/sdk-operation.js';
import {contractResult} from '../src/artifact/result-publisher.js';
import type {ScanResult} from '../src/contract/scan-result.js';
const hash=(v:Buffer|string)=>createHash('sha256').update(v).digest('hex');
async function fixture(t:TestContext){
 const previous=Object.fromEntries(Object.entries(process.env).filter(([key])=>key.startsWith('GIT_')));t.after(()=>{for(const key of Object.keys(process.env))if(key.startsWith('GIT_'))delete process.env[key];Object.assign(process.env,previous);});
 const root=await mkdtemp(path.join(tmpdir(),'axiom-git-target-'));t.after(()=>rm(root,{recursive:true,force:true}));const source=path.join(root,'source');await mkdir(source);
 const git=(args:string[])=>execFileSync('git',['-C',source,...args],{env:{...process.env,GIT_CONFIG_GLOBAL:process.platform==='win32'?'NUL':'/dev/null',GIT_CONFIG_NOSYSTEM:'1',GIT_AUTHOR_NAME:'Fixture',GIT_AUTHOR_EMAIL:'fixture@example.invalid',GIT_COMMITTER_NAME:'Fixture',GIT_COMMITTER_EMAIL:'fixture@example.invalid'}});
 git(['init','-q','--template=','--initial-branch=main','--object-format=sha1']);await writeFile(path.join(source,'fixture.ts'),'// base\n');git(['add','.']);git(['commit','-qm','base']);const base=git(['rev-parse','HEAD']).toString().trim();
 const bytes=Buffer.from('// Fixtures / Mock\nconst query = request.query.q;\n');await writeFile(path.join(source,'fixture.ts'),bytes);git(['add','.']);git(['commit','-qm','head']);const head=git(['rev-parse','HEAD']).toString().trim();
 const job=decode('ScannerJobV6',await readFile(new URL('../../contracts/fixtures/v1/job-git-diff.json',import.meta.url)));job.source.path=source;job.output.path=path.join(root,'output');job.execution.taskId='42';job.gitTarget={...job.gitTarget,baseCommit:base,headCommit:head,baseTree:git(['rev-parse',base+'^{tree}']).toString().trim(),headTree:git(['rev-parse',head+'^{tree}']).toString().trim(),diffSha256:hash(git(gitDiffArgs(base,head))),headContentHash:hash(`${bytes.length}\tfixture.ts\t${hash(bytes)}\n`)};
 const task=scanTaskFromJob(job);return {root,source,task,git,job};
}
test('Git job maps to pinned SDK refs without path-array or mutable ref fallback',async t=>{
 const {task,job}=await fixture(t),target=await sdkScanTarget(task);assert.equal(target?.kind,'refs');assert.equal((target as {base:string}).base,job.gitTarget.baseCommit);assert.equal((target as {head:string}).head,job.gitTarget.headCommit);
 const binding=gitEvidenceBinding(task)!;decode('GitDiffAssignment',JSON.stringify(binding),job.execution);assert.equal(binding.sourceSnapshotId,job.source.snapshotId);assert.equal(binding.evidenceRevision,'head');
 assertGitSDKResult(task,{manifest:{scan:{target:{kind:'git_diff',baseRevision:job.gitTarget.baseCommit,headRevision:job.gitTarget.headCommit}}}});
 assert.throws(()=>assertGitSDKResult(task,{manifest:{scan:{target:{kind:'git_diff',baseRevision:job.gitTarget.headCommit,headRevision:job.gitTarget.baseCommit}}}}),/frozen/);
 for(const changed of [{...task,scanMode:'deep' as const},{...task,includePaths:['fixture.ts']}])await assert.rejects(sdkScanTarget(changed),/Standard/);
});
test('changed HEAD, object identity, diff digest and worktree are rejected before the SDK',async t=>{
 for(const mutation of ['head-tree','diff','worktree','head'])await t.test(mutation,async t=>{
  const {task,source,git}=await fixture(t);
  if(mutation==='head-tree')task.gitTarget!.headTree='f'.repeat(40);
  if(mutation==='diff')task.gitTarget!.diffSha256='f'.repeat(64);
  if(mutation==='worktree')await writeFile(path.join(source,'fixture.ts'),'dirty worktree');
  if(mutation==='head')git(['update-ref','HEAD',task.gitTarget!.baseCommit]);
  await assert.rejects(sdkScanTarget(task),/differs/);
 });
});
test('result and every finding bind execution, frozen source and HEAD; foreign SDK target fails',async t=>{
 const {task}=await fixture(t);const native={manifest:{scan:{target:{kind:'git_diff',baseRevision:task.gitTarget!.baseCommit,headRevision:task.gitTarget!.headCommit}}}};
 const result:ScanResult={taskId:task.taskId,status:'completed',startedAt:'2026-10-16T00:00:00Z',finishedAt:'2026-10-16T00:01:00Z',engine:{name:'codex-security',version:'fixture'},model:{provider:task.provider,model:task.model},findings:[{fingerprint:'fixture-git',title:'Fixture only',severity:'high',description:'Synthetic only'}],rawArtifactDir:'raw/codex-security',normalizedResultPath:'normalized/result.json',sdkRaw:native};
 const full=contractResult(task,result),binding=gitEvidenceBinding(task);assert.deepEqual(full.extensions?.gitDiff,binding);assert.deepEqual(full.findings[0]?.extensions?.axiomGitTarget,binding);
 const bad=structuredClone(result);(bad.sdkRaw as any).manifest.scan.target.headRevision=task.gitTarget!.baseCommit;assert.throws(()=>contractResult(task,bad),/frozen base\/head/);
 bad.status='failed';assert.throws(()=>contractResult(task,bad),/frozen base\/head/,'failed scans must not publish findings from an unbound target');
});
test('pinned SDK 0.1.25 preflight accepts committed refs locally without a model call',async t=>{
 const {task,root}=await fixture(t);task.mock=true;task.sdkOperation={schemaVersion:1,execution:{taskId:String(task.taskId),attempt:task.executionAttempt!},kind:'preflight',origin:null,findingId:null,finding:null,acknowledgeUnknownCost:false};
 const privateSource=path.join(task.outputPath,'runtime/git-source-fixture');await cp(task.sourcePath,privateSource,{recursive:true});task.sourcePath=privateSource;
 process.env.GIT_DIR='/fixture/ambient-other-repository';process.env.GIT_CONFIG_COUNT='1';process.env.GIT_CONFIG_KEY_0='core.hooksPath';process.env.GIT_CONFIG_VALUE_0='/fixture/ambient-hooks';
 const previous=process.env.CODEX_SECURITY_STATE_DIR;process.env.CODEX_SECURITY_STATE_DIR=path.join(root,'sdk-state');t.after(()=>{if(previous===undefined)delete process.env.CODEX_SECURITY_STATE_DIR;else process.env.CODEX_SECURITY_STATE_DIR=previous;});
 const result=await runSDKOperation(task,new AbortController().signal);assert.equal(result.status,'completed',result.error?.message);assert.equal(result.cost?.estimatedUsd,0);assert.equal((result.sdkOperation!.preflight!.authentication as {verified:boolean}).verified,false);assert.match(JSON.stringify(result.sdkOperation!.preflight),/git_diff|refs/);
 assert.equal(process.env.GIT_DIR,undefined);assert.equal(process.env.GIT_CONFIG_COUNT,undefined);
});
