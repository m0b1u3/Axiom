import {appendFileSync,existsSync,lstatSync,mkdirSync,readFileSync,writeFileSync,renameSync} from 'node:fs';
import path from 'node:path';
import {redactSecrets} from '../security/secret-redactor.js';
import type {ScanResult} from '../contract/scan-result.js';

/** Evidence archive, never promoted to validated findings or a resume checkpoint. */
export class AuditCheckpoints {
 private bytes=0;private count=0;private emptyWorkers=new Set<string>();private timer:ReturnType<typeof setInterval>;
 private root:string;private writeWarned=false;
 constructor(private output:string,private warning:(s:string)=>void){
  this.root=path.join(output,'checkpoints');mkdirSync(this.root,{recursive:true,mode:0o700});
  this.timer=setInterval(()=>this.snapshot(),15000);this.timer.unref();
 }
 session(event:{threadId:string;parentThreadId:string|null;event:Record<string,unknown>}):void{
  const payload=event.event['payload'] as Record<string,unknown>|undefined;if(!payload)return;
  if(payload['type']==='task_complete'&&event.parentThreadId){
   if(!payload['last_agent_message']){this.emptyWorkers.add(event.threadId);this.warning(`子任务 ${event.threadId} 未返回审计结论，不能视为完成覆盖`);}
   else this.emptyWorkers.delete(event.threadId);
  }
  if(!['function_call_output','message','task_complete'].includes(String(payload['type'])))return;
  if(payload['type']==='message'&&payload['role']!=='assistant')return;
  const text=redactSecrets(JSON.stringify(payload));
  const entry=JSON.stringify({timestamp:new Date().toISOString(),threadId:event.threadId,parentThreadId:event.parentThreadId,type:payload['type'],truncated:text.length>32768,record:text.slice(0,32768)})+'\n';
  if(this.bytes+Buffer.byteLength(entry)>10*1024*1024)return;
  try{appendFileSync(path.join(this.root,'evidence.jsonl'),entry,{mode:0o600});this.bytes+=Buffer.byteLength(entry);this.count++;}catch{if(!this.writeWarned){this.writeWarned=true;this.warning('中间证据保存失败，请检查扫描目录磁盘空间与权限');}}
 }
 snapshot():void{
  for(const name of ['scan-manifest.json','findings.json','coverage.json']){
   try{
    const src=path.join(this.output,'raw/codex-security',name);if(!existsSync(src))continue;
    const st=lstatSync(src);if(!st.isFile()||st.isSymbolicLink()||st.size>16*1024*1024)continue;
    const content=readFileSync(src,'utf8');JSON.parse(content);
    const dest=path.join(this.root,name);writeFileSync(dest+'.tmp',redactSecrets(content),{mode:0o600});renameSync(dest+'.tmp',dest);
   }catch{/* Keep last complete JSON while the model writes the next draft. */}
  }
  const state={version:1,evidenceRecords:this.count,emptyWorkerThreadIds:[...this.emptyWorkers],updatedAt:new Date().toISOString(),artifactDirectory:'checkpoints',notValidated:true};
  try{writeFileSync(path.join(this.root,'index.json.tmp'),JSON.stringify(state),{mode:0o600});renameSync(path.join(this.root,'index.json.tmp'),path.join(this.root,'index.json'));}catch(e){this.warning('中间证据保存失败，请检查扫描目录磁盘空间与权限');}
 }
 attach(result:ScanResult):void{
  this.snapshot();result.coverage={...result.coverage,axiomCheckpoints:{evidenceRecords:this.count,emptyWorkerThreadIds:[...this.emptyWorkers],artifactDirectory:'checkpoints',notValidated:true}};
  if(this.emptyWorkers.size){result.coverage.completeness='partial';if(result.status==='completed')result.status='partial';}
 }
 close():void{clearInterval(this.timer);this.snapshot();}
}
