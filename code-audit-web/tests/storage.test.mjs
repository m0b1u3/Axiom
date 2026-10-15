import {test} from 'node:test';import assert from 'node:assert/strict';import {readFile} from 'node:fs/promises';import ts from 'typescript';
const source=await readFile(new URL('../src/utils/storage.ts',import.meta.url),'utf8');const js=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText;
const {formatStorageBytes}=await import('data:text/javascript;base64,'+Buffer.from(js).toString('base64'));
test('storage displays actual small files, known zero, unknown and large byte counts distinctly',()=>{
 assert.equal(formatStorageBytes(null),'未知');assert.equal(formatStorageBytes(undefined),'未知');assert.equal(formatStorageBytes(-1),'未知');assert.equal(formatStorageBytes(NaN),'未知');assert.equal(formatStorageBytes(0),'0 B');assert.equal(formatStorageBytes(36),'36 B');assert.equal(formatStorageBytes(1024),'1 KiB');assert.equal(formatStorageBytes(1073741824),'1 GiB');assert.equal(formatStorageBytes(18014398509481982n),'16 PiB');
});
