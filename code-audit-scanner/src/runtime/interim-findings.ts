import {createHash} from 'node:crypto';
import {existsSync,lstatSync,mkdirSync,readFileSync,readdirSync,renameSync,writeFileSync} from 'node:fs';
import path from 'node:path';
import {decode,validate,type ExecutionRef,type InterimCandidate,type InterimDocument,type InterimDocumentRef,type InterimObservation,type InterimSnapshot} from '@axiom/contracts';
import {sanitizeSDKData} from './sdk-session-journal.js';

const MAX_SOURCE=2*1024*1024,MAX_INDEX=8*1024*1024-65536;
const hash=(value:string|Buffer)=>createHash('sha256').update(value).digest('hex');
const record=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v);
const text=(v:unknown,max:number):string|null=>typeof v==='string'&&v.trim()?v.slice(0,max):null;
function cleanString(value:string):string {
 return String(sanitizeSDKData(value)).replace(/-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----/g,'[REDACTED:private_key]').replace(/(\b(?:password|passwd|api[_-]?key|access[_-]?token|refresh[_-]?token|secret|authorization)["']?\s*[:=]\s*)(["'])([^\r\n]*?)\2/gi,'$1"[REDACTED:credential]"');
}
function clean(value:unknown):unknown {
 if(typeof value==='string')return cleanString(value);
 if(Array.isArray(value))return value.map(clean);
 if(record(value))return Object.fromEntries(Object.entries(sanitizeSDKData(value) as Record<string,unknown>).map(([k,v])=>[k,clean(v)]));
 return value;
}
function regular(output:string,relative:string):boolean {
 if(path.isAbsolute(relative)||relative.split(/[\\/]/).some(v=>v==='..'||v==='.')||relative.includes('\\'))return false;
 let at=output;for(const part of relative.split('/')){at=path.join(at,part);if(lstatSync(at).isSymbolicLink())return false;}
 return lstatSync(at).isFile();
}
function atomic(file:string,value:string){writeFileSync(file+'.tmp',value,{mode:0o600});renameSync(file+'.tmp',file);}
export function extractInterimCandidates(value:unknown,sourcePath:string,artifact:string):InterimCandidate[]{
 const root=record(value)?value:{};const array=Array.isArray(root.candidates)?root.candidates:Array.isArray(root.findings)?root.findings:record(root.findings)&&Array.isArray(root.findings.findings)?root.findings.findings:[];
 const base=Array.isArray(root.candidates)?'/candidates':Array.isArray(root.findings)?'/findings':'/findings/findings';
 return array.flatMap((raw,index)=>{
  if(!record(raw)||typeof raw.title!=='string'||!raw.title.trim())return [];
  const pointer=base+'/'+index,e=Array.isArray(raw.evidence)?raw.evidence:Array.isArray(raw.codeEvidence)?raw.codeEvidence:[];
  const all=e.map(v=>typeof v==='string'?v:JSON.stringify(v));
  const summary=raw.summary??raw.description??raw.impact;
  const severity=record(raw.severity)?raw.severity.level:raw.severity;
  return [{id:hash(sourcePath+'\0'+pointer),artifact,pointer,title:raw.title.slice(0,1024),summary:text(typeof summary==='string'?summary:summary?JSON.stringify(summary):null,8192),claimId:text(raw.id??raw.findingId??raw.ruleId,512),reportedStatus:text(raw.status,512),reportedSeverity:text(severity,512),evidence:all.slice(0,64).map(v=>v.slice(0,4096)),omittedEvidence:Math.max(0,all.length-64),truncated:raw.title.length>1024||all.some(v=>v.length>4096)||(typeof summary==='string'&&summary.length>8192),assessment:'unverified' as const}];
 });
}

/** Only the Scanner interprets SDK files. Append-only sanitized objects and an
 * atomic portable index survive hard termination; they never finalize a scan. */
export class InterimFindings {
 private value:InterimSnapshot;private timer:ReturnType<typeof setInterval>|undefined;
 private root:string;private issues=new Set<string>();private persistentIssues=new Set<string>();private dirty=false;private warned=false;
 constructor(private output:string,private execution:ExecutionRef,private sourceContentHash:string,private notify:(value:InterimObservation)=>void=()=>{},private warning:(message:string)=>void=()=>{}){
  if(!/^[a-f0-9]{64}$/.test(sourceContentHash))throw new Error('中间发现需要冻结源码身份');
  this.root=path.join(output,'checkpoints/interim');
  for(const rel of ['checkpoints','checkpoints/interim','checkpoints/interim/objects']){const at=path.join(output,rel);if(existsSync(at)){if(lstatSync(at).isSymbolicLink()||!lstatSync(at).isDirectory())throw new Error('中间证据目录边界无效');}else mkdirSync(at,{mode:0o700});}
  this.value={schemaVersion:1,execution,sourceContentHash,sdkVersion:'0.5.0',revision:1,observedAt:new Date().toISOString(),state:'observed',documents:[],candidates:[],issues:[]};
  const index=path.join(this.root,'index.json');
  if(existsSync(index)){
   if(!regular(output,'checkpoints/interim/index.json')||lstatSync(index).size>MAX_INDEX)throw new Error('中间发现索引边界无效');
   this.value=decode('InterimSnapshot',readFileSync(index,'utf8'),execution);
   if(this.value.sourceContentHash!==sourceContentHash)throw new Error('中间发现源码身份不匹配');
  }else{atomic(index,JSON.stringify(this.value)+'\n');}
 }
 start(){this.scan();this.timer=setInterval(()=>this.scan(),2000);this.timer.unref();return this;}
 private issue(message:string){this.issues.add(message);}
 private save(sourcePath:string,raw:Buffer,kind:InterimDocument['kind'],modified:string|null,threadId:string|null=null,truncated=false){
  if(raw.length>MAX_SOURCE){this.issue('部分中间证据超过 2 MiB 展示边界；原始文件保留，未将其当作无发现。');return;}
  const sourceSha256=hash(raw);
  if(this.value.documents.filter(d=>d.sourcePath===sourcePath).at(-1)?.sourceSha256===sourceSha256)return;
  if(this.value.documents.length>=2048){this.issue('中间证据版本超过本次索引展示容量；原始产物保持保留。');return;}
  let parsed:unknown,content:string;
  try{parsed=JSON.parse(new TextDecoder('utf-8',{fatal:true}).decode(raw));parsed=clean(parsed);content=JSON.stringify(parsed,null,2);}catch(e){if(sourcePath.endsWith('.json')){this.issue('部分 JSON 检查点尚未写完或格式无效，保留最近有效版本。');return;}content=cleanString(raw.toString('utf8'));}
  // JSON inside a complete fenced worker conclusion may contain candidates.
  if(parsed===undefined){const m=/^\s*```(?:json)?\s*\n([\s\S]*)\n```\s*$/.exec(content);if(m)try{parsed=JSON.parse(m[1]!)}catch{}}
  const observedAt=new Date(Math.max(Date.now(),(Date.parse(this.value.documents.at(-1)?.observedAt??'')||0)+1)).toISOString(),label=(record(parsed)?text(parsed.checkpoint??parsed.phase,1024):null)??path.posix.basename(sourcePath);
  const doc:InterimDocument={schemaVersion:1,execution:this.execution,sourceContentHash:this.sourceContentHash,sdkVersion:'0.5.0',sourcePath,sourceSha256,observedAt,sourceModifiedAt:modified,kind,label,threadId,truncated,content,redacted:true,assessment:'unverified'};
  validate('InterimDocument',doc,this.execution);const bytes=JSON.stringify(doc)+'\n',digest=hash(bytes),relativePath='checkpoints/interim/objects/'+digest+'.json';
  const candidates=extractInterimCandidates(parsed,sourcePath,digest);
  const reference:InterimDocumentRef={artifact:{kind:'engine_raw',relativePath,sizeBytes:Buffer.byteLength(bytes),sha256:digest},sourcePath,sourceSha256,observedAt,sourceModifiedAt:modified,kind,label,threadId,truncated,candidateCount:candidates.length};
  const latestIds=new Set(this.value.documents.filter(d=>d.sourcePath===sourcePath).map(d=>d.artifact.sha256));
  const nextCandidates=[...this.value.candidates.filter(c=>!latestIds.has(c.artifact)),...candidates];
  if(nextCandidates.length>10000){this.issue('候选记录超过本次展示容量，原始检查点仍保留；未自动截断为完整扫描。');return;}
  const next={...this.value,documents:[...this.value.documents,reference],candidates:nextCandidates};
  if(Buffer.byteLength(JSON.stringify(next))>MAX_INDEX){this.issue('中间发现索引超过展示容量，最近有效版本与原始检查点仍保留。');return;}
  const file=path.join(this.output,relativePath);
  if(existsSync(file)){if(!regular(this.output,relativePath)||hash(readFileSync(file))!==digest)throw new Error('中间证据哈希不匹配');}
  else writeFileSync(file,bytes,{flag:'wx',mode:0o600});
  this.value=next;this.dirty=true;
 }
 scan(){
  this.issues=new Set(this.persistentIssues);
  try{
   const files:string[]=[];
   const visit=(relative:string,depth=0)=>{if(depth>8){this.issue('检查点目录嵌套超过展示边界，原始目录保留。');return;}const dir=path.join(this.output,relative);if(!existsSync(dir))return;if(relative.split('/').some((_,i,parts)=>lstatSync(path.join(this.output,...parts.slice(0,i+1))).isSymbolicLink())){this.issue('检查点含符号链接，已拒绝跟随。');return;}
    for(const e of readdirSync(dir,{withFileTypes:true}).sort((a,b)=>a.name.localeCompare(b.name))){const next=relative+'/'+e.name;if(e.isSymbolicLink()){this.issue('检查点含符号链接，已拒绝跟随。');continue;}if(e.isDirectory())visit(next,depth+1);else if(e.isFile()&&/\.(json|md|txt)$/.test(e.name))files.push(next);if(files.length>2048)break;}};
   visit('raw/codex-security/artifacts/checkpoints');
   for(const relative of ['raw/codex-security/findings.json',...files.slice(0,2048)]){
    if(!existsSync(path.join(this.output,relative)))continue;
    if(!regular(this.output,relative)){this.issue('中间文件边界无效，已拒绝读取。');continue;}
    const file=path.join(this.output,relative),st=lstatSync(file);
    if(st.size>MAX_SOURCE){this.issue('部分中间证据超过 2 MiB 展示边界；原始文件保留，未将其当作无发现。');continue;}
    try{this.save(relative,readFileSync(file),relative.endsWith('/findings.json')?'sdk_draft':'checkpoint',st.mtime.toISOString());}catch{this.issue('部分中间证据暂无法投影，最近有效快照与原始产物保留。');this.warningOnce();}
   }
   if(files.length>2048)this.issue('检查点文件超过展示容量；原始目录保留。');
   this.flush();
  }catch{this.warningOnce();}
 }
 session(input:{threadId:string;parentThreadId:string|null;event:Record<string,unknown>}){
  const p=input.event.payload;if(!record(p)||!input.parentThreadId||p.type!=='task_complete'||typeof p.last_agent_message!=='string'||!p.last_agent_message.trim())return;
  if(!/^[A-Za-z0-9._:-]{1,128}$/.test(input.threadId))return;
  try{this.save('sessions/'+input.threadId+'/conclusion.txt',Buffer.from(p.last_agent_message),'worker_message',null,input.threadId);this.flush();}catch{this.warningOnce();}
 }
 importSavedConclusions(){
  // Read-only compatibility for the retired second session journal. New runs
  // consume native checkpoints and SDKSessionJournal callbacks instead.
  const rel='checkpoints/evidence.jsonl',file=path.join(this.output,rel);
  if(!existsSync(file))return;
  if(!regular(this.output,rel)||lstatSync(file).size>10*1024*1024){this.issue('历史会话证据超过读取边界或身份不明；原始会话仍保留。');this.flush();return;}
  for(const line of readFileSync(file,'utf8').split('\n')){try{const v=JSON.parse(line),p=JSON.parse(v.record);if(v.truncated){if(v.type==='task_complete')this.persistentIssues.add('历史 Worker 结论含截断记录，不能据此确认完整发现集合。');continue;}this.session({threadId:v.threadId,parentThreadId:v.parentThreadId,event:{payload:p}});}catch{if(line.trim())this.persistentIssues.add('历史会话证据含未完成记录，保留完整行，完整性未知。');}}
  for(const issue of this.persistentIssues)this.issues.add(issue);this.flush();
 }
 private flush(){
  const issues=[...this.issues].slice(0,64),state=issues.length?'incomplete':'observed';
  if(!this.dirty&&JSON.stringify(issues)===JSON.stringify(this.value.issues)&&state===this.value.state)return;
  this.value={...this.value,issues,state,revision:this.value.revision+1,observedAt:new Date().toISOString()};
  validate('InterimSnapshot',this.value,this.execution);atomic(path.join(this.root,'index.json'),JSON.stringify(this.value)+'\n');this.dirty=false;
  this.notify({schemaVersion:1,execution:this.execution,sourceContentHash:this.sourceContentHash,sdkVersion:'0.5.0',revision:this.value.revision,observedAt:this.value.observedAt,state,documentCount:this.value.documents.length,candidateRecords:this.value.candidates.length});
 }
 private warningOnce(){if(!this.warned){this.warned=true;this.warning('中间证据投影保存失败，正式扫描继续；请保留原始产物核查。');}}
 close(){clearInterval(this.timer);this.scan();}
 snapshot(){return structuredClone(this.value);}
}

/** Maintenance-only backfill, no SDK import, credentials or model invocation. */
export async function exportSavedInterim(inputFile:string){
 const input=JSON.parse(readFileSync(inputFile,'utf8')) as {output:string;execution:ExecutionRef;sourceContentHash:string};
 validate('ExecutionRef',input.execution);if(!path.isAbsolute(input.output))throw new Error('Backfill output must be absolute');
 const exporter=new InterimFindings(input.output,input.execution,input.sourceContentHash);exporter.scan();exporter.importSavedConclusions();exporter.close();const v=exporter.snapshot();
 process.stdout.write(JSON.stringify({execution:v.execution,revision:v.revision,documents:v.documents.length,candidateRecords:v.candidates.length,state:v.state,modelCalls:0})+'\n');
}
