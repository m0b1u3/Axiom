import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import ts from 'typescript';
const source=await readFile(new URL('../src/utils/page-load-error.ts',import.meta.url),'utf8');
const code=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText;
const {isPageLoadError,pageReloadTarget}=await import('data:text/javascript;base64,'+Buffer.from(code).toString('base64'));

test('missing dynamic imports and CSS chunks are recognized across supported browser errors',()=>{
  for(const text of ['Failed to fetch dynamically imported module: https://fixture.invalid/assets/NewScan-abc.js','Importing a module script failed.','error loading dynamically imported module: https://fixture.invalid/chunk.js','Loading chunk 42 failed.','Loading CSS chunk settings failed.','Unable to preload CSS for /assets/style-abc.css']){
    assert.equal(isPageLoadError(new Error(text)),true,text);
    assert.equal(isPageLoadError({message:text}),true,text);
  }
});
test('API, model and unrelated JavaScript errors retain their original handling',()=>{
  for(const error of [new Error('输入不能为空'),new Error('Failed to fetch'),new Error('401 Unauthorized'),new Error('Cannot read properties of undefined'),null,{},404])assert.equal(isPageLoadError(error),false);
});
test('manual recovery preserves the target route and query without external redirects',()=>{
  const current='https://fixture.invalid/scans/14?attempt=1#evidence';
  assert.equal(pageReloadTarget('/scans/new?projectId=2#source',current),'/scans/new?projectId=2#source');
  for(const target of ['https://outside.invalid/','//outside.invalid/','javascript:alert(1)','http://fixture.invalid/scans/new'])assert.equal(pageReloadTarget(target,current),'/scans/14?attempt=1#evidence');
});
