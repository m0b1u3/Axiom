import {standaloneValidationContract,structuredOutputControls,type StandaloneValidationContractSummary,type StructuredOutputControls} from './validation-output-contract.js';
import {createHash,type Hash} from 'node:crypto';
import {openSync,closeSync,writeSync,fsyncSync,mkdirSync} from 'node:fs';
import path from 'node:path';
import {SseParser} from './sse.js';

type Json = Record<string,unknown>;
const object=(v:unknown):Json=>v!==null&&typeof v==='object'&&!Array.isArray(v)?v as Json:{};
const sha=(value:string|Uint8Array)=>createHash('sha256').update(value).digest('hex');
const statuses=new Set(['completed','in_progress','failed','incomplete','cancelled','queued']);
const events=new Set(['response.created','response.in_progress','response.output_text.delta','response.output_text.done','response.output_item.done','response.completed','response.failed','response.incomplete','error']);
class TextDigest {
 private hash:Hash=createHash('sha256');private pending='';private units=0;private bytes=0;
 update(value:unknown){
  if(typeof value!=='string')return;this.units+=value.length;let text=this.pending+value;this.pending='';
  if(text.length){const c=text.charCodeAt(text.length-1);if(c>=0xd800&&c<=0xdbff){this.pending=text.slice(-1);text=text.slice(0,-1);}}
  this.hash.update(text);this.bytes+=Buffer.byteLength(text);
 }
 value(){const copy=this.hash.copy();copy.update(this.pending);return {sha256:copy.digest('hex'),utf16Units:this.units,bytes:this.bytes+Buffer.byteLength(this.pending)};}
}
export interface TurnDiagnostic {
 schemaVersion:1;protocol:'responses'|'chat';request:{sha256:string;bytes:number;stream:boolean;format:string;formatSha256:string|null;validationContract:StandaloneValidationContractSummary|null;controls:StructuredOutputControls};
 response:{httpStatus:number|null;contentType:string;rawSha256:string;rawBytes:number;events:Record<string,number>;invalidFrames:number;status:string|null;finishReason:string|null;deltaText:ReturnType<TextDigest['value']>;doneText:ReturnType<TextDigest['value']>;itemText:ReturnType<TextDigest['value']>;terminalText:ReturnType<TextDigest['value']>};cancelled:boolean;
}
/** A receipt of byte/text hashes, never a prompt, response body, URL, header,
 * reasoning text, tool argument, identity or credential. Does not change wire data. */
export class UpstreamTurnDiagnostic {
 private readonly rawHash=createHash('sha256');private rawBytes=0;private httpStatus:number|null=null;private contentType='unknown';
 private readonly delta=new TextDigest();private readonly done=new TextDigest();private readonly items=new TextDigest();private readonly terminal=new TextDigest();
 private readonly counts:Record<string,number>={};private invalidFrames=0;private status:string|null=null;private finishReason:string|null=null;
 private readonly decoder=new TextDecoder();private parser:SseParser|null=null;private json:string[]=[];private jsonBytes=0;private ended=false;
 private readonly request:TurnDiagnostic['request'];
 constructor(raw:Uint8Array,private readonly protocol:'responses'|'chat',private readonly record:(value:TurnDiagnostic)=>void){
  let body:Json={};try{body=object(JSON.parse(Buffer.from(raw).toString('utf8')));}catch{}
  const format=protocol==='responses'?object(object(body.text).format):object(body.response_format);
  const type=typeof format.type==='string'&&['json_schema','json_object','text'].includes(format.type)?format.type:'unspecified';
  this.request={sha256:sha(raw),bytes:raw.byteLength,stream:body.stream===true,format:type,formatSha256:Object.keys(format).length?sha(JSON.stringify(format)):null,validationContract:standaloneValidationContract(format),controls:structuredOutputControls(body)};
 }
 headers(status:number,contentType:string){
  this.httpStatus=status;this.contentType=contentType.includes('text/event-stream')?'event_stream':contentType.includes('application/json')?'json':'other';
  if(this.contentType==='event_stream')this.parser=new SseParser((event,payload)=>{
   if(payload.trim()==='[DONE]')return;
   let value:unknown;try{value=JSON.parse(payload)}catch{this.invalidFrames++;return;}
   this.event(object(value),event);
  });
 }
 chunk(chunk:Uint8Array){
  this.rawHash.update(chunk);this.rawBytes+=chunk.byteLength;
  const text=this.decoder.decode(chunk,{stream:true});
  try{if(this.parser)this.parser.push(text);else if(this.contentType==='json'){this.jsonBytes+=chunk.byteLength;if(this.jsonBytes<=32*1024*1024)this.json.push(text);}}
  catch{this.invalidFrames++;this.parser=null;this.json=[];}
 }
 private message(value:unknown,digest:TextDigest){const item=object(value);if(item.type!=='message'||!Array.isArray(item.content))return;for(const part of item.content){const p=object(part);if(p.type==='output_text')digest.update(p.text);}}
 private event(value:Json,event:string){
  if(this.protocol==='chat'){
   const choice=object(Array.isArray(value.choices)?value.choices[0]:null),delta=object(choice.delta??choice.message);
   this.delta.update(delta.content);
   if(typeof choice.finish_reason==='string')this.finishReason=['stop','tool_calls','function_call','length','content_filter'].includes(choice.finish_reason)?choice.finish_reason:'other';
   return;
  }
  const kind=typeof value.type==='string'?value.type:event,key=events.has(kind)?kind:'other';this.counts[key]=(this.counts[key]??0)+1;
  if(kind==='response.output_text.delta')this.delta.update(value.delta);
  if(kind==='response.output_text.done')this.done.update(value.text);
  if(kind==='response.output_item.done')this.message(value.item,this.items);
  if(['response.completed','response.failed','response.incomplete'].includes(kind)){
   const response=object(value.response);this.status=typeof response.status==='string'&&statuses.has(response.status)?response.status:'unknown';
   if(Array.isArray(response.output))for(const item of response.output)this.message(item,this.terminal);
  }
 }
 finish(cancelled:boolean){
  if(this.ended)return;this.ended=true;
  try{
   const tail=this.decoder.decode();if(this.parser){this.parser.push(tail);this.parser.end();}
   else if(this.contentType==='json'&&this.jsonBytes<=32*1024*1024){
    const value=object(JSON.parse(this.json.join('')+tail));
    if(this.protocol==='responses'){this.status=typeof value.status==='string'&&statuses.has(value.status)?value.status:'unknown';if(Array.isArray(value.output))for(const item of value.output)this.message(item,this.terminal);}else this.event(value,'json');
   }
  }catch{this.invalidFrames++;}
  try{this.record({schemaVersion:1,protocol:this.protocol,request:this.request,response:{httpStatus:this.httpStatus,contentType:this.contentType,rawSha256:this.rawHash.digest('hex'),rawBytes:this.rawBytes,events:this.counts,invalidFrames:this.invalidFrames,status:this.status,finishReason:this.finishReason,deltaText:this.delta.value(),doneText:this.done.value(),itemText:this.items.value(),terminalText:this.terminal.value()},cancelled});}catch{/* Diagnostics must never trigger a request replay or alter model output. */}
 }
}

/** Per independent validation, bounded private metadata. Failed/omitted writes
 * are explicit in the closing summary, not silently treated as complete capture. */
export class ValidationTransportJournal {
 private fd:number|null=null;private bytes=0;private received=0;private omitted=0;private failed=false;
 constructor(output:string,private readonly maximumBytes=4*1024*1024){try{const folder=path.join(output,'logs');mkdirSync(folder,{recursive:true,mode:0o700});this.fd=openSync(path.join(folder,'validation-transport.jsonl'),'wx',0o600);}catch{this.failed=true;}}
 record=(value:TurnDiagnostic)=>{this.received++;if(this.fd===null){this.omitted++;return;}const raw=Buffer.from(JSON.stringify({sequence:this.received,...value})+'\n');if(this.bytes+raw.length+1024>this.maximumBytes){this.omitted++;return;}try{this.write(raw);this.bytes+=raw.length;}catch{this.failed=true;this.omitted++;}};
 private write(raw:Buffer){let offset=0;while(offset<raw.length){const n=writeSync(this.fd!,raw,offset);if(!n)throw new Error('short write');offset+=n;}}
 close(){const summary={schemaVersion:1,received:this.received,omitted:this.omitted,state:this.failed||this.omitted?'incomplete':'complete'};if(this.fd!==null){try{this.write(Buffer.from(JSON.stringify({summary})+'\n'));fsyncSync(this.fd);}catch{summary.state='incomplete';}finally{try{closeSync(this.fd);}catch{summary.state='incomplete';}this.fd=null;}}return summary;}
}
