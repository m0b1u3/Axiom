import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,mkdirSync,writeFileSync,existsSync,rmSync,renameSync} from 'node:fs';
import path from 'node:path';
import {tmpdir} from 'node:os';
import {prepareAuditWorkspace} from '../src/runtime/audit-bootstrap.js';
test('host resolves guidance with scope and restores temporary environment',async()=>{
 const dir=mkdtempSync(path.join(tmpdir(),'axiom-bootstrap-'));const source=path.join(dir,'source');mkdirSync(source);writeFileSync(path.join(source,'SECURITY.md'),'Audit guidance fixture');const before=process.env.TMPDIR;
 const workspace=await prepareAuditWorkspace({taskId:1,provider:'openai',model:'fixture-only',scanMode:'standard',timeoutMinutes:1,sourcePath:source,outputPath:path.join(dir,'attempt')});
 try{assert.match(workspace.prompt,/指导文件已由宿主执行/);assert.match(workspace.prompt,/source-evidence.mjs/);assert.match(workspace.prompt,/检查非零退出时/);assert.equal(existsSync(path.join(dir,'attempt/runtime/preflight/source-evidence.mjs')),true);assert.equal(existsSync(path.join(dir,'attempt/runtime/preflight/security-guidance.md')),true);workspace.ensureTemp();assert.equal(existsSync(process.env.TMPDIR!),true);assert.match(process.env.TMPDIR!,/runtime\/sdk-tmp/);
 // Native SDK archives raw output before creating the initial thread. The
 // bootstrap temp directory must survive even before any callback can run.
 const raw=path.join(dir,'attempt/raw/codex-security');renameSync(raw,raw+'.archived-fixture');
 assert.equal(existsSync(process.env.TMPDIR!),true);workspace.ensureTemp();assert.equal(existsSync(path.join(raw,'artifacts/tmp')),true);}
 finally{workspace.restore();assert.equal(process.env.TMPDIR,before);rmSync(dir,{recursive:true,force:true});}
});

test('a rejected SDK runtime restores temporary environment before propagating the error',async()=>{
 const dir=mkdtempSync(path.join(tmpdir(),'axiom-bootstrap-rejected-'));const source=path.join(dir,'source');mkdirSync(source);
 const names=['TMPDIR','TMP','TEMP'];const before=names.map(name=>process.env[name]);
 try{
  await assert.rejects(prepareAuditWorkspace({taskId:1,provider:'openai',model:'fixture-only',scanMode:'standard',timeoutMinutes:1,sourcePath:source,outputPath:path.join(dir,'attempt'),sdkRuntime:{id:'official-v2',fingerprint:'0'.repeat(64)}}),/基线发生变化/);
  assert.deepEqual(names.map(name=>process.env[name]),before);
 }finally{rmSync(dir,{recursive:true,force:true});}
});
