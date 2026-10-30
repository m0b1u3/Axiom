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

function isolatedFixture(t){
 const root=mkdtempSync(path.join(tmpdir(),'axiom-sdk-isolation-'));t.after(()=>rmSync(root,{recursive:true,force:true}));
 const sdkPath='@openai/codex-security',pkg=path.join(root,'node_modules',sdkPath),compat=path.join(root,'node_modules/@axiom/codex-security-compat-v1');
 const files={'package.json':JSON.stringify({name:sdkPath,version:'0.5.0'}),'dist/config.js':'export const features={multi_agent_v2:{enabled:true}};','dist/api.js':'// stable api','dist/runtime.js':'// stable runtime'};
 for(const [name,data] of Object.entries(files)){mkdirSync(path.dirname(path.join(pkg,name)),{recursive:true});writeFileSync(path.join(pkg,name),data);}
 const depPath='@openai/codex-sdk',dep=JSON.stringify({name:depPath,version:'0.163.0'});mkdirSync(path.join(root,'node_modules',depPath),{recursive:true});writeFileSync(path.join(root,'node_modules',depPath,'package.json'),dep);
 const after='export const features={multi_agent:true};';writeFileSync(path.join(root,'after'),after);
 const upstream=path.join(root,'upstream',sdkPath,'dist/config.js');mkdirSync(path.dirname(upstream),{recursive:true});writeFileSync(upstream,files['dist/config.js']);
 const m={schema:1,runtimeIsolation:{schemaVersion:1},packages:[{path:sdkPath,name:sdkPath,version:'0.5.0',files:Object.fromEntries(Object.entries({...files,'dist/config.js':after}).map(([k,v])=>[k,digest(v)]))},{path:depPath,name:depPath,version:'0.163.0',files:{'package.json':digest(dep)}}],patches:[{path:sdkPath+'/dist/config.js',source:'after',before:digest(files['dist/config.js']),after:digest(after)}]};
 const manifestPath=path.join(root,'manifest.json');writeFileSync(manifestPath,JSON.stringify(m));return {root,pkg,compat,manifestPath,before:files['dist/config.js'],after};
}
test('isolated install preserves upstream and confines V1 to a distinct, reproducible package',t=>{
 const f=isolatedFixture(t);assert.equal(apply(f),1);assert.equal(readFileSync(path.join(f.pkg,'dist/config.js'),'utf8'),f.before);assert.equal(readFileSync(path.join(f.compat,'dist/config.js'),'utf8'),f.after);assert.equal(apply(f),0);assert.equal(apply({...f,check:true}),0);
 const d=JSON.parse(readFileSync(path.join(f.root,'node_modules/.axiom-sdk-runtimes.json'))).runtimes;assert.notEqual(d['official-v2'].fingerprint,d['legacy-v1'].fingerprint);
});
test('existing patched installations migrate only known bytes back to upstream',t=>{
 const f=isolatedFixture(t);writeFileSync(path.join(f.pkg,'dist/config.js'),f.after);assert.equal(apply(f),2);assert.equal(readFileSync(path.join(f.pkg,'dist/config.js'),'utf8'),f.before);assert.equal(readFileSync(path.join(f.compat,'dist/config.js'),'utf8'),f.after);
});
test('drift in either isolated runtime stops rebuilding before touching the other',t=>{
 const f=isolatedFixture(t);apply(f);writeFileSync(path.join(f.compat,'dist/api.js'),'unmanaged');assert.throws(()=>apply(f),/content drift/);assert.equal(readFileSync(path.join(f.pkg,'dist/config.js'),'utf8'),f.before);
});
