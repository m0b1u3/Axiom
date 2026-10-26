import {DatabaseSync} from 'node:sqlite';
import {mkdirSync} from 'node:fs';
import path from 'node:path';
import {setTimeout as sleep} from 'node:timers/promises';

export function retryDelay(value:string|null, attempt:number, now=Date.now()):number {
 const numeric=value===null?NaN:Number(value);
 const header=Number.isFinite(numeric)?numeric*1000:Date.parse(value??'')-now;
 return Math.max(1000,Number.isFinite(header)?header:Math.min(30000,1000*2**attempt));
}
export function quotaExhausted(text:string):boolean {return /free quota exhausted|insufficient_quota|insufficient (?:balance|credit)|quota.{0,20}exhausted|use free tier only/i.test(text);}

/** Share provider-reported cooldowns across Scanner processes. Concurrency and
 * execution lifetime belong to the SDK and the user's task configuration. */
export class RequestGate {
 private db:DatabaseSync;
 constructor(file:string){
  mkdirSync(path.dirname(file),{recursive:true});this.db=new DatabaseSync(file);
  // Earlier versions also stored request_leases here. Leave that historical
  // table untouched; its rows no longer impose a platform concurrency quota.
  this.db.exec('PRAGMA busy_timeout=1000; CREATE TABLE IF NOT EXISTS request_cooldowns(model TEXT PRIMARY KEY, until INTEGER);');
 }
 async acquire(model:string,signal:AbortSignal):Promise<()=>void>{
  for(;;){
   signal.throwIfAborted();
   const cooldown=this.db.prepare('SELECT until FROM request_cooldowns WHERE model=?').get(model) as {until:number}|undefined;
   const remaining=(cooldown?.until??0)-Date.now();
   if(remaining<=0)return ()=>{};
   // No additional queue timeout: the originating request and task deadline
   // cancel this wait. Respect the actual Retry-After across all instances.
   await sleep(Math.min(remaining,1000),undefined,{signal});
  }
 }
 cooldown(model:string,delay:number):void{this.db.prepare('INSERT INTO request_cooldowns VALUES(?,?) ON CONFLICT(model) DO UPDATE SET until=max(until,excluded.until)').run(model,Date.now()+delay);}
 close(){this.db.close();}
}
