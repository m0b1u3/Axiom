import {test} from 'node:test';import assert from 'node:assert/strict';import {readFile} from 'node:fs/promises';import ts from 'typescript';
const compile=async file=>ts.transpileModule(await readFile(new URL('../src/utils/'+file,import.meta.url),'utf8'),{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText;
const dep='data:text/javascript;base64,'+Buffer.from(await compile('advanced-scan-input.ts')).toString('base64');
const code=(await compile('component-scan-input.ts')).replace("'./advanced-scan-input'",JSON.stringify(dep));
const {componentSDKInput}=await import('data:text/javascript;base64,'+Buffer.from(code).toString('base64'));
test('component form keeps fixed knowledge IDs and multiline prompts without injecting scope or validation controls',()=>{
 assert.deepEqual(componentSDKInput([],'',''),{});
 const ids=[27,39],value=componentSDKInput(ids,' 第一行\n第二行 ','合成后处理');ids.push(44);
 assert.deepEqual(value,{knowledgeVersionIds:[27,39],scanPrompt:'第一行\n第二行',postScanPrompt:'合成后处理'});
 for(const v of ['x'.repeat(16001),'invalid\0prompt'])assert.throws(()=>componentSDKInput([],v,''));
});
