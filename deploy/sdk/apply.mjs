import {createHash} from 'node:crypto';
import {readFileSync,writeFileSync,renameSync,readdirSync,lstatSync,existsSync} from 'node:fs';
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
if(process.argv[1] && path.resolve(process.argv[1])===fileURLToPath(import.meta.url)) {
 try{const args=process.argv.slice(2),i=args.indexOf('--root');apply({check:args.includes('--check'),...(i>=0?{root:path.resolve(args[i+1])}:{})});}
 catch(e){console.error(e.message);process.exitCode=1;}
}
