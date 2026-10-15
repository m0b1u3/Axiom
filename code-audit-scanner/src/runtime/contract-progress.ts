import {appendFile,mkdir,writeFile,rename} from 'node:fs/promises';
import path from 'node:path';
import {validate,normalizeLegacyErrorCode,type ActivityV2,type ContractError,type CostObservation,type CounterUnit,type ExecutionRef,type ProgressSnapshotV2,type ProgressSource,type ScanEventV2,type ScanPhase,type TerminalStatus,type WorkerObservation,type WorkerRef,type WorkerPoolStatus} from '@axiom/contracts';
import {redactSecrets} from '../security/secret-redactor.js';
import {SCANNER_LAYOUT} from '../contract/workspace.js';

export interface ObserverProgress {phase:string;phaseReportedAt:string|null;reportedAt:string|null;filesTotal:number|null;completed:number|null;total:number|null;unit:string|null}
const safe=(s:string,max=32768)=>{const value=redactSecrets(s).slice(0,max);const last=value.charCodeAt(value.length-1);return last>=0xd800&&last<=0xdbff?value.slice(0,-1):value;};
const count=(v:unknown):number|null=>typeof v==='number'&&Number.isSafeInteger(v)&&v>=0?v:null;
const phases=new Set<ScanPhase>(['preparing','preflight','threat_model','discovery','validation','attack_path','reporting','processing','completed','partial','failed','canceled','timeout']);
const units=new Set<CounterUnit>(['files','checks','threat_surfaces','review_receipts','candidate_findings','validated_findings','report_artifacts','discovery_runs']);
export function canonicalPhase(raw:string):{phase:ScanPhase;rawPhase?:string}{return phases.has(raw as ScanPhase)?{phase:raw as ScanPhase}:{phase:'unknown',rawPhase:safe(raw||'unreported',256)};}
const record=(v:unknown):Record<string,unknown>=>v!==null&&typeof v==='object'&&!Array.isArray(v)?v as Record<string,unknown>:{};

/** Sole writer for an execution's progress.json and events.jsonl. All mutations,
 * revisions and sequences run in the same queue, including workbench observations. */
export class ContractProgressSink {
 readonly progressPath:string;readonly eventsPath:string;
 private snapshot:ProgressSnapshotV2;
 private writes=Promise.resolve();private sequence=0;private terminal=false;
 private observerKey='';private phaseReportedAt=0;private countsReportedAt=0;
 constructor(output:string,execution:ExecutionRef){
  this.progressPath=path.join(output,SCANNER_LAYOUT.progress);this.eventsPath=path.join(output,SCANNER_LAYOUT.events);
  this.snapshot={schemaVersion:2,execution,revision:1,phase:'preparing',source:'scanner',observedAt:new Date().toISOString(),reportedAt:null,files:null,phaseCounts:null,deep:null,workers:[],workerStatus:null,activity:null,cost:null,reconnect:null,error:null};
 }
 async init(){await mkdir(path.dirname(this.progressPath),{recursive:true,mode:0o700});await mkdir(path.dirname(this.eventsPath),{recursive:true,mode:0o700});await writeFile(this.eventsPath,'',{mode:0o600});await this.flush();await this.emit('scan.phase_changed',{phase:'preparing',source:'scanner'},'info','scan started');}
 private enqueue(work:()=>Promise<void>):Promise<void>{const next=this.writes.then(work);this.writes=next.catch(()=>undefined);return next;}
 private update(work:()=>Promise<void>|void):Promise<void>{return this.enqueue(async()=>{if(this.terminal)return;await work();this.snapshot.revision++;this.snapshot.observedAt=new Date().toISOString();await this.flush();});}
 private async flush(){while(Buffer.byteLength(JSON.stringify(this.snapshot))>1024*1024-65536&&this.snapshot.workers.length>0){this.snapshot.workers.shift();this.snapshot.workersTruncated=true;}validate('ProgressSnapshotV2',this.snapshot,this.snapshot.execution);const temp=`${this.progressPath}.${process.pid}.tmp`;await writeFile(temp,JSON.stringify(this.snapshot)+'\n',{mode:0o600});await rename(temp,this.progressPath);}
 private async emit<K extends ScanEventV2['type']>(type:K,data:Extract<ScanEventV2,{type:K}>['data'],level:ScanEventV2['level'],message:string){
  const event={schemaVersion:2,execution:this.snapshot.execution,sequence:++this.sequence,occurredAt:new Date().toISOString(),type,data,message:safe(message),level};
  validate('ScanEventV2',event,this.snapshot.execution);await appendFile(this.eventsPath,JSON.stringify(event)+'\n',{mode:0o600});
 }
 private async phase(raw:string,source:ProgressSource,reportedAt:string|null){const next=canonicalPhase(raw);const changed=this.snapshot.phase!==next.phase||this.snapshot.rawPhase!==next.rawPhase;this.snapshot.phase=next.phase;delete this.snapshot.rawPhase;if(next.rawPhase)this.snapshot.rawPhase=next.rawPhase;this.snapshot.source=source;this.snapshot.reportedAt=reportedAt;if(changed){this.snapshot.phaseCounts=null;this.countsReportedAt=0;}if(changed)await this.emit('scan.phase_changed',{...next,source},'info',`phase=${raw}`);}
 private async countsEvent(){const s=this.snapshot;await this.emit('scan.progress',{phase:s.phase,source:s.source,observedAt:new Date().toISOString(),reportedAt:s.reportedAt,files:s.files,phaseCounts:s.phaseCounts,deep:s.deep,...(s.rawPhase?{rawPhase:s.rawPhase}:{})},'info',`phase=${s.rawPhase??s.phase}`);}
 setPhase(phase:string){return this.update(()=>this.phase(phase,'scanner',null));}
 progress(input:{phase?:string;filesCompleted?:number;filesTotal?:number}){const value=structuredClone(input),now=new Date().toISOString();return this.update(async()=>{if(value.phase){await this.phase(value.phase,'engine_callback',now);this.phaseReportedAt=Date.parse(now);}if(value.filesCompleted!==undefined||value.filesTotal!==undefined){this.snapshot.files={completed:count(value.filesCompleted),total:count(value.filesTotal),unit:'files',basis:'reported'};}await this.countsEvent();});}
 observation(input:ObserverProgress){const value=structuredClone(input),key=JSON.stringify(value);return this.enqueue(async()=>{
  if(this.terminal||key===this.observerKey)return;
  this.observerKey=key;const phaseAt=Date.parse(value.phaseReportedAt??''),countsAt=Date.parse(value.reportedAt??'');let changed=false;
  if(Number.isFinite(phaseAt)&&phaseAt>=this.phaseReportedAt){await this.phase(value.phase,'engine_observer',value.reportedAt);this.phaseReportedAt=phaseAt;changed=true;}
  if(Number.isFinite(countsAt)&&countsAt>=this.countsReportedAt&&this.snapshot.phase===canonicalPhase(value.phase).phase&&this.snapshot.rawPhase===canonicalPhase(value.phase).rawPhase){
   // Scope totals are observed in the workbench; a missing completion is never 0.
   if(this.snapshot.files===null||this.snapshot.files.basis==='observed')this.snapshot.files={completed:null,total:count(value.filesTotal),unit:'files',basis:'observed'};
   const unit=units.has(value.unit as CounterUnit)?value.unit as CounterUnit:'unknown';
   this.snapshot.phaseCounts={completed:count(value.completed),total:count(value.total),unit,basis:'observed'};
   this.snapshot.reportedAt=value.reportedAt;this.countsReportedAt=countsAt;changed=true;
   if(unit==='unknown'&&value.unit)this.snapshot.extensions={...this.snapshot.extensions,rawProgressUnit:safe(value.unit,256)};
  }
  if(changed){this.snapshot.source='engine_observer';await this.countsEvent();this.snapshot.revision++;this.snapshot.observedAt=new Date().toISOString();await this.flush();}
 });}
 deepProgress(input:{completed:number;active:number;maximum:number}){const v=structuredClone(input);return this.update(async()=>{this.snapshot.deep={completed:count(v.completed),active:count(v.active),maximum:count(v.maximum)};await this.countsEvent();});}
 workerStatus(input:object){const v=record(structuredClone(input));return this.update(async()=>{
  let status:WorkerPoolStatus;
  const observedAt=new Date().toISOString();
  if(v.kind==='preflight')status={kind:'preflight',delegation:['available','unavailable'].includes(String(v.delegation))?v.delegation as 'available'|'unavailable':'unknown',configuredSlots:count(v.configuredSlots),observedAt};
  else if(v.kind==='dispatch'&&count(v.planned)!==null&&count(v.started)!==null)status={kind:'dispatch',phase:safe(String(v.phase??'unknown'),256),planned:count(v.planned)!,started:count(v.started)!,observedAt};
  else return;
  if(status.kind==='dispatch'&&status.started!>status.planned!)return;
  this.snapshot.workerStatus=status;await this.emit('scan.worker_status',status,'info','worker status updated');
 });}
 private findWorker(worker:WorkerRef){return worker.threadId?this.snapshot.workers.find(w=>w.worker.threadId===worker.threadId):this.snapshot.workers.filter(w=>w.worker.index===worker.index).at(-1);}
 private putWorker(worker:WorkerRef,status:WorkerObservation['status'],activity?:ActivityV2,error?:ContractError){
  const previous=this.findWorker(worker)??this.snapshot.workers.find(w=>!w.worker.threadId&&worker.index!==undefined&&w.worker.index===worker.index);
  const merged:WorkerObservation={...previous,worker:{...previous?.worker,...worker},status,observedAt:new Date().toISOString(),source:'engine_callback',...(activity?{activity:activitySummary(activity)}:{}),...(error?{error:{...error,message:safe(error.message,512)}}:{})};
  const match=this.snapshot.workers.findIndex(w=>w===previous||(!w.worker.threadId&&worker.index!==undefined&&w.worker.index===worker.index));
  if(match>=0)this.snapshot.workers.splice(match,1);
  this.snapshot.workers.push(merged);if(this.snapshot.workers.length>256){this.snapshot.workers.shift();this.snapshot.workersTruncated=true;}return merged;
 }
 activity(input:ActivityV2){const v=structuredClone(input);v.description=safe(v.description);if(v.description!==input.description)v.truncated=true;if(v.paths){v.paths=v.paths.slice(0,256).map(p=>safe(p,4096));while(Buffer.byteLength(JSON.stringify(v))>1024*1024&&v.paths.length){v.paths.pop();}if(v.paths.length!==input.paths?.length){v.omittedPaths=(input.paths?.length??0)-v.paths.length;v.truncated=true;}}return this.update(async()=>{this.snapshot.activity=activitySummary(v);if(v.worker){const previous=this.findWorker(v.worker);await this.emit('scan.worker',this.putWorker(v.worker,previous?.status??'unknown',v),v.status==='failed'?'error':'info',v.description);}await this.emit('scan.activity',v,v.status==='failed'?'error':'info',v.description);});}
 cost(input:number|Record<string,unknown>){const v=typeof input==='number'?{estimatedUsd:input}:structuredClone(input);return this.update(async()=>{const cost:CostObservation={estimatedUsd:typeof v.estimatedUsd==='number'&&Number.isFinite(v.estimatedUsd)&&v.estimatedUsd>=0?v.estimatedUsd:null,inputTokens:count(v.inputTokens),outputTokens:count(v.outputTokens),cachedInputTokens:count(v.cachedInputTokens),cacheWriteInputTokens:count(v.cacheWriteInputTokens),currency:'USD',isEstimate:true,observedAt:new Date().toISOString(),source:'engine_callback',...(typeof v.model==='string'&&v.model?{reportedModel:safe(v.model,512)}:{})};this.snapshot.cost=cost;await this.emit('scan.cost',cost,'info','cost estimate updated');});}
 reconnect(attempt:number,maximum:number,details?:{reason:string;retryAfterSeconds?:number}){return this.update(async()=>{const reconnect={attempt,maximum,reason:safe(details?.reason??'unknown',256),...(details?.retryAfterSeconds!==undefined?{retryAfterSeconds:details.retryAfterSeconds}:{})};this.snapshot.reconnect=reconnect;this.snapshot.reconnectAt=new Date().toISOString();await this.emit('scan.reconnect',reconnect,'warn','SDK connection retry');});}
 warning(message:string,code:ContractError['code']='unknown',origin:ContractError['origin']='engine'){return this.update(()=>this.emit('scan.warning',{code,origin,message:safe(message)||'Unreported observer warning'},'warn',message));}
 error(error:ContractError){const v={...error,message:safe(error.message)||'Unreported execution error'};return this.update(async()=>{this.snapshot.error=v;await this.emit('scan.error',v,'error',v.message);});}
 session(input:{threadId:string;parentThreadId:string|null;worker?:number;event:Record<string,unknown>}){
  const v=structuredClone(input),payload=record(v.event.payload);const lifecycle=v.event.type==='event_msg'&&(payload.type==='task_started'||payload.type==='task_complete');const text=reasoningText(v.event);if(!lifecycle&&!text)return Promise.resolve();const worker:WorkerRef={threadId:v.threadId,parentThreadId:v.parentThreadId,...(v.worker!==undefined?{index:v.worker}:{})};
  return this.update(async()=>{
   // Lifecycle follows typed session fields. A completed turn is waiting for
   // another turn, not proof that an independent scan finished successfully.
   if(lifecycle){
    const rawError=record(payload.error);const failure=payload.type==='task_complete'&&payload.error!==undefined&&payload.error!==null;
    const error:ContractError|undefined=failure?{code:normalizeLegacyErrorCode(String(rawError.code??'unknown')).code,origin:'engine',message:safe(String(rawError.message??payload.error??'Worker error')),retryable:false}:undefined;
    const observation=this.putWorker(worker,failure?'failed':payload.type==='task_started'?'running':'waiting',undefined,error);
    await this.emit('scan.worker',observation,failure?'error':'info',failure?error!.message:'execution turn state updated');
    if(error){this.snapshot.error=error;await this.emit('scan.error',error,'error',error.message);}
   }
   if(text){const activity:ActivityV2={kind:'reasoning',status:'completed',description:safe(text),worker};this.snapshot.activity=activitySummary(activity);this.putWorker(worker,this.snapshot.workers.find(w=>w.worker.threadId===worker.threadId)?.status??'unknown',activity);await this.emit('scan.activity',activity,'info',activity.description);}
  });
 }
 async finish(status:TerminalStatus,error?:{code:string;message:string}){await this.enqueue(async()=>{if(this.terminal)return;await this.phase(status,'scanner',null);let fault:ContractError|undefined;if(error){fault={code:normalizeLegacyErrorCode(error.code).code,origin:'engine',message:safe(error.message)||'Unreported scan error',retryable:false};this.snapshot.error=fault;await this.emit('scan.error',fault,'error',fault.message);}this.snapshot.revision++;this.snapshot.observedAt=new Date().toISOString();await this.flush();await this.emit('scan.finished',{status,...(fault?{error:fault}:{})},status==='failed'||status==='timeout'?'error':'info',`scan ${status}`);this.terminal=true;});}
 notice(message:string){return this.enqueue(()=>this.emit('scan.warning',{code:'unknown',origin:'scanner',message:safe(message)||'Unreported progress persistence error'},'warn',message));}
 async drain(){await this.writes;}
}
function reasoningText(event:Record<string,unknown>):string|null{const p=record(event.payload);if(!((event.type==='response_item'&&p.type==='reasoning')||(event.type==='event_msg'&&typeof p.type==='string'&&p.type.includes('reason'))))return null;const value=p.text??p.delta??p.summary??p.content;if(typeof value==='string')return value;if(Array.isArray(value))return value.map(v=>record(v).text).filter(v=>typeof v==='string').join('\n')||null;return null;}

function activitySummary(activity:ActivityV2):ActivityV2{const {paths,...rest}=activity;const description=safe(activity.description,512);return {...rest,description,...(description!==activity.description||paths?.length?{truncated:true}:{}),...(paths?.length?{omittedPaths:paths.length}:{})};}
