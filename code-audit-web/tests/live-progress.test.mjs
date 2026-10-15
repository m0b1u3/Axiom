import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import ts from 'typescript';
const js=ts.transpileModule(await readFile(new URL('../src/utils/scan-log.ts',import.meta.url),'utf8'),{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText;
const {progressEntries,fileProgressLabel,phaseProgressLabel,activityEntry}=await import(`data:text/javascript;base64,${Buffer.from(js).toString('base64')}`);
test('progress failures require later structured success evidence to show recovered',()=>{
 const event={n:1,timestamp:'2026-09-10T01:32:54Z',kind:'activity',level:'error',message:'failed tool update_codex_security_scan_progress'};
 assert.equal(progressEntries([event])[0].failed,true);
 assert.equal(progressEntries([event],'2026-09-10T01:31:00Z')[0].failed,true);
 assert.equal(progressEntries([event],'2026-09-10T01:33:07Z')[0].recovered,true);
 assert.equal(progressEntries([{...event,message:'failed command cat missing'}],'2026-09-10T01:33:07Z')[0].failed,true);
});
test('unknown file counts and phase units remain distinct',()=>{
 assert.match(fileProgressLabel({filesTotal:175,filesCompleted:0}),/尚未上报/);
 assert.match(fileProgressLabel({filesTotal:175,filesCompleted:null}),/尚未上报/);
 assert.equal(fileProgressLabel({filesTotal:175,filesCompleted:5}),'扫描器上报 5 / 175 文件 · 未核验覆盖');
 assert.equal(phaseProgressLabel({phaseProgress:{completed:2,total:3,unit:'review_receipts'}}),'2 / 3 项审查回执');
 assert.match(activityEntry({message:'t1 [system] 会话开始',kind:'session',level:'info'}).summary,/不代表重新扫描/);
});
