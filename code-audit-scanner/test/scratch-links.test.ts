import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,mkdir,writeFile,readFile,lstat,readlink,symlink,rm} from 'node:fs/promises';
import path from 'node:path';
import {tmpdir} from 'node:os';
import {createHash} from 'node:crypto';
import {quarantineSDKTempLinks} from '../src/runtime/scratch-links.js';

test('SDK scratch links are moved without following targets and canonical files remain exact',{skip:process.platform==='win32'},async t=>{
 const root=await mkdtemp(path.join(tmpdir(),'axiom-scratch-link-'));t.after(()=>rm(root,{recursive:true,force:true}));
 const relative='raw/codex-security/components/component-1',scan=path.join(root,relative),scratch=path.join(scan,'artifacts/tmp');await mkdir(scratch,{recursive:true});
 const target=path.join(root,'outside-fixture.txt');await writeFile(target,'Fixtures / Mock: never copy this target');await writeFile(path.join(scan,'report.md'),'# Canonical fixture report\n');await writeFile(path.join(scratch,'proof.py'),'# Proof fixture retained\n');
 await symlink(target,path.join(scratch,'escape-link'));const records=await quarantineSDKTempLinks(root,relative);assert.equal(records.length,1);const record=records[0]!;
 assert.equal(record.targetDereferenced,false);assert.equal(record.targetSha256,createHash('sha256').update(target).digest('hex'));assert.equal(await readlink(path.join(root,record.quarantinedAs)),target);assert.equal((await lstat(path.join(root,record.quarantinedAs))).isSymbolicLink(),true);
 await assert.rejects(lstat(path.join(scratch,'escape-link')));assert.equal(await readFile(target,'utf8'),'Fixtures / Mock: never copy this target');assert.equal(await readFile(path.join(scan,'report.md'),'utf8'),'# Canonical fixture report\n');assert.equal(await readFile(path.join(scratch,'proof.py'),'utf8'),'# Proof fixture retained\n');assert.deepEqual(await quarantineSDKTempLinks(root,relative),[]);
 // An unsafe canonical output is left for the unchanged publisher to reject.
 await symlink(target,path.join(scan,'unsafe-canonical-link'));assert.deepEqual(await quarantineSDKTempLinks(root,relative),[]);assert.equal((await lstat(path.join(scan,'unsafe-canonical-link'))).isSymbolicLink(),true);
});

test('scratch containment refuses aliased parents and arbitrary paths',{skip:process.platform==='win32'},async t=>{
 const root=await mkdtemp(path.join(tmpdir(),'axiom-scratch-boundary-'));t.after(()=>rm(root,{recursive:true,force:true}));const relative='raw/codex-security/components/component-1';await mkdir(path.join(root,relative,'artifacts'),{recursive:true});await mkdir(path.join(root,'unrelated'));await symlink(path.join(root,'unrelated'),path.join(root,relative,'artifacts/tmp'));
 await assert.rejects(quarantineSDKTempLinks(root,relative),/父目录/);await assert.rejects(quarantineSDKTempLinks(root,'../outside'),/身份/);
});
