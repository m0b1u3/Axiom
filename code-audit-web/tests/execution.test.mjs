import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import ts from 'typescript';
const js=ts.transpileModule(await readFile(new URL('../src/utils/execution.ts',import.meta.url),'utf8'),{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText;
const {elapsed,failureHelp}=await import(`data:text/javascript;base64,${Buffer.from(js).toString('base64')}`);
test('timing distinguishes missing history, zero duration and timezone offsets',()=>{
 assert.equal(elapsed(null),'—');assert.equal(elapsed('bad'),'—');
 assert.equal(elapsed('2026-09-10T08:00:00+08:00','2026-09-10T00:01:05Z'),'1 分 5 秒');
 assert.equal(elapsed('2026-09-10T00:01:05Z','2026-09-10T00:00:00Z'),'0 秒');
});
test('failure guidance does not fabricate a cause from a generic failure',()=>{
 assert.equal(failureHelp('','completed'),null);
 assert.equal(failureHelp('scanner_error','failed').title,'扫描执行失败');
 assert.match(failureHelp('rate_limit','failed').action,/降低/);
 assert.match(failureHelp('timeout','timeout').action,/沿用本任务参数/);
});
test('quota exhaustion directs users to billing, not repeated retries',()=>{
 assert.equal(failureHelp('quota_exhausted','failed').title,'模型额度已耗尽');
 assert.match(failureHelp('quota_exhausted','failed').action,/免费额度/);
});
