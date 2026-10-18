import {test,type TestContext} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,mkdir,writeFile,readFile,rm,symlink} from 'node:fs/promises';
import {tmpdir} from 'node:os';import path from 'node:path';import {createHash} from 'node:crypto';
import {normalizeComponentPlan} from '@openai/codex-security';
import {decode} from '@axiom/contracts';
import {componentCoverage,frozenTreeHash} from '../src/runtime/component-plan.js';
import {runComponentPlanning} from '../src/engine/component-planning.js';
import {scanTaskFromJob} from '../src/contract/scanner-job.js';

async function fixture(t:TestContext){
 const root=await mkdtemp(path.join(tmpdir(),'axiom-components-'));t.after(()=>rm(root,{recursive:true,force:true}));const source=path.join(root,'source');
 const files=['src/api/a.ts','src/web/b.ts','src2/c.ts','.ignored','README.md','源码/证据.ts','\ue000.ts','😀.ts','.git/objects/fixture'];
 for(const name of files){const f=path.join(source,name);await mkdir(path.dirname(f),{recursive:true});await writeFile(f,'Fixture only\n');}await mkdir(path.join(source,'empty'));
 const job=decode('ScannerJobV7',await readFile(new URL('../../contracts/fixtures/v1/job-components.json',import.meta.url)));job.source.path=source;job.source.contentHash=await frozenTreeHash(source);job.componentPlanning.source.contentHash=job.source.contentHash;job.output.path=path.join(root,'output');job.execution.taskId='42';job.componentPlanning.execution=job.execution;
 return {root,source,job,task:scanTaskFromJob(job),files:files.filter(p=>!p.startsWith('.git/'))};
}
test('pinned public normalizeComponentPlan trims names and exact duplicates without a model',async t=>{
 const {task,source}=await fixture(t);task.componentPlanning!.document={components:[{name:'  Complete source  ',paths:['.','.']}]};
 const result=await runComponentPlanning(task,new AbortController().signal);assert.equal(result.status,'completed',result.error?.message);assert.deepEqual(result.componentPlanning!.document,{components:[{name:'Complete source',paths:['.']}]});assert.equal(result.cost?.estimatedUsd,0);assert.equal(result.componentPlanning!.coverage?.valid,true);
 const stored=decode('ScanResult',await readFile(path.join(task.outputPath,'normalized/result.json')),task.componentPlanning!.execution);assert.equal(stored.extensions?.sdkNative,undefined);assert.equal(stored.findings.length,0);assert.ok(stored.extensions?.componentPlanning);
 await assert.rejects(normalizeComponentPlan(source,{components:[{name:'Missing',paths:['missing']}]}));
});
test('coverage includes ignored/dotfiles and uses UTF-8 byte ordering shared with Go',async t=>{
 const {source,files}=await fixture(t),v=await componentCoverage(source,{components:[{name:'All',paths:['.']}]});files.sort((a,b)=>Buffer.compare(Buffer.from(a),Buffer.from(b)));const h=createHash('sha256');files.forEach(p=>h.update(p+'\0'));assert.equal(v.inventoryHash,h.digest('hex'));assert.equal(v.fileCount,8);assert.equal(v.valid,true);
 const partial=await componentCoverage(source,{components:[{name:'Source',paths:['src']}]});assert.equal(partial.coveredFileCount,2);assert.equal(partial.uncoveredPaths.length,6);assert.equal(partial.uncoveredPaths.includes('src2/c.ts'),true);
 const bad=await componentCoverage(source,{components:[{name:' Same ',paths:['src','src/api']},{name:'Same',paths:['src/api/a.ts']},{name:'Empty',paths:['empty']}]});assert.equal(bad.overlapCount,3);assert.deepEqual(bad.emptyComponents,[2]);assert.deepEqual(bad.duplicateNames,['Same']);assert.equal(bad.valid,false);
 const many=await componentCoverage(source,{components:Array.from({length:25},()=>({name:'same',paths:['.']}))});assert.equal(many.overlapCount,300);assert.equal(many.overlaps.length,200);
});
test('metadata, path traversal, symlinks and cancellation are rejected',async t=>{
 const {source}=await fixture(t);for(const p of ['.git','src/.git/objects','../secret','/etc/passwd','src\\api','missing','C:drive'])await assert.rejects(componentCoverage(source,{components:[{name:'Bad',paths:[p]}]}));
 const controller=new AbortController();controller.abort();await assert.rejects(componentCoverage(source,{components:[{name:'All',paths:['.']}]},controller.signal));
 await symlink(path.join(source,'README.md'),path.join(source,'link'));await assert.rejects(componentCoverage(source,{components:[{name:'All',paths:['.']}]}));
});
test('changed immutable source and mock suggestions fail without invoking a planning model',async t=>{
 for(const scenario of ['mutation','mock'])await t.test(scenario,async t=>{
  const {source,task}=await fixture(t);if(scenario==='mutation')await writeFile(path.join(source,'README.md'),'Changed');else{task.mock=true;task.componentPlanning!.kind='suggest';task.componentPlanning!.document=null;task.componentPlanning!.acknowledgeUnknownCost=true;}
  const result=await runComponentPlanning(task,new AbortController().signal);assert.equal(result.status,'failed');assert.equal(result.componentPlanning!.document,null);assert.equal(result.componentPlanning!.coverage,null);assert.match(result.error!.message,scenario==='mutation'?/源码已变化/:/没有调用模型/);assert.equal(result.componentPlanning!.cost.state,scenario==='mutation'?'known':'unknown');
 });
});
test('local normalization failure preserves a structured error without exposing host directories',async t=>{
 const {source,task}=await fixture(t);task.componentPlanning!.document={components:[{name:'Missing',paths:['missing.ts']}]};
 const result=await runComponentPlanning(task,new AbortController().signal);assert.equal(result.status,'failed');assert.ok(result.componentPlanning!.error);assert.equal(result.componentPlanning!.document,null);assert.equal(result.cost?.estimatedUsd,0);assert.equal(result.error!.message.includes(source),false);
 decode('ScanResult',await readFile(path.join(task.outputPath,'normalized/result.json')),task.componentPlanning!.execution);
});
