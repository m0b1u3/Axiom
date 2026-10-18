import type {ExecutionRef, JsonObject, SDKDiagnostic, SDKDiagnostics} from '@axiom/contracts';
import {validate} from '@axiom/contracts';
import {appendFile,mkdir} from 'node:fs/promises';
import path from 'node:path';
import {redactSecrets} from '../security/secret-redactor.js';

function safe(value:string,maximum:number):string {
  let clean=redactSecrets(value);
  for(const [name,secret] of Object.entries(process.env)){if(/(?:KEY|TOKEN|PASSWORD|SECRET)$/.test(name)&&secret&&secret.length>=4)clean=clean.split(secret).join('[REDACTED:credential]');}
  const text=clean.slice(0,maximum);
  const last=text.charCodeAt(text.length-1);
  return last>=0xd800&&last<=0xdbff?text.slice(0,-1):text;
}

/** A bounded snapshot supplements the complete, sequenced JSONL diagnostic events. */
export class SDKDiagnosticTracker {
  private state:SDKDiagnostics;
  private writes=Promise.resolve();
  constructor(execution:ExecutionRef,private output?:string) {
    this.state={schemaVersion:1,execution,warningCount:0,observerErrorCount:0,omittedCount:0,entries:[]};
  }
  warning(message:string,details?:{kind?:string}):SDKDiagnostic {
    const kind=typeof details?.kind==='string'?details.kind:'unknown';
    return this.add({source:'sdk_warning',kind:safe(kind,128),observer:null,message:safe(message,2048)||'SDK 未提供警告说明',error:null,details:typeof details?.kind==='string'?{kind:safe(details.kind,128)}:{},truncated:message.length>2048||kind.length>128});
  }
  observer(name:string,error:unknown):SDKDiagnostic {
    const value=error&&typeof error==='object'?error as Record<string,unknown>:{};
    const message=typeof value.message==='string'?value.message:typeof error==='string'?error:'SDK 未提供观察器错误说明';
    const errorName=typeof value.name==='string'?value.name:'Error';
    const code=typeof value.code==='string'?value.code:null;
    return this.add({source:'observer_error',kind:'observer_failed',observer:safe(name,128),message:safe(message,2048)||'SDK 未提供观察器错误说明',error:{name:safe(errorName,256),code:code===null?null:safe(code,256),message:safe(message,2048)},details:{},truncated:message.length>2048||name.length>128||errorName.length>256||(code?.length??0)>256});
  }
  private add(value:Omit<SDKDiagnostic,'schemaVersion'|'execution'|'occurredAt'|'sequence'>):SDKDiagnostic {
    const entry:SDKDiagnostic={schemaVersion:1,execution:this.state.execution,sequence:this.state.warningCount+this.state.observerErrorCount+1,occurredAt:new Date().toISOString(),...value};
    validate('SDKDiagnostic',entry,this.state.execution);
    if(entry.source==='sdk_warning')this.state.warningCount++;else this.state.observerErrorCount++;
    this.state.entries.push(entry);
    if(this.state.entries.length>32){this.state.entries.shift();this.state.omittedCount++;}
    if(this.output){const file=path.join(this.output,'logs/sdk-diagnostics.jsonl'),raw=JSON.stringify(entry)+'\n';this.writes=this.writes.then(async()=>{await mkdir(path.dirname(file),{recursive:true,mode:0o700});await appendFile(file,raw,{mode:0o600});}).catch(()=>undefined);}
    return structuredClone(entry);
  }
  async drain():Promise<void>{await this.writes;}
  snapshot():SDKDiagnostics {return structuredClone(this.state);}
}
