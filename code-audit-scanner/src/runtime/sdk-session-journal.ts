import {openSync,closeSync,writeSync,fsyncSync,mkdirSync} from 'node:fs';
import path from 'node:path';
import {decode,type ExecutionRef,type JsonObject,type SDKSessionEvent,type SDKSessionSummary} from '@axiom/contracts';
import {redactSecrets} from '../security/secret-redactor.js';

/** All callback types are retained; UI activity filtering never filters evidence.
 * Synchronous bounded writes avoid an unbounded in-memory promise backlog. */
export class SDKSessionJournal {
  private fd:number|null=null;
  private bytes=0;
  private summary:SDKSessionSummary;
  constructor(private execution:ExecutionRef,output:string,private maximumBytes=64*1024*1024) {
    this.summary={schemaVersion:1,execution,receivedCount:0,persistedCount:0,omittedCount:0,state:'complete',reason:null,journal:null};
    try{mkdirSync(path.join(output,'logs'),{recursive:true,mode:0o700});this.fd=openSync(path.join(output,'logs/sdk-session-events.jsonl'),'wx',0o600);}catch{this.fail('会话事件日志无法创建；后续事件计入缺失数量。');}
  }
  record(input:{threadId:string;worker?:number;event:Record<string,unknown>}):void {
    this.summary.receivedCount++;
    try {
      if(this.fd===null||this.summary.state==='incomplete')throw new Error('journal unavailable');
      const item:SDKSessionEvent={schemaVersion:1,execution:this.execution,sequence:this.summary.receivedCount,occurredAt:new Date().toISOString(),threadId:input.threadId,worker:input.worker??null,event:sanitizeSDKData(input) as JsonObject,redacted:true};
      decode('SDKSessionEvent',JSON.stringify(item),this.execution);
      const raw=Buffer.from(JSON.stringify(item)+'\n');
      if(raw.length>8*1024*1024||this.bytes+raw.length>this.maximumBytes){this.fail('会话事件超过单条 8 MiB 或日志容量上限；未保存事件已明确计数。');throw new Error('capacity');}
      let offset=0;while(offset<raw.length){const n=writeSync(this.fd,raw,offset);if(!n)throw new Error('short write');offset+=n;}
      this.bytes+=raw.length;this.summary.persistedCount++;
    }catch{this.summary.omittedCount++;this.fail(this.summary.reason??'会话事件写入或合同校验失败；日志可能不完整。');}
  }
  private fail(reason:string){this.summary.state='incomplete';this.summary.reason=reason;}
  close():SDKSessionSummary {
    if(this.fd!==null){try{fsyncSync(this.fd);}catch{this.fail('会话事件日志无法确认持久化；请保留原始产物核查。');}finally{closeSync(this.fd);this.fd=null;}}
    return structuredClone(this.summary);
  }
}

/** Preserve structure and values, except secrets and private execution roots. */
export function sanitizeSDKData(value:unknown,roots:Record<string,string>={},depth=0):unknown {
  if(depth>64)throw new Error('SDK data nesting exceeds limit');
  if(typeof value==='string'){
    let text=redactSecrets(value);
    for(const [name,secret] of Object.entries(process.env))if(/(?:KEY|TOKEN|PASSWORD|SECRET)$/.test(name)&&secret&&secret.length>=4)text=text.split(secret).join('[REDACTED:credential]');
    for(const [root,label] of Object.entries(roots).sort(([a],[b])=>b.length-a.length))text=text.split(root).join(label);
    return text;
  }
  if(Array.isArray(value))return value.map(v=>sanitizeSDKData(v,roots,depth+1));
  if(value&&typeof value==='object')return Object.fromEntries(Object.entries(value).filter(([,v])=>v!==undefined).map(([k,v])=>[k,/(?:api[_-]?key|access[_-]?token|refresh[_-]?token|authorization|password|secret)$/i.test(k)?'[REDACTED:credential]':sanitizeSDKData(v,roots,depth+1)]));
  return value;
}
