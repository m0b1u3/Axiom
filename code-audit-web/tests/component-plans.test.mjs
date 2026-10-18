import {test} from 'node:test';import assert from 'node:assert/strict';import {readFile} from 'node:fs/promises';import ts from 'typescript';
const source=await readFile(new URL('../src/utils/component-plans.ts',import.meta.url),'utf8');
const module=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText.replace('../../../contracts/ts/src/index',new URL('../../contracts/ts/dist/index.js',import.meta.url).href);
const {componentDocument,componentView,canFreezeComponentPlan,checkedComponentVersion,sameComponentSource}=await import('data:text/javascript;base64,'+Buffer.from(module).toString('base64'));
const fixture=async name=>JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/'+name+'.json',import.meta.url),'utf8'));
const plan=await fixture('component-snapshot'),result=await fixture('component-result'),version=await fixture('component-version'),view={plan,result,operation:result.operation,operationError:null,operationTaskId:result.execution.taskId,operationStatus:'completed',resultSha256:'a'.repeat(64)};
test('plan input accepts root and Unicode, rejects escape, unknown fields and corrupt JSON',()=>{
 assert.deepEqual(componentDocument(JSON.stringify(plan.document)),plan.document);assert.throws(()=>componentDocument('{broken'),/JSON 无法解析/);
 for(const document of [{components:[]},{components:[{name:'a',paths:['../x']}]},{components:[{name:'a',paths:['/etc/passwd']}]},{...plan.document,unknown:'value'}])assert.throws(()=>componentDocument(JSON.stringify(document)),/规范相对路径/);
});
test('freeze readiness requires the current successful local validation with bound source and identity',()=>{
 assert.ok(componentView(view));assert.equal(canFreezeComponentPlan(view),true);
 for(const mutate of [v=>v.plan.revision++,v=>v.operationStatus='failed',v=>v.result.operation.kind='suggest',v=>v.result.coverage.valid=false,v=>v.resultSha256=null]){const changed=structuredClone(view);mutate(changed);assert.equal(canFreezeComponentPlan(changed),false)}
 for(const mutate of [v=>v.operationTaskId='43',v=>v.result.operation.source.contentHash='b'.repeat(64),v=>v.result.operation.planId='43',v=>v.result.operation.execution.attempt++]){const changed=structuredClone(view);mutate(changed);assert.throws(()=>componentView(changed));}
});
test('frozen version readers preserve history and reject another plan, HEAD or source',()=>{
 assert.ok(checkedComponentVersion(version,plan));for(const mutate of [v=>v.planId='43',v=>v.source.contentHash='f'.repeat(64),v=>v.source.headCommit='a'.repeat(40)]){const changed=structuredClone(version);mutate(changed);assert.throws(()=>checkedComponentVersion(changed,plan),/不匹配/)}
 assert.equal(sameComponentSource(plan.source,{...plan.source,headCommit:'a'.repeat(40)}),false);
});
