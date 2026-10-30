import {createHash} from 'node:crypto';

type RecordValue=Record<string,unknown>;
const record=(value:unknown):RecordValue|undefined=>value!==null&&typeof value==='object'&&!Array.isArray(value)?value as RecordValue:undefined;
const digest=(value:string)=>createHash('sha256').update(value).digest('hex');
const dispatchTools=new Set(['spawn_agent','followup_task','send_message']);

export function deepSeekAgentMessageCompatibility(provider:string,baseUrl:string,runtime:'official-v2'|'legacy-v1'):boolean {
 return runtime==='official-v2'&&provider==='deepseek'&&new URL(baseUrl).href.replace(/\/+$/,'')==='https://api.deepseek.com';
}

export class AgentMessageCompatibilityError extends Error {
 constructor(readonly path:string,readonly code:'agent_message_payload_unverified'|'agent_message_content_unsupported'|'agent_message_reference_unsupported') {
  super(`DeepSeek V2 任务消息无法安全转为标准消息（${path}，${code}）；请求未发送。`);
 }
}

/** DeepSeek silently ignores Codex's agent_message item (real A/B evidence,
 * 2026-10-29). Preserve its existing attribution text and ordering in a public
 * user message. Never decrypt opaque content: hidden blocks must exactly match
 * plaintext already exposed in a native V2 dispatch tool's arguments. */
export class ResponsesAgentMessages {
 private readonly approved=new Set<string>();
 constructor(private readonly maximumMessages=8192){}

 private remember(value:unknown):void {
  const item=record(value);
  if(item?.type!=='function_call'||item.namespace!=='collaboration'||typeof item.name!=='string'||!dispatchTools.has(item.name)||typeof item.arguments!=='string')return;
  let args:RecordValue|undefined;try{args=record(JSON.parse(item.arguments));}catch{return;}
  if(typeof args?.message!=='string'||!args.message)return;
  const key=digest(args.message);
  if(this.approved.has(key)||this.approved.size<this.maximumMessages)this.approved.add(key);
 }

 /** Observing complete items is idempotent and never changes a response. */
 observe(value:unknown):void {
  const event=record(value);if(!event)return;
  if(event.type==='response.output_item.done')this.remember(event.item);
  const response=record(event.response)??event;
  if(Array.isArray(response.output))for(const item of response.output)this.remember(item);
 }

 prepare(raw:Buffer):Buffer {
  const body=record(JSON.parse(raw.toString('utf8')));
  if(!body||!Array.isArray(body.input))return raw;
  // Saved model outputs remain usable when the same parent request carries
  // their dispatch arguments. No message text is retained by this registry.
  for(const item of body.input)this.remember(item);
  const referenced=new Set(body.input.flatMap(value=>{
   const item=record(value);
   return typeof item?.id==='string'&&(item.type==='item_reference'||item.type==null&&item.role===undefined&&item.content===undefined)?[item.id]:[];
  }));
  let changed=false;
  const input=body.input.map((value,index)=>{
   const item=record(value);if(item?.type!=='agent_message')return value;
   const location=`input[${index}]`;
   if(typeof item.id==='string'&&referenced.has(item.id))throw new AgentMessageCompatibilityError(location+'.id','agent_message_reference_unsupported');
   if(typeof item.author!=='string'||!item.author||typeof item.recipient!=='string'||!item.recipient||!Array.isArray(item.content)||!item.content.length)throw new AgentMessageCompatibilityError(location,'agent_message_content_unsupported');
   const content=item.content.map((value,partIndex)=>{
    const part=record(value),where=`${location}.content[${partIndex}]`;
    if(part?.type==='input_text'&&typeof part.text==='string')return value;
    if(part?.type==='encrypted_content'&&typeof part.encrypted_content==='string'){
     if(!part.encrypted_content||!this.approved.has(digest(part.encrypted_content)))throw new AgentMessageCompatibilityError(where,'agent_message_payload_unverified');
     return {type:'input_text',text:part.encrypted_content};
    }
    throw new AgentMessageCompatibilityError(where,'agent_message_content_unsupported');
   });
   changed=true;
   // SDK-local IDs are not server output IDs; references are rejected above.
   // Author/recipient attribution remains verbatim in the SDK's header text.
   return {type:'message',role:'user',content};
  });
  return changed?Buffer.from(JSON.stringify({...body,input})):raw;
 }
}
