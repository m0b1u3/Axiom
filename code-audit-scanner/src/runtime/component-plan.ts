import {createHash} from 'node:crypto';
import {createReadStream} from 'node:fs';
import {lstat,readdir} from 'node:fs/promises';
import path from 'node:path';
import {validate,type ComponentPlanDocument,type ComponentPlanCoverage} from '@axiom/contracts';

const order=(a:string,b:string)=>Buffer.compare(Buffer.from(a),Buffer.from(b));
export async function frozenTreeHash(root:string,signal?:AbortSignal):Promise<string>{
 const entries:{name:string;size:number;sha:string}[]=[];let total=0;
 async function visit(dir:string){for(const entry of await readdir(dir)){signal?.throwIfAborted();const file=path.join(dir,entry),info=await lstat(file);if(info.isSymbolicLink())throw new Error('组件源码包含链接');if(info.isDirectory()){await visit(file);continue;}if(!info.isFile()||info.size>100<<20||entries.length>=200000||(total+=info.size)>10*2**30)throw new Error('组件源码超过不可变快照限制');const h=createHash('sha256');let size=0;for await(const chunk of createReadStream(file,{signal})){h.update(chunk as Buffer);size+=(chunk as Buffer).length;}if(size!==info.size)throw new Error('组件源码在读取期间变化');entries.push({name:path.relative(root,file).split(path.sep).join('/'),size,sha:h.digest('hex')});}}
 await visit(root);entries.sort((a,b)=>order(a.name,b.name));const h=createHash('sha256');for(const e of entries)h.update(`${e.size}\t${e.name}\t${e.sha}\n`);return h.digest('hex');
}

export async function componentCoverage(root:string,document:ComponentPlanDocument,signal?:AbortSignal):Promise<ComponentPlanCoverage>{
 validate('ComponentPlanDocument',document);const files:string[]=[];
 async function visit(dir:string){for(const entry of await readdir(dir)){signal?.throwIfAborted();const file=path.join(dir,entry),info=await lstat(file);if(entry==='.git'){if(info.isDirectory())continue;throw new Error('源码包含外部 Git worktree');}if(info.isSymbolicLink())throw new Error('源码包含链接');if(info.isDirectory())await visit(file);else{if(!info.isFile()||files.length>=200000)throw new Error('源码超过组件检查限制');files.push(path.relative(root,file).split(path.sep).join('/'));}}}
 await visit(root);files.sort(order);const h=createHash('sha256');for(const f of files)h.update(f+'\0');
 const out:ComponentPlanCoverage={inventoryHash:h.digest('hex'),fileCount:files.length,coveredFileCount:0,uncoveredPaths:[],overlapCount:0,overlaps:[],emptyComponents:[],duplicateNames:[],valid:false};
 for(const c of document.components)for(const p of c.paths){const info=await lstat(path.join(root,p));if(!info.isDirectory()&&!info.isFile())throw new Error('组件路径不是普通源码');}
 const paths:{index:number;path:string}[]=[],owners=new Map<string,number[]>(),names=new Set<string>(),counts=document.components.map(()=>0);
 document.components.forEach((c,index)=>{const name=c.name.trim();if(names.has(name))out.duplicateNames.push(name);names.add(name);for(const p of c.paths){if(p!=='.'&&(p.includes(':')||p.split('/').some(x=>x==='.git')))throw new Error('组件路径不能选择 Git 元数据或盘符');paths.push({index,path:p});owners.set(p,[...(owners.get(p)??[]),index]);}});
 const contains=(a:string,b:string)=>a==='.'||a===b||b.startsWith(a+'/');
 for(let i=0;i<paths.length;i++){signal?.throwIfAborted();const a=paths[i]!;for(let j=i+1;j<paths.length;j++){const b=paths[j]!;if(contains(a.path,b.path)||contains(b.path,a.path)){out.overlapCount++;if(out.overlaps.length<200)out.overlaps.push({leftComponent:a.index,leftPath:a.path,rightComponent:b.index,rightPath:b.path});}}}
 for(const file of files){signal?.throwIfAborted();let matched=false;for(let p=file;;){for(const i of owners.get(p)??[]){counts[i]=(counts[i]??0)+1;matched=true;}if(p==='.')break;const at=p.lastIndexOf('/');p=at<0?'.':p.slice(0,at);}if(matched)out.coveredFileCount++;else out.uncoveredPaths.push(file);}
 counts.forEach((n,i)=>{if(!n)out.emptyComponents.push(i)});out.valid=!!out.fileCount&&out.coveredFileCount===out.fileCount&&!out.overlapCount&&!out.emptyComponents.length&&!out.duplicateNames.length;validate('ComponentPlanCoverage',out);return out;
}
