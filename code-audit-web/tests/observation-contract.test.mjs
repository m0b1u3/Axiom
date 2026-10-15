import {test} from 'node:test';import assert from 'node:assert/strict';import {readFile} from 'node:fs/promises';import ts from 'typescript';
async function module(name){const code=ts.transpileModule(await readFile(new URL(`../src/utils/${name}.ts`,import.meta.url),'utf8'),{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText;return import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);}
const {activityEntry,fileProgressLabel,phaseProgressLabel,mergeLogEvents,progressEntries}=await module('scan-log');const {progressView}=await module('scan-progress');
const root=new URL('../../contracts/fixtures/v1/',import.meta.url);const rich=JSON.parse(await readFile(new URL('observation-rich.json',root)));const unknown=JSON.parse(await readFile(new URL('observation-unknown.json',root)));
function row(event){return {event,n:event.sequence,timestamp:event.occurredAt,level:event.level,kind:event.type,message:event.message};}
const event=JSON.parse(await readFile(new URL('observation-event-activity.json',root)));
test('Web progress projects zero and unknown values distinctly without converting deep limits to file coverage',()=>{
 const p=progressView(rich);assert.equal(p.taskId,'9007199254740993');assert.equal(p.filesCompleted,0);assert.match(fileProgressLabel(p),/0 \/ 0 文件/);assert.match(phaseProgressLabel(p),/0 \/ 3/);assert.equal(p.costUsd,0);assert.equal(p.snapshot.deep.maximum,9);
 const absent=progressView(unknown);assert.equal(absent.rawPhase,'future_audit_phase');assert.equal(absent.costUsd,undefined);assert.equal(fileProgressLabel(absent),'文件进度尚未上报');
 assert.equal(progressView(null,JSON.stringify(rich)),null,'versioned data cannot fall back to a legacy cast');
});
test('display text and language never determine structured event status, kind or error recovery',()=>{
 const first=activityEntry(row(event));assert.equal(first.failed,false);assert.equal(first.label,'执行中');assert.equal(first.category,'command');
 const translated=structuredClone(event);translated.message='scan failed';translated.data.description='扫描已经完成（只是描述）';const next=activityEntry(row(translated));assert.equal(next.failed,false);assert.equal(next.label,'执行中');
 translated.data.status='failed';const failed=activityEntry(row(translated));assert.equal(failed.failed,true);assert.equal(failed.label,'调用失败');assert.equal(progressEntries([row(translated)],'2099-01-01T00:00:00Z')[0].failed,true);
});
test('reconnect replay merges by execution and sequence, keeping the original immutable event',()=>{
 const old=row(event),replay=row({...event,message:'文字已变化'});const merged=mergeLogEvents([old],[replay]);assert.equal(merged.length,1);assert.equal(merged[0],old);
 const other=row({...event,execution:{...event.execution,attempt:3}});assert.equal(mergeLogEvents([old],[other]).length,2);
});
test('error and worker meaning survive unrelated message content',async()=>{
 const error=JSON.parse(await readFile(new URL('observation-event-error.json',root)));error.message='all good';const e=activityEntry(row(error));assert.equal(e.failed,true);assert.match(e.summary,/rate_limit/);assert.match(e.summary,/engine/);
 const worker=JSON.parse(await readFile(new URL('observation-event-worker.json',root)));worker.message='scan completed';const w=activityEntry(row(worker));assert.match(w.summary,/等待下一轮/);assert.equal(w.failed,false);
});
