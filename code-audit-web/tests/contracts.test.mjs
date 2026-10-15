import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { parse, compileStyle } from '@vue/compiler-sfc';
import ts from 'typescript';
import axios from 'axios';

// Compile real scoped styles: a misplaced :global() previously inverted the root.
test('dark icon treatment never targets the document root', async () => {
  const source = await readFile(new URL('../src/views/ModelConfigs.vue', import.meta.url), 'utf8');
  const { descriptor } = parse(source);
  const css = descriptor.styles.map(style => compileStyle({source:style.content, filename:'ModelConfigs.vue', id:'data-v-test', scoped:style.scoped}).code).join('\n');
  const filters = [...css.matchAll(/([^{}]+)\{[^{}]*filter:\s*invert\(1\)[^{}]*\}/g)];
  assert.equal(filters.length, 1);
  assert.match(filters[0][1], /\.dark \.provider img\[src\*="openai"\]/);
  assert.doesNotMatch(css, /\.dark\s*\{[^}]*filter:/);
});

const memory = new Map();
globalThis.localStorage = {getItem:key=>memory.get(key)??null,setItem:(key,value)=>memory.set(key,String(value)),removeItem:key=>memory.delete(key)};
globalThis.window = new EventTarget();
let response = {data:{id:42}}, last;
axios.defaults.adapter = async config => {
  last = config;
  if (response instanceof Error) throw response;
  return {data:response,status:200,statusText:'OK',headers:{},config};
};
let js = ts.transpileModule(await readFile(new URL('../src/api.ts',import.meta.url),'utf8'),{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText;
js = js.replace('from "axios"',`from ${JSON.stringify(import.meta.resolve('axios'))}`);
const {api,setToken,getToken,ApiError} = await import(`data:text/javascript;base64,${Buffer.from(js).toString('base64')}`);

test('real API client preserves payloads, authorization, envelopes and blobs', async () => {
  setToken('test-only-token');
  const payload = {name:'Review model',provider:'custom',baseUrl:'https://example.invalid/v1',model:'test-model',remark:'',websiteUrl:'',wireApi:'responses'};
  assert.deepEqual(await api.updateModelConfig(42,payload),{id:42});
  assert.equal(last.baseURL,'/api/v1');
  assert.equal(last.url,'/model-configs/42');
  assert.equal(last.method,'put');
  assert.equal(last.headers.Authorization,'Bearer test-only-token');
  assert.deepEqual(JSON.parse(last.data),payload);
  assert.ok(!Object.hasOwn(JSON.parse(last.data),'apiKey'),'blank edit must omit secret replacement');
  await api.enableModelConfig(42,true);
  assert.equal(last.url,'/model-configs/42/enable');
  assert.deepEqual(JSON.parse(last.data),{enabled:true});
  const scan = {sourceSnapshotId:7,modelConfigId:42,projectId:3,scanMode:'standard',timeoutMinutes:30};
  await api.createScan(scan);
  assert.equal(last.url,'/scans');
  assert.deepEqual(JSON.parse(last.data),scan);
  await api.setFindingStatus(9,'false_positive');
  assert.equal(last.url,'/findings/9/status');
  assert.deepEqual(JSON.parse(last.data),{status:'false_positive'});
  await api.listFindingsPool({q:'SQL & PHP',severity:'high'});
  assert.equal(new URL(last.url,'http://test').searchParams.get('q'),'SQL & PHP');
  response = new Blob(['%PDF-test'],{type:'application/pdf'});
  assert.equal(await api.exportReportPdf(9),response);
  assert.equal(last.responseType,'blob');
  assert.equal(last.timeout,120000);
});

test('expired session redirects once through the app event; failed login does not', async () => {
  let expired=0;
  const listener=()=>expired++;
  window.addEventListener('axiom:session-expired',listener);
  setToken('expired-token');
  response=Object.assign(new Error('Unauthorized'),{response:{status:401,data:{message:'会话已失效'}},config:{url:'/projects',headers:{Authorization:'Bearer expired-token'}}});
  await assert.rejects(api.listProjects(),/会话已失效/);
  assert.equal(getToken(),'');
  assert.equal(expired,1);
  response.config.url='/auth/login';
  await assert.rejects(api.login('test','wrong'),/会话已失效/);
  assert.equal(expired,1);
  window.removeEventListener('axiom:session-expired',listener);
});

test('a late 401 cannot revoke a newer login, and 403/429 preserve session and retry details', async () => {
  setToken('new-session');
  response=Object.assign(new Error('Unauthorized'),{response:{status:401,data:{message:'旧会话已失效'}},config:{url:'/projects',headers:{Authorization:'Bearer previous-session'}}});
  await assert.rejects(api.listProjects(),/旧会话已失效/);
  assert.equal(getToken(),'new-session');
  response=Object.assign(new Error('Forbidden'),{response:{status:403,data:{message:'当前账户没有权限'}},config:{url:'/users',headers:{Authorization:'Bearer new-session'}}});
  await assert.rejects(api.listAccounts(),e=>e instanceof ApiError && e.status===403);
  assert.equal(getToken(),'new-session');
  response=Object.assign(new Error('Limited'),{response:{status:429,headers:{'retry-after':'900'},data:{message:'登录尝试过多'}},config:{url:'/auth/login'}});
  await assert.rejects(api.login('fixture','wrong'),e=>e instanceof ApiError && e.status===429 && e.retryAfter===900);
  assert.equal(getToken(),'new-session');
});

test('log normalization retains details, surfaces legacy failures and deduplicates cursor overlaps', async () => {
 const src=await readFile(new URL('../src/utils/scan-log.ts',import.meta.url),'utf8');
 const code=ts.transpileModule(src,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText;
 const {activityEntry,isImportant,mergeLogEvents}=await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
 const event={n:28,timestamp:'2026-09-09T14:31:54.312Z',kind:'activity',level:'info',message:'failed tool update_codex_security_scan_progress'};
 assert.equal(activityEntry(event).failed,true);
 assert.equal(isImportant(activityEntry(event)),true);
 const long={...event,n:29,message:'completed command '+ 'a'.repeat(600)+'\nsecond line'};
 assert.equal(activityEntry(long).message,long.message);
 assert.equal(isImportant(activityEntry(long)),false);
 assert.deepEqual(mergeLogEvents([event],[event,long]).map(e=>e.n),[28,29]);
 assert.equal(activityEntry({...event,kind:'progress',message:'phase=validation files=1/1'}).summary,'验证发现 · 已处理 1/1 文件');
});

test('execution settings writes omit server-owned metadata and archive requests name an attempt', async () => {
  response={data:{global:2,perModel:1,perProject:2,revision:4}};
  await api.saveExecutionSettings({global:2,perModel:1,perProject:2,revision:3,updatedBy:'spoof',updatedAt:'unused'});
  assert.deepEqual(JSON.parse(last.data),{global:2,perModel:1,perProject:2,revision:3});
  assert.equal(last.method,'put');
  await api.attemptResult(9,2);assert.equal(last.url,'/scans/9/attempts/2/result');
  await api.queueOverview(9);assert.equal(last.params.taskId,9);
});
test('budget changes and reconciliation name task, attempt and expected revision',async()=>{
 response={data:{schemaVersion:1,taskId:'9'}};
 await api.updateTaskBudget(9,8.5,12);assert.equal(last.url,'/scans/9/budget');assert.deepEqual(JSON.parse(last.data),{maxTotalCostUsd:8.5,revision:12});
 await api.reconcileTaskCost(9,2,0,'Fixture explicit zero estimate',13);assert.equal(last.url,'/scans/9/budget/attempts/2');assert.deepEqual(JSON.parse(last.data),{estimatedUsd:0,note:'Fixture explicit zero estimate',revision:13});
});

test('storage policy and cleanup actions preserve revisions and only submit object identity',async()=>{
 response={data:{schemaVersion:1,revision:3}};
 await api.updateStoragePolicy({revision:2,minFreeBytes:0,orphanGraceHours:48});assert.equal(last.url,'/storage/policy');assert.deepEqual(JSON.parse(last.data),{revision:2,minFreeBytes:0,orphanGraceHours:48});
 await api.previewCleanup('scan',9);assert.deepEqual(JSON.parse(last.data),{kind:'scan',resourceId:9});
 await api.executeCleanup('fixture-job',4);assert.equal(last.url,'/storage/cleanup/fixture-job/execute');assert.deepEqual(JSON.parse(last.data),{revision:4});
 await api.cancelCleanup('fixture-job',5);assert.deepEqual(JSON.parse(last.data),{revision:5});assert.equal(last.url,'/storage/cleanup/fixture-job/cancel');
});
