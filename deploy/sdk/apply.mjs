import {createHash} from 'node:crypto';
import {readFileSync,writeFileSync,renameSync,readdirSync,lstatSync,existsSync,cpSync,mkdirSync,rmSync} from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
const here=path.dirname(fileURLToPath(import.meta.url));
export const digest=bytes=>createHash('sha256').update(bytes).digest('hex');
function inventory(root, prefix='') {
 const files=[];
 for(const entry of readdirSync(path.join(root,prefix),{withFileTypes:true})) {
  if(['node_modules','__pycache__'].includes(entry.name)||entry.name.endsWith('.pyc'))continue;
  const rel=path.posix.join(prefix,entry.name);
  if(entry.isSymbolicLink())throw new Error(`Unexpected symlink: ${rel}`);
  if(entry.isDirectory())files.push(...inventory(root,rel));else if(entry.isFile())files.push(rel);
 }
 return files.sort();
}
export function apply({root=path.resolve(here,'../../code-audit-scanner'),check=false,manifestPath=path.join(here,'manifest.json')}={}) {
 const manifest=JSON.parse(readFileSync(manifestPath,'utf8'));
 if(manifest.runtimeIsolation)return applyIsolated(root,check,manifest, path.dirname(manifestPath));
 const bundle=path.dirname(manifestPath),modules=path.join(root,'node_modules');
 if(manifest.schema!==1)throw new Error('Unsupported SDK manifest schema');
 const patches=new Map(manifest.patches.map(p=>[p.path,p]));
 const writes=[];
 // Validate the whole dependency baseline before touching any file.
 for(const pkg of manifest.packages) {
  if(pkg.platform && pkg.platform!==`${process.platform}-${process.arch}`)continue;
  const dir=path.join(modules,pkg.path),info=JSON.parse(readFileSync(path.join(dir,'package.json'),'utf8'));
  if(info.name!==pkg.name||info.version!==pkg.version)throw new Error(`SDK version drift: ${pkg.path}`);
  const actual=inventory(dir),expected=Object.keys(pkg.files).sort();
  if(JSON.stringify(actual)!==JSON.stringify(expected))throw new Error(`SDK file inventory drift: ${pkg.path}`);
  for(const rel of actual) {
   const name=pkg.path+'/'+rel,target=path.join(dir,rel),hash=digest(readFileSync(target));
   if(hash===pkg.files[rel])continue;
   const patch=patches.get(name);
   if(check||!patch||hash!==patch.before)throw new Error(`SDK content drift: ${name}`);
   const bytes=readFileSync(path.join(bundle,patch.source));
   if(digest(bytes)!==patch.after||patch.after!==pkg.files[rel])throw new Error(`Patch checksum mismatch: ${name}`);
   writes.push({target,bytes,mode:lstatSync(target).mode & 0o777});
  }
 }
 for(const {target,bytes,mode} of writes){const temp=target+'.axiom-tmp';writeFileSync(temp,bytes,{mode,flag:'wx'});renameSync(temp,target);}
 console.log(`SDK ${check?'verified':'ready'}: ${manifest.patches.length} pinned patches, ${writes.length} files updated`);
 return writes.length;
}

// The public package always contains upstream bytes. The old, hash-pinned
// compatibility runtime is an independently imported package, never a mutable
// process-wide switch. Both use the same pinned Codex binary/dependencies.
export function runtimeDescriptors(manifest){
 const pkg=manifest.packages.find(p=>p.name==='@openai/codex-security');
 const codex=manifest.packages.find(p=>p.name==='@openai/codex-sdk').version;
 const native={...pkg.files};
 for(const p of manifest.patches)native[p.path.slice(pkg.path.length+1)]=p.before;
 return Object.fromEntries([['official-v2',native],['legacy-v1',pkg.files]].map(([id,files])=>[id,{
  id,sdkVersion:pkg.version,codexVersion:codex,
  checks:Object.fromEntries(['package.json','dist/api.js','dist/runtime.js',...manifest.patches.map(p=>p.path.slice(pkg.path.length+1))].map(name=>[name,files[name]])),
  fingerprint:digest(JSON.stringify({id,sdkVersion:pkg.version,codexVersion:codex,files})),
 }]));
}
function validatePackage(dir,pkg,files,allowOther){
 const actual=inventory(dir),expected=Object.keys(files).sort();
 if(JSON.stringify(actual)!==JSON.stringify(expected))throw new Error(`SDK file inventory drift: ${pkg.path}`);
 for(const name of actual){
  const hash=digest(readFileSync(path.join(dir,name)));
  if(hash!==files[name]&&hash!==allowOther?.[name])throw new Error(`SDK content drift: ${pkg.path}/${name}`);
 }
}
function applyIsolated(root,check,manifest,bundle){
 const modules=path.join(root,'node_modules'),sdk=manifest.packages.find(p=>p.name==='@openai/codex-security');
 const native=path.join(modules,sdk.path),compat=path.join(modules,'@axiom/codex-security-compat-v1');
 const original={...sdk.files};for(const p of manifest.patches)original[p.path.slice(sdk.path.length+1)]=p.before;
 // Check every source, dependency and replacement before any writes.
 for(const pkg of manifest.packages){
  if(pkg.platform&&pkg.platform!==`${process.platform}-${process.arch}`)continue;
  validatePackage(path.join(modules,pkg.path),pkg,pkg===sdk?original:pkg.files,check?undefined:pkg===sdk?sdk.files:undefined);
 }
 for(const p of manifest.patches){
  if(digest(readFileSync(path.join(bundle,p.source)))!==p.after||digest(readFileSync(path.join(bundle,'upstream',p.path)))!==p.before)throw new Error(`Patch checksum mismatch: ${p.path}`);
 }
 if(existsSync(compat))validatePackage(compat,sdk,sdk.files);
 else if(check)throw new Error('SDK compatibility runtime missing; run the pinned installer');
 const metadata=JSON.stringify({schemaVersion:1,runtimes:runtimeDescriptors(manifest)},null,2)+'\n';
 const record=path.join(modules,'.axiom-sdk-runtimes.json');
 if(check){if(readFileSync(record,'utf8')!==metadata)throw new Error('SDK runtime descriptor drift');return 0;}
 let writes=0;
 if(!existsSync(compat)){
  const staging=compat+'.axiom-tmp';mkdirSync(path.dirname(compat),{recursive:true});
  try{cpSync(native,staging,{recursive:true});for(const p of manifest.patches)cpSync(path.join(bundle,p.source),path.join(staging,p.path.slice(sdk.path.length+1)));validatePackage(staging,sdk,sdk.files);renameSync(staging,compat);writes++;}
  finally{rmSync(staging,{recursive:true,force:true});}
 }
 for(const p of manifest.patches){
  const target=path.join(modules,p.path);if(digest(readFileSync(target))===p.before)continue;
  const temp=target+'.axiom-tmp';writeFileSync(temp,readFileSync(path.join(bundle,'upstream',p.path)),{flag:'wx',mode:lstatSync(target).mode&0o777});renameSync(temp,target);writes++;
 }
 writeFileSync(record,metadata,{mode:0o644});
 console.log(`SDK isolated: upstream official-v2; ${manifest.patches.length} patches confined to legacy-v1; ${writes} files/runtime updated`);
 return writes;
}
if(process.argv[1] && path.resolve(process.argv[1])===fileURLToPath(import.meta.url)) {
 try{const args=process.argv.slice(2),i=args.indexOf('--root');apply({check:args.includes('--check'),...(i>=0?{root:path.resolve(args[i+1])}:{})});}
 catch(e){console.error(e.message);process.exitCode=1;}
}
