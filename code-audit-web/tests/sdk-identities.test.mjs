import {test} from 'node:test';import assert from 'node:assert/strict';import {readFile} from 'node:fs/promises';import ts from 'typescript';
const source=await readFile(new URL('../src/utils/sdk-identities.ts',import.meta.url),'utf8');
const js=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText.replace('../../../contracts/ts/src/index',new URL('../../contracts/ts/dist/index.js',import.meta.url).href);
const {checkedIdentities,checkedIdentityOperations,usableIdentity,identitySelectionError}=await import('data:text/javascript;base64,'+Buffer.from(js).toString('base64'));
const fixture=async name=>JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/identity-'+name+'.json',import.meta.url),'utf8'));
test('Web refuses unknown identity contracts and authentication contradictions',async()=>{
 const value=await fixture('snapshot');assert.deepEqual(checkedIdentities([value]),[value]);for(const mutation of [{schemaVersion:2},{authenticated:false},{state:'unknown'},{cleanupPending:true},{apiKey:'must not be public'}])assert.throws(()=>checkedIdentities([{...value,...mutation}]));assert.throws(()=>checkedIdentities({items:[value]}));
});
test('scan selection requires an authenticated account for account-only models and never falls back silently',async()=>{
 const value=await fixture('snapshot');
 const base={id:value.identity.id,native:true,required:true,items:[value]};
 assert.equal(identitySelectionError(base),'');
 for(const change of [{id:''},{items:[]},{native:false},{error:'offline'},{items:[{...value,state:'revoked'}]},{items:[{...value,authenticated:false}]}])assert.notEqual(identitySelectionError({...base,...change}),'');
 assert.equal(identitySelectionError({...base,id:'',required:false,items:[],error:'offline'}),'');
 assert.notEqual(identitySelectionError({...base,required:false,items:[]}), '');
});
test('only authenticated identities with confirmed local cleanup can be selected',async()=>{
 const value=await fixture('snapshot');assert.equal(usableIdentity(value),true);for(const mutation of [{authenticated:null},{state:'unknown'},{state:'revoked'},{cleanupPending:true}])assert.equal(usableIdentity({...value,...mutation}),false);
 const op=await fixture('operation');assert.deepEqual(checkedIdentityOperations([op]),[op]);assert.throws(()=>checkedIdentityOperations([{...op,status:'waiting',instructions:null}]));assert.throws(()=>checkedIdentityOperations([{...op,action:'login_api_key'}]));
});
