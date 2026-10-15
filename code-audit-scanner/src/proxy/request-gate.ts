import {DatabaseSync} from 'node:sqlite';
import {mkdirSync} from 'node:fs';
import path from 'node:path';
import {randomUUID} from 'node:crypto';
import {setTimeout as sleep} from 'node:timers/promises';

export function retryDelay(value:string|null, attempt:number, now=Date.now()):number {
 const numeric=value===null?NaN:Number(value);
 const header=Number.isFinite(numeric)?numeric*1000:Date.parse(value??'')-now;
 return Math.max(1000,Number.isFinite(header)?header:Math.min(30000,1000*2**attempt));
}
export function quotaExhausted(text:string):boolean {return /free quota exhausted|insufficient_quota|insufficient (?:balance|credit)|quota.{0,20}exhausted|use free tier only/i.test(text);}

/** Shared across scanner processes: controls HTTP requests, not SDK worker count. */
export class RequestGate {
 private db:DatabaseSync;
 constructor(file:string,private limit=2){
  mkdirSync(path.dirname(file),{recursive:true});this.db=new DatabaseSync(file);
  this.db.exec('PRAGMA busy_timeout=1000; CREATE TABLE IF NOT EXISTS request_leases(id TEXT PRIMARY KEY, model TEXT, expires INTEGER); CREATE TABLE IF NOT EXISTS request_cooldowns(model TEXT PRIMARY KEY, until INTEGER);');
 }
 async acquire(model:string,signal:AbortSignal):Promise<()=>void>{
  const id=randomUUID(),deadline=Date.now()+300000;
  for(;;){
   signal.throwIfAborted();if(Date.now()>deadline)throw new Error('model_request_queue_timeout');
   let acquired=false;
   this.db.exec('BEGIN IMMEDIATE');
   try {
    this.db.prepare('DELETE FROM request_leases WHERE expires<?').run(Date.now());
    const count=this.db.prepare('SELECT count(*) AS n FROM request_leases WHERE model=?').get(model) as {n:number};
    const cooldown=this.db.prepare('SELECT until FROM request_cooldowns WHERE model=?').get(model) as {until:number}|undefined;
    if(count.n<this.limit && (cooldown?.until??0)<=Date.now()){
     this.db.prepare('INSERT INTO request_leases VALUES(?,?,?)').run(id,model,Date.now()+90000);acquired=true;
    }
    this.db.exec('COMMIT');
   }catch(e){this.db.exec('ROLLBACK');throw e;}
   if(acquired){
    const heartbeat=setInterval(()=>{try{this.db.prepare('UPDATE request_leases SET expires=? WHERE id=?').run(Date.now()+90000,id)}catch{}},15000);heartbeat.unref();
    let released=false;return ()=>{if(released)return;released=true;clearInterval(heartbeat);this.db.prepare('DELETE FROM request_leases WHERE id=?').run(id);};
   }
   await sleep(100,undefined,{signal});
  }
 }
 cooldown(model:string,delay:number):void{this.db.prepare('INSERT INTO request_cooldowns VALUES(?,?) ON CONFLICT(model) DO UPDATE SET until=max(until,excluded.until)').run(model,Date.now()+delay);}
 close(){this.db.close();}
}
