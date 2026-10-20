import assert from 'node:assert/strict';
import {test} from 'node:test';
import {readFile, access, mkdtemp, rm} from 'node:fs/promises';
import os from 'node:os';
import {createHash} from 'node:crypto';
import {spawn} from 'node:child_process';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {decode, validate, normalizeLegacyErrorCode, exitCode, COMPATIBILITY, SCHEMA_HASH, CONTRACT_KINDS, EXECUTION_MATRIX, EXECUTION_MATRIX_HASH} from '../ts/dist/index.js';

const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..');
const fixtureRoot=path.join(root,'fixtures/v1');
const manifest=JSON.parse(await readFile(path.join(fixtureRoot,'manifest.json'),'utf8'));
const codec=process.env.AXIOM_CONTRACT_CODEC;
if (!codec) throw new Error('Run npm test: it builds the Go conformance codec');
const processGo=spawn(codec,[],{stdio:['pipe','pipe','pipe']});
processGo.stdin.setDefaultEncoding('utf8');
const replies=[];
let buffer='',stderr='',terminalError;
processGo.stderr.setEncoding('utf8');
processGo.stderr.on('data',data=>stderr+=data);
processGo.stdout.setEncoding('utf8');
processGo.stdout.on('data',data=>{
  buffer+=data;
  while (buffer.includes('\n')) {
    const index=buffer.indexOf('\n'),line=buffer.slice(0,index);buffer=buffer.slice(index+1);
    const reply=replies.shift();
    if (!reply) throw new Error('Unsolicited Go codec response');
    reply.resolve(JSON.parse(line));
  }
});
processGo.on('error',error=>{terminalError=error;for(const r of replies.splice(0))r.reject(error)});
processGo.on('exit',code=>{
  if (code!==0){terminalError=new Error('Go codec exited '+code+': '+stderr);for(const r of replies.splice(0))r.reject(terminalError)}
});
function go(kind,json,expectedExecution) {
  if (terminalError) return Promise.reject(terminalError);
  return new Promise((resolve,reject)=>{
    replies.push({resolve,reject});
    processGo.stdin.write(JSON.stringify({kind,json,expectedExecution})+'\n');
  });
}
function attempt(fn){try{return {valid:true,value:fn()}}catch(error){return {valid:false,error}}}

for (const fixture of manifest.cases) {
  test('Go and TS agree: '+fixture.id, async()=>{
    const raw=await readFile(path.join(fixtureRoot,fixture.file),'utf8');
    const ts=attempt(()=>decode(fixture.kind,raw,fixture.expectedExecution));
    const tsBytes=attempt(()=>decode(fixture.kind,new TextEncoder().encode(raw),fixture.expectedExecution));
    const fromGo=await go(fixture.kind,raw,fixture.expectedExecution);
    assert.equal(ts.valid,fixture.valid,ts.error?.message);
    assert.equal(tsBytes.valid,fixture.valid,tsBytes.error?.message);
    assert.equal(fromGo.valid,fixture.valid,fromGo.code);
    assert.equal(fromGo.valid,ts.valid);
    if (!fixture.valid) {
      assert.equal(fromGo.code,ts.error?.code,'Go and TS error categories disagree');
      assert.equal(tsBytes.error?.code,ts.error?.code,'TS text and byte error categories disagree');
      return;
    }
    const original=JSON.parse(raw);
    assert.deepEqual(tsBytes.value,ts.value,'TS text and UTF-8 bytes disagree');
    assert.deepEqual(fromGo.document,original,'Go typed decoding lost fields');
    assert.deepEqual(decode(fixture.kind,JSON.stringify(fromGo.document),fixture.expectedExecution),original,'Go → TS failed');
    const tsProduced=JSON.stringify(ts.value);
    const afterGo=await go(fixture.kind,tsProduced,fixture.expectedExecution);
    assert.equal(afterGo.valid,true,afterGo.code);
    assert.deepEqual(afterGo.document,original,'TS → Go failed');
    assert.deepEqual(decode(fixture.kind,JSON.stringify(afterGo.document)),original,'TS → Go → TS failed');
  });
}
test('validation preserves absent, null and explicit zero and never inserts defaults',async()=>{
  for (const name of ['result-cost-null.json','result-empty-cost-absent.json','result-rich.json']) {
    const value=JSON.parse(await readFile(path.join(fixtureRoot,name),'utf8'));
    const before=JSON.stringify(value);validate('ScanResult',value);assert.equal(JSON.stringify(value),before);
  }
});
test('aliases, limits, enum kinds and source hashes are stable',()=>{
  assert.deepEqual(normalizeLegacyErrorCode('auth_failed'),{code:'authentication_error',recognized:true});
  assert.deepEqual(normalizeLegacyErrorCode('rate_limited'),{code:'rate_limit',recognized:true});
  assert.deepEqual(normalizeLegacyErrorCode('result_corrupt'),{code:'invalid_result',recognized:true});
  assert.deepEqual(normalizeLegacyErrorCode('authentication_error'),{code:'authentication_error',recognized:true});
  assert.deepEqual(normalizeLegacyErrorCode('HTTP 401 in arbitrary text'),{code:'unknown',recognized:false});
  assert.equal(exitCode('partial'),2);
  assert.match(SCHEMA_HASH,/^[a-f0-9]{64}$/);
  assert.equal(CONTRACT_KINDS.length,85);
  assert.ok(CONTRACT_KINDS.includes('AgentRuntime'));
  assert.throws(()=>decode('ProgressSnapshot',' '.repeat(COMPATIBILITY.limits.progressBytes+1)),e=>e.code==='payload_too_large');
  assert.throws(()=>decode('ExecutionRef',new Uint8Array([255])),e=>e.code==='invalid_json');
  assert.throws(()=>decode('Unknown','{}'),e=>e.code==='unsupported_kind');
});
test('scanner describe advertises current input, result and observation contracts',async()=>{
  const tempOutput=process.env.AXIOM_CONTRACT_TEST_OUTPUT;
  assert.ok(tempOutput);
  const p=spawn(process.execPath,['--no-warnings','--experimental-strip-types','--loader',path.join(root,'tools/scanner-test-loader.mjs'),
    path.join(root,'../code-audit-scanner/src/main.ts'),'--describe','--output',tempOutput],
    {env:{...process.env,OPENAI_API_KEY:'fixture-not-a-real-key',AXIOM_BUSINESS_CONTEXT:'fixture context',AXIOM_CONTRACT_ENGINE_STUB:new URL('./fixtures/forbidden-engine.mjs',import.meta.url).href}});
  let stdout='',stderr='';p.stdout.on('data',d=>stdout+=d);p.stderr.on('data',d=>stderr+=d);
  const code=await new Promise((resolve,reject)=>{p.on('error',reject);p.on('close',resolve)});
  assert.equal(code,0,stderr);
  const value=decode('ScannerDescriptionV13',stdout);
  assert.equal(value.runtime.input,'scanner_job');
  assert.deepEqual(value.runtime.versions.job,[1,2,3,4,5,6,7,8]);
  assert.equal(value.capabilities.jobFile,true);
  assert.equal(value.capabilities.maxCostUsd,true);
  assert.equal(value.runtime.events,'scan_event');
  assert.deepEqual(value.runtime.versions.result,[1]);
  assert.deepEqual(value.baselineVersions.job,[1,2,3,4,5,6,7,8]);
  assert.equal(stdout.includes('fixture-not-a-real-key'),false);
  await assert.rejects(access(tempOutput),'--describe created a scan output directory');
});
test('legacy CLI keeps its existing task fields and environment forwarding',async()=>{
  const output=await mkdtemp(path.join(os.tmpdir(),'axiom-legacy-cli-'));
  try {
    const p=spawn(process.execPath,['--no-warnings','--experimental-strip-types','--loader',path.join(root,'tools/scanner-test-loader.mjs'),path.join(root,'../code-audit-scanner/src/main.ts'),
      '--source',fixtureRoot,'--output',output,'--task-id','42','--provider','openai','--model','fixture-model','--mode','standard','--timeout','2','--include','src/a.ts,src/中文.ts','--focus','authz','--max-cost','4','--mock'],
      {env:{...process.env,AXIOM_VALIDATION_MODE:'exploitability',AXIOM_BUSINESS_CONTEXT:'fixture\n第二行',AXIOM_CONTRACT_ENGINE_STUB:new URL('./fixtures/legacy-engine.mjs',import.meta.url).href}});
    let stdout='',stderr='';p.stdout.on('data',d=>stdout+=d);p.stderr.on('data',d=>stderr+=d);
    const code=await new Promise((resolve,reject)=>{p.on('error',reject);p.on('close',resolve)});
    assert.equal(code,0,stderr);
    assert.deepEqual(JSON.parse(stdout).legacyTask,{taskId:42,sourcePath:fixtureRoot,outputPath:output,provider:'openai',model:'fixture-model',scanMode:'standard',timeoutMinutes:2,
      validationMode:'exploitability',businessContext:'fixture\n第二行',includePaths:['src/a.ts','src/中文.ts'],scanFocus:['authz'],maxCostUsd:4,mock:true});
  } finally {await rm(output,{recursive:true,force:true});}
});
test('TS matrix and digest match the same canonical data used by Go',async()=>{
 const raw=await readFile(path.join(root,'data/execution-compatibility.json'),'utf8');
 assert.deepEqual(EXECUTION_MATRIX,JSON.parse(raw));
 assert.equal(EXECUTION_MATRIX_HASH,createHash('sha256').update(raw).digest('hex'));
});
test.after(async()=>{
  processGo.stdin.end();
  if (processGo.exitCode===null) await new Promise(resolve=>processGo.once('exit',resolve));
  assert.equal(processGo.exitCode,0,stderr);
});
