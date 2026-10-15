import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';

function harness() {
 const source=fs.readFileSync(new URL('../src/views/ScanDetail.vue',import.meta.url),'utf8');
 const snippet=source.slice(source.indexOf('let loadBusy = false;'),source.indexOf('const focusFindingId'));
 let resolve, calls=0;
 const task={value:{status:'scanning',attempt:1}};
 const context={task,id:1,api:{getScan(){calls++;return new Promise(r=>{resolve=r})},queueOverview:async()=>({})},queueOverview:{value:null},queueError:{value:''},loadError:{value:''},progress:{value:null},findings:{value:[]},timer:undefined,progressView:()=>null,loadAttempts:()=>{},loadFindings:async()=>{}};
 vm.createContext(context);
 vm.runInContext(ts.transpile(snippet+'\nglobalThis.poll=load; globalThis.invalidate=()=>{loadGeneration++;}; globalThis.dispose=()=>{disposed=true;};'),context);
 return {context,task,calls:()=>calls,resolve:(v)=>resolve(v)};
}
test('slow polling cannot overlap or overwrite a newer action',async()=>{
 const h=harness();const first=h.context.poll();await h.context.poll();assert.equal(h.calls(),1);
 h.context.invalidate();h.task.value={status:'pending',attempt:2};
 h.resolve({status:'scanning',attempt:1});await first;
 assert.equal(h.task.value.status,'pending');assert.equal(h.task.value.attempt,2);
 const next=h.context.poll();h.resolve({status:'scanning',attempt:2});await next;assert.equal(h.task.value.status,'scanning');
});
test('response after unmount is ignored',async()=>{
 const h=harness();const pending=h.context.poll();h.context.dispose();h.resolve({status:'pending',attempt:2});await pending;assert.equal(h.task.value.attempt,1);
});
