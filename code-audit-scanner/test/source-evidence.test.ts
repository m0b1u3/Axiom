import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,mkdir,writeFile,readFile,symlink,rm} from 'node:fs/promises';
import path from 'node:path';
import {tmpdir} from 'node:os';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {extractSource,checkSourceEvidence} from '../runtime/source-evidence.mjs';
const exec=promisify(execFile);
const helper=new URL('../runtime/source-evidence.mjs',import.meta.url).pathname;
test('producer preserves indentation and whole prose lines; historical bad excerpts still fail',async t=>{
 const root=await mkdtemp(path.join(tmpdir(),'axiom-evidence-'));t.after(()=>rm(root,{recursive:true,force:true}));
 await writeFile(path.join(root,'app.py'),'# Fixtures / Mock\n        user = USERS.get(token)\n        return get_contact(user, contact_id)\n');
 await writeFile(path.join(root,'README.md'),'# Fixtures / Mock\nOnly same-tenant reads are allowed. Cross-tenant reads are forbidden.\n');
 const correct=[extractSource(root,'app.py',2,3),extractSource(root,'README.md',2,2)];
 assert.match(correct[0].code,/^ {8}user/);assert.match(correct[1].code,/forbidden\.$/);
 const draft={findings:[{codeEvidence:correct.map(({sourceFileSha256,...e})=>e)}]};
 assert.deepEqual(checkSourceEvidence(root,draft),{ok:true,checked:2,diagnostics:[]});
 const old=structuredClone(draft);old.findings[0]!.codeEvidence[0]!.code='user = USERS.get(token)\nreturn get_contact(user, contact_id)';old.findings[0]!.codeEvidence[1]!.code='Only same-tenant reads are allowed.';
 const before=JSON.stringify(old);const checked=checkSourceEvidence(root,old);assert.equal(checked.ok,false);assert.equal(checked.diagnostics.length,2);assert.equal(JSON.stringify(old),before,'checker must never repair model claims silently');
 const draftPath=path.join(root,'draft.json');await writeFile(draftPath,before);
 await assert.rejects(exec(process.execPath,[helper,'check',root,draftPath]),e=>{assert.equal((e as any).code,1);assert.equal(JSON.parse((e as any).stdout).diagnostics.length,2);return true});
 assert.equal(await readFile(draftPath,'utf8'),before);
 const extracted=JSON.parse((await exec(process.execPath,[helper,'extract',root,'app.py','2','3'])).stdout);assert.equal(extracted.code,correct[0].code);
});
test('exact checks reject changed tokens, wrong ranges, unsafe paths, symlinks and oversized files',async t=>{
 const root=await mkdtemp(path.join(tmpdir(),'axiom-evidence-boundary-'));t.after(()=>rm(root,{recursive:true,force:true}));
 await writeFile(path.join(root,'a.py'),'    allow(user)\r\n    return data\r\n');
 const evidence=extractSource(root,'a.py',1,2);assert.equal(evidence.code,'    allow(user)\n    return data');
 const check=(e:unknown)=>checkSourceEvidence(root,{findings:[{codeEvidence:[e]}]}).ok;
 assert.equal(check({...evidence,code:evidence.code.replaceAll('\n','\r\n')+'\r\n'}),true);
 for(const e of [{...evidence,code:evidence.code.replace('allow','deny')},{...evidence,startLine:2},{...evidence,endLine:1},{...evidence,code:'...'},null])assert.equal(check(e),false);
 for(const p of ['../a.py','/a.py','C:/a.py','a\\b.py','./a.py'])assert.throws(()=>extractSource(root,p,1,1));
 for(const n of [0,-1,NaN,1.5,99])assert.throws(()=>extractSource(root,'a.py',n,n));
 await symlink(path.join(root,'a.py'),path.join(root,'link.py'));assert.throws(()=>extractSource(root,'link.py',1,1));
 await mkdir(path.join(root,'sub'));await symlink(path.join(root,'sub'),path.join(root,'alias'));assert.throws(()=>extractSource(root,'alias/a.py',1,1));
 await writeFile(path.join(root,'big'),'x'.repeat(4*1024*1024+1));assert.throws(()=>extractSource(root,'big',1,1));
});

test('CLI checks execute through a file alias with and without preserved entry symlinks',async t=>{
 const root=await mkdtemp(path.join(tmpdir(),'axiom-evidence-cli-alias-'));t.after(()=>rm(root,{recursive:true,force:true}));
 const copied=path.join(root,'copied.mjs'),alias=path.join(root,'helper-alias.mjs');await writeFile(copied,await readFile(helper));await symlink(copied,alias);
 await writeFile(path.join(root,'a.py'),'    allow(user)\n    return data\n');const draft=path.join(root,'draft.json');await writeFile(draft,JSON.stringify({findings:[{codeEvidence:[{path:'a.py',startLine:1,endLine:1,code:'    allow(user)\n    return data'}]}]}));
 for(const flags of [[],['--preserve-symlinks-main']])await assert.rejects(exec(process.execPath,[...flags,alias,'check',root,draft]),e=>{assert.equal((e as any).code,1);assert.equal(JSON.parse((e as any).stdout).ok,false);return true});
 const valid=JSON.parse((await exec(process.execPath,[alias,'extract',root,'a.py','1','2'])).stdout);assert.equal(valid.code,'    allow(user)\n    return data');
});
