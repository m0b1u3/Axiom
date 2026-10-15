import {spawnSync} from 'node:child_process';
import {mkdtempSync, rmSync} from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..');
const scratch=mkdtempSync(path.join(os.tmpdir(),'axiom-contract-check-'));
const env={...process.env,GOTOOLCHAIN:'local',GOCACHE:process.env.GOCACHE??path.join(os.tmpdir(),'axiom-contract-go-build'),
  AXIOM_CONTRACT_CODEC:path.join(scratch,process.platform==='win32'?'contract-codec.exe':'contract-codec'),
  AXIOM_CONTRACT_TEST_OUTPUT:path.join(scratch,'describe-must-not-create-this')};
const tsc=path.join(root,'node_modules/typescript/bin/tsc');
function run(cmd,args,cwd=root) {
  const result=spawnSync(cmd,args,{cwd,env,stdio:'inherit'});
  if(result.error)throw result.error;
  if(result.status!==0)throw new Error(cmd+' failed with exit '+result.status);
}
try {
  run(process.execPath,['tools/generate.mjs','--check']);
  run(process.execPath,[tsc,'-p','ts/tsconfig.json']);
  run(process.execPath,[tsc,'-p','ts/tsconfig.type-tests.json']);
  // Shared fixtures live outside the Go module, so do not reuse test-result caches.
  run('go',['test','-count=1','./...'],path.join(root,'go'));
  run('go',['build','-o',env.AXIOM_CONTRACT_CODEC,'./cmd/contract-codec'],path.join(root,'go'));
  run(process.execPath,['--test','tests/conformance.test.mjs']);
} finally {rmSync(scratch,{recursive:true,force:true});}
