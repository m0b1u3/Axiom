import {test} from 'node:test';import assert from 'node:assert/strict';import {readFile} from 'node:fs/promises';import ts from 'typescript';
const code=ts.transpileModule(await readFile(new URL('../src/utils/git-diff.ts',import.meta.url),'utf8'),{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText;
const {gitTarget,sameGitTarget,gitEvidenceBinding}=await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
const fixture=JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/git-assignment.json',import.meta.url),'utf8'));fixture.execution.taskId='42';
test('Web Git bindings require the selected execution, source and committed target',()=>{
 assert.ok(gitTarget(fixture.target));assert.ok(gitEvidenceBinding(fixture,42,2,fixture.sourceContentHash,fixture.target));
 for(const change of [v=>v.execution.attempt++,v=>v.execution.taskId='43',v=>v.sourceContentHash='f'.repeat(64),v=>v.evidenceRevision='base',v=>v.target.headCommit=v.target.baseCommit,v=>v.target.headTree='f'.repeat(40),v=>v.target.schemaVersion=2,v=>v.sourceSnapshotId=7,v=>v.sourceSnapshotId='0']){const v=structuredClone(fixture);change(v);assert.equal(gitEvidenceBinding(v,42,2,fixture.sourceContentHash,fixture.target),null);}
});
test('Git identity comparison is independent of JSON object property order',()=>{
 const reverse=Object.fromEntries(Object.entries(fixture.target).reverse());assert.ok(sameGitTarget(fixture.target,reverse));assert.ok(gitEvidenceBinding({...fixture,target:reverse},42,2,fixture.sourceContentHash,fixture.target));assert.equal(sameGitTarget(null,fixture.target),false);
});
