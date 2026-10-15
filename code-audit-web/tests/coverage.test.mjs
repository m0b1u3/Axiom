import assert from 'node:assert/strict';
import {test} from 'node:test';
import ts from 'typescript';
import {readFile} from 'node:fs/promises';
const source=await readFile(new URL('../src/utils/coverage.ts',import.meta.url),'utf8');
const module=await import('data:text/javascript;base64,'+Buffer.from(ts.transpile(source,{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022})).toString('base64'));
test('missing coverage fields differ from explicitly empty arrays',()=>{const a=module.coverageSections({surfaces:[]});assert.equal(a[0].available,true);assert.equal(a[1].available,false);assert.equal(module.coverageLabel(null),'完整性未知');assert.equal(module.coverageLabel({completeness:'partial'}),'部分覆盖');assert.equal(module.coverageSections({deferred:[{reason:'missing setup'}]})[2].items.length,1)});
