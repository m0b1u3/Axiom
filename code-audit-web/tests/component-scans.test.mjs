import {test} from 'node:test';import assert from 'node:assert/strict';import {readFile} from 'node:fs/promises';import ts from 'typescript';
const source=await readFile(new URL('../src/utils/component-scans.ts',import.meta.url),'utf8');
const js=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText.replace('../../../contracts/ts/src/index',new URL('../../contracts/ts/dist/index.js',import.meta.url).href);
const {checkedComponentExecutions,componentRows,componentBinding,componentResult,componentMoney}=await import('data:text/javascript;base64,'+Buffer.from(js).toString('base64'));
const fixture=async name=>JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/'+name+'.json',import.meta.url),'utf8'));
test('component history reader rejects foreign parent/source/version and malformed budget snapshots',async()=>{
 const result=await fixture('component-scan-result');result.execution.taskId='42';result.request.execution.taskId='42';result.snapshot.execution.taskId='42';const value={request:result.request,attempts:[{request:result.request,snapshot:result.snapshot,status:'completed'}]};
 assert.ok(checkedComponentExecutions(value,Number(result.execution.taskId))); // The deployment only uses safe numeric IDs; contract identities remain strings.
 for(const mutate of [v=>v.request.execution.taskId='9',v=>v.attempts[0].snapshot.execution.attempt++,v=>v.attempts[0].snapshot.source.contentHash='f'.repeat(64),v=>v.attempts[0].snapshot.versionSha256='a'.repeat(64),v=>v.attempts[0].snapshot.observedMicros=0]){const v=JSON.parse(JSON.stringify(value));mutate(v);assert.throws(()=>checkedComponentExecutions(v,Number(result.execution.taskId)))}
});
test('latest component selection retains successful historical evidence across failed-only retry',async()=>{
 const result=await fixture('component-scan-result');result.request.execution.taskId='42';result.snapshot.execution.taskId='42';
 const previous={request:JSON.parse(JSON.stringify(result.request)),snapshot:JSON.parse(JSON.stringify(result.snapshot)),status:'partial'};previous.snapshot.components[1].status='failed';previous.snapshot.components[1].finalMicros=null;
 const retry=JSON.parse(JSON.stringify(previous));retry.request.execution.attempt=3;retry.request.selectedIndices=[1];retry.snapshot.execution.attempt=3;retry.snapshot.components=[retry.snapshot.components[1]];retry.snapshot.components[0].status='completed';retry.snapshot.components[0].finalMicros=125000;retry.snapshot.observedMicros=125000;
 const view={request:result.request,attempts:[previous,retry]};const rows=componentRows(view);assert.equal(rows[0].attempt,2);assert.equal(rows[1].attempt,3);assert.equal(componentRows(view,3)[0].receipt,null);assert.equal(componentRows(view,2)[1].receipt.status,'failed');
});
test('unknown costs and invalid finding/version bindings never become zero or evidence',async()=>{
 assert.equal(componentMoney(null),'未知');assert.equal(componentMoney(0),'0');assert.equal(componentMoney(125000),'0.125');const b=await fixture('component-finding-binding');assert.ok(componentBinding(b));assert.equal(componentBinding({...b,versionSha256:'not-a-digest'}),null);
 const result=await fixture('component-scan-result');assert.ok(componentResult(result));result.snapshot.matching.costState='known';assert.equal(componentResult(result),null);
});
