import assert from 'node:assert/strict';
import {test} from 'node:test';
import {readFile,mkdtemp,writeFile,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';

import {runComponentScans} from '@openai/codex-security';
import {validate} from '@axiom/contracts';
import {validateScanTask} from '../src/contract/validate-task.js';
import {deepScanOptions} from '../src/contract/deep-policy.js';
import type {ScanTask} from '../src/contract/scan-task.js';

const base:ScanTask={taskId:42,sourcePath:'/fixture-source',outputPath:'/fixture-output',provider:'openai',model:'fixture-only',scanMode:'deep',timeoutMinutes:47};

test('pinned SDK pure validator and all adapter fields agree at native integer boundaries',async()=>{
 const sdkRoot=new URL('../node_modules/@openai/codex-security/',import.meta.url);
 assert.equal(JSON.parse(await readFile(new URL('package.json',sdkRoot),'utf8')).version,'0.5.0');
 // Import the pinned pure validator only. It reads no credentials and makes no model calls.
 const {deepScanOptions:check}=await import(new URL('dist/deep-config.js',sdkRoot).href);
 for(const [sdk,field] of [['workers','workers'],['maxDiscoveryRuns','deepMaxDiscoveryRuns'],['stopAfterNoNew','deepStopAfterNoNew']] as const){
  for(const value of [1,65,1001,Number.MAX_SAFE_INTEGER]){
   assert.equal(check({mode:'deep',[sdk]:value})[sdk],value);
   const task={...base,[field]:value};validateScanTask(task);deepScanOptions(task);
  }
  for(const value of [0,-1,1.5,NaN,Infinity,Number.MAX_SAFE_INTEGER+1]){
   assert.throws(()=>check({mode:'deep',[sdk]:value}));
   assert.throws(()=>field==='workers'?validateScanTask({...base,workers:value}):deepScanOptions({...base,[field]:value}));
  }
 }
 for(const value of [0,65,Number.MAX_SAFE_INTEGER]){
  assert.equal(check({mode:'deep',subagents:value}).subagents,value);
  validate('ScanInput',{schemaVersion:1,knowledge:[],subagents:value});
 }
 for(const value of [-1,1.5,Infinity,Number.MAX_SAFE_INTEGER+1]){
  assert.throws(()=>check({mode:'deep',subagents:value}));
  assert.throws(()=>validate('ScanInput',{schemaVersion:1,knowledge:[],subagents:value}));
 }
 assert.equal(check({mode:'deep',maxTimeHours:96}).maxTimeHours,96);
 assert.throws(()=>check({mode:'deep',maxTimeHours:96.01}));
 assert.throws(()=>check({mode:'standard',workers:65}));
 assert.equal(deepScanOptions({...base,deepMaxDiscoveryRuns:1001,deepStopAfterNoNew:1002}).maxTimeHours,47/60);
});

test('native component scheduler runs nine components concurrently and naturally bounds by selected count',async t=>{
 const scratch=await mkdtemp(path.join(tmpdir(),'axiom-native-concurrency-'));t.after(()=>rm(scratch,{recursive:true,force:true}));
 const {mkdir}=await import('node:fs/promises');const repository=path.join(scratch,'source');await mkdir(repository);
 const components=Array.from({length:9},(_,i)=>({name:'Fixtures / Mock '+i,paths:[`file-${i}.ts`]}));
 for(const c of components)await writeFile(path.join(repository,c.paths[0]!),'// Fixtures / Mock; no model.\n');
 for(const workers of [9,Number.MAX_SAFE_INTEGER]){
  let active=0,peak=0,started=0,release!:()=>void;
  const barrier=new Promise<void>(r=>{release=r});
  const timer=setTimeout(()=>release(),5000);t.after(()=>clearTimeout(timer));
  const result=await runComponentScans({repository,components,workers,outputDir:path.join(scratch,'output-'+workers),createSecurity:()=>({
   close:async()=>{},run:async()=>{
    active++;peak=Math.max(peak,active);if(++started===9)release();await barrier;active--;
    return {manifest:{scan:{id:'fixture-only-'+started,status:'completed'}},findings:{findings:[]},coverage:{completeness:'complete'},cost:null} as never;
   },
  })});
  clearTimeout(timer);assert.equal(peak,9);assert.equal(active,0);assert.equal(result.completed,9);
 }
});
