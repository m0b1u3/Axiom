import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,mkdir,writeFile,readFile,readdir,rm,chmod,realpath} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
import {spawnSync} from 'node:child_process';

const scanner=fileURLToPath(new URL('../',import.meta.url));
const loader=pathToFileURL(path.join(scanner,'node_modules/tsx/dist/loader.mjs')).href;
const helper=new URL('../src/runtime/sdk-working-directory.ts',import.meta.url).href;
test('SDK bootstrap writes remain outside a read-only installation and source, with per-execution cwd isolation',async()=>{
 const root=await realpath(await mkdtemp(path.join(tmpdir(),'axiom-sdk-cwd-')));
 const install=path.join(root,'installation'),source=path.join(root,'source');
 await mkdir(install);await mkdir(source);await writeFile(path.join(source,'source.py'),'# immutable source\n');await chmod(install,0o555);
 try{
  for(const attempt of [1,2]){
   const output=path.join(root,'attempt-'+attempt);
   const program=`import {enterSDKWorkingDirectory} from ${JSON.stringify(helper)};import {writeFile,mkdir} from 'node:fs/promises';const before=process.cwd();const restore=await enterSDKWorkingDirectory(${JSON.stringify(output)});await writeFile('.codex/sdk-probe','bootstrap');const cwd=process.cwd();restore();console.log(JSON.stringify({before,cwd,after:process.cwd()}));`;
   const child=spawnSync(process.execPath,['--import',loader,'--input-type=module','-e',program],{cwd:install,encoding:'utf8'});
   assert.equal(child.status,0,child.stderr);
   const result=JSON.parse(child.stdout);assert.equal(result.before,install);assert.equal(result.after,install);
   assert.equal(result.cwd,path.join(output,'runtime','sdk-cwd'));
   assert.equal(await readFile(path.join(result.cwd,'.codex/sdk-probe'),'utf8'),'bootstrap');
  }
  assert.deepEqual(await readdir(install),[]);
  assert.deepEqual(await readdir(source),['source.py']);assert.equal(await readFile(path.join(source,'source.py'),'utf8'),'# immutable source\n');
 }finally{await chmod(install,0o700);await rm(root,{recursive:true,force:true});}
});
