import {lstat,realpath,readdir,readlink,rename,mkdir,mkdtemp} from 'node:fs/promises';
import path from 'node:path';
import {createHash} from 'node:crypto';

export interface QuarantinedScratchLink {originalPath:string;quarantinedAs:string;targetSha256:string;targetDereferenced:false;handling:'moved_link_only'}

/** Preserve proof-created links in private runtime storage, never in the
 * downloadable artifact tree. Canonical SDK outputs and every regular file
 * remain untouched; the publisher still rejects all remaining links. */
export async function quarantineSDKTempLinks(output:string,scanRelative:string):Promise<QuarantinedScratchLink[]>{
 if(!/^raw\/codex-security\/components\/component-[1-9][0-9]*$/.test(scanRelative))throw new Error('SDK 临时产物目录身份无效');
 const owner=await lstat(output);if(!owner.isDirectory()||owner.isSymbolicLink())throw new Error('SDK 执行目录身份无效');
 const root=await realpath(output),records:QuarantinedScratchLink[]=[];
 let current=root;
 for(const part of [...scanRelative.split('/'),'artifacts','tmp']){
  current=path.join(current,part);const info=await lstat(current).catch(error=>{if((error as NodeJS.ErrnoException).code==='ENOENT')return null;throw error});
  if(!info)return records;
  if(!info.isDirectory()||info.isSymbolicLink())throw new Error('SDK 临时产物的父目录含链接或非目录，拒绝处理');
 }
 let destination:string|undefined;
 async function visit(directory:string):Promise<void>{
  for(const entry of await readdir(directory,{withFileTypes:true})){
   const file=path.join(directory,entry.name),info=await lstat(file);
   if(info.isSymbolicLink()){
    if(!destination){
     const runtime=path.join(root,'runtime');let parent=await lstat(runtime).catch(error=>{if((error as NodeJS.ErrnoException).code==='ENOENT')return null;throw error});
     if(!parent){await mkdir(runtime,{mode:0o700});parent=await lstat(runtime)}if(!parent.isDirectory()||parent.isSymbolicLink())throw new Error('SDK 临时证据隔离目录身份无效');
     destination=await mkdtemp(path.join(runtime,'quarantined-sdk-scratch-'));
    }
    const target=await readlink(file,{encoding:'buffer'}),moved=path.join(destination,String(records.length+1)+'.symlink');
    await rename(file,moved);
    records.push({originalPath:path.relative(root,file).replaceAll('\\','/'),quarantinedAs:path.relative(root,moved).replaceAll('\\','/'),targetSha256:createHash('sha256').update(target).digest('hex'),targetDereferenced:false,handling:'moved_link_only'});
   }else if(info.isDirectory())await visit(file);
  }
 }
 await visit(current);return records;
}
