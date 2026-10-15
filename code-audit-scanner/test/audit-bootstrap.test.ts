import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,mkdirSync,writeFileSync,existsSync,rmSync} from 'node:fs';
import path from 'node:path';
import {tmpdir} from 'node:os';
import {prepareAuditWorkspace} from '../src/runtime/audit-bootstrap.js';
test('host resolves guidance with scope and restores temporary environment',async()=>{
 const dir=mkdtempSync(path.join(tmpdir(),'axiom-bootstrap-'));const source=path.join(dir,'source');mkdirSync(source);writeFileSync(path.join(source,'SECURITY.md'),'Audit guidance fixture');const before=process.env.TMPDIR;
 const workspace=await prepareAuditWorkspace({sourcePath:source,outputPath:path.join(dir,'attempt')} as any);
 try{assert.match(workspace.prompt,/预检已由宿主执行/);assert.equal(existsSync(path.join(dir,'attempt/runtime/preflight/security-guidance.md')),true);workspace.ensureTemp();assert.equal(existsSync(process.env.TMPDIR!),true);assert.match(process.env.TMPDIR!,/raw\/codex-security\/artifacts\/tmp/);}
 finally{workspace.restore();assert.equal(process.env.TMPDIR,before);rmSync(dir,{recursive:true,force:true});}
});
