import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,mkdirSync,writeFileSync,readFileSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {apply,digest} from './apply.mjs';
function fixture(t){const root=mkdtempSync(path.join(tmpdir(),'axiom-sdk-patch-'));t.after(()=>rmSync(root,{recursive:true,force:true}));const pkg=path.join(root,'node_modules','test-sdk');mkdirSync(pkg,{recursive:true});const info=JSON.stringify({name:'test-sdk',version:'1'});writeFileSync(path.join(pkg,'package.json'),info);writeFileSync(path.join(pkg,'a.js'),'before');writeFileSync(path.join(pkg,'b.js'),'stable');writeFileSync(path.join(root,'after'),'after');const m={schema:1,packages:[{path:'test-sdk',name:'test-sdk',version:'1',files:{'package.json':digest(info),'a.js':digest('after'),'b.js':digest('stable')}}],patches:[{path:'test-sdk/a.js',source:'after',before:digest('before'),after:digest('after')}]};const manifestPath=path.join(root,'manifest.json');writeFileSync(manifestPath,JSON.stringify(m));return {root,pkg,manifestPath};}
test('rebuild applies known patch, verifies and is idempotent',t=>{const f=fixture(t);assert.equal(apply(f),1);assert.equal(apply(f),0);assert.equal(apply({...f,check:true}),0)});
test('unknown change stops the whole application before writes',t=>{const f=fixture(t);writeFileSync(path.join(f.pkg,'b.js'),'unknown');assert.throws(()=>apply(f),/content drift/);assert.equal(readFileSync(path.join(f.pkg,'a.js'),'utf8'),'before')});
test('altered patch fails checksum verification',t=>{const f=fixture(t);writeFileSync(path.join(f.root,'after'),'tampered');assert.throws(()=>apply(f),/checksum/)});
test('new unmanaged files fail verification',t=>{const f=fixture(t);writeFileSync(path.join(f.pkg,'extra.js'),'extra');assert.throws(()=>apply(f),/inventory drift/)});
