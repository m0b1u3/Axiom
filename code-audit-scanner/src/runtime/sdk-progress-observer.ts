import {DatabaseSync} from 'node:sqlite';
import {existsSync} from 'node:fs';
import path from 'node:path';
import type {ObserverProgress} from './contract-progress.js';

// SDK storage is private to Scanner. This adapter never writes another projection.
export function readSdkProgress(output:string):ObserverProgress|null {
 const dbPath=path.join(output,'runtime/sdk/workbench.sqlite3');if(!existsSync(dbPath))return null;
 const db=new DatabaseSync(dbPath,{readOnly:true});
 try{
  const row=db.prepare(`SELECT s.phase,s.updated_at AS phase_updated_at,p.scope_file_count,
   p.phase_items_total,p.phase_items_completed,p.phase_progress_unit,p.updated_at
   FROM scans s JOIN scan_progress p ON s.id=p.scan_id WHERE s.scan_dir=? AND s.status='running'
   ORDER BY s.updated_at DESC LIMIT 1`).get(path.join(output,'raw/codex-security')) as Record<string,unknown>|undefined;
  if(!row)return null;
  const count=(v:unknown)=>typeof v==='number'&&Number.isSafeInteger(v)&&v>=0?v:null;
  const time=(v:unknown)=>typeof v==='string'&&Number.isFinite(Date.parse(v))?new Date(v).toISOString():null;
  return {phase:typeof row.phase==='string'?row.phase:'unreported',phaseReportedAt:time(row.phase_updated_at),filesTotal:count(row.scope_file_count),completed:count(row.phase_items_completed),total:count(row.phase_items_total),unit:typeof row.phase_progress_unit==='string'?row.phase_progress_unit:null,reportedAt:time(row.updated_at)};
 }finally{db.close();}
}
export function observeSdkProgress(output:string,accept:(progress:ObserverProgress)=>Promise<void>,warning:(message:string)=>void):()=>void{
 let warned=false,stopped=false;
 const tick=()=>{try{const value=readSdkProgress(output);if(value)void accept(value).then(()=>{warned=false;}).catch(fail);}catch{fail();}};
 function fail(){if(!stopped&&!warned){warned=true;warning('SDK 工作台进度暂不可用，继续显示最近已观测状态');}}
 tick();const timer=setInterval(tick,3000);timer.unref();return ()=>{stopped=true;clearInterval(timer);};
}
