import {validationJSONRequest,type ValidationOutputMode} from './validation-output-mode.js';
import {createServer} from 'node:http';
import {fetch as upstreamFetch} from 'undici';
import {createHash,randomUUID} from 'node:crypto';
import {SseParser} from './sse.js';
import {type UpstreamProxy,upstreamDispatcher} from './upstream-http.js';
import {RequestGate,retryDelay,quotaExhausted} from './request-gate.js';
import {UpstreamTurnDiagnostic,type TurnDiagnostic} from './turn-diagnostic.js';
import {upstreamTransportDiagnostic,upstreamResponseDiagnostic} from './transport-diagnostic.js';
import {NamespaceTools,NamespaceToolError} from './namespace-tools.js';
import {prepareResponsesInput,ResponsesInputError} from './responses-input.js';
import {validationSourceCitations} from './validation-source-citations.js';
import {ResponseUsageReceipts} from '../runtime/execution-token-usage.js';
import {TokenBudgetError,type TokenBudget} from '../runtime/token-budget.js';
import {ResponsesAgentMessages,AgentMessageCompatibilityError} from './agent-messages.js';
import {flattenedResponseSchema,SchemaUnionCompatibilityError} from './schema-unions.js';

/** Responses transport guard: preserve requests and require a completed answer. */
export async function startReliableUpstream(upstream:string,gateFile:string,notice:(value:string)=>void=()=>{},observeTurn?:((value:TurnDiagnostic)=>void),outputMode:ValidationOutputMode='native_schema',frozenReview=false,budget?:TokenBudget,compatibility:{agentMessages?:boolean;nestedAnyOf?:boolean}={}):Promise<UpstreamProxy>{
 if(new URL(upstream).pathname.replace(/\/+$/,'').endsWith('/chat/completions'))throw new Error('Chat endpoints are no longer supported; configure a Responses base URL');
 const gate=new RequestGate(gateFile);
 const dispatcher=upstreamDispatcher(upstream);
 const namespaceTools=new NamespaceTools();
 const agentMessages=compatibility.agentMessages?new ResponsesAgentMessages():undefined;
 const usageReceipts=new ResponseUsageReceipts();
 const server=createServer((req,res)=>{void(async()=>{
  const controller=new AbortController();res.on('close',()=>controller.abort());
  const chunks:Buffer[]=[];let size=0;
  for await(const chunk of req){size+=chunk.length;if(size>32*1024*1024){res.writeHead(413);res.end();return;}chunks.push(chunk);}
  const raw=Buffer.concat(chunks);let body:Record<string,unknown>;
  try{body=JSON.parse(raw.toString('utf8'));}catch{res.writeHead(400);res.end();return;}
  const route=(req.url??'').split('?')[0];
  if(req.method!=='POST'||!['/responses','/v1/responses','/responses/compact','/v1/responses/compact'].includes(route??'')){res.writeHead(404);res.end();return;}
  const endpoint=route!.endsWith('/responses/compact')?'/responses/compact':'/responses';
  const url=upstream.replace(/\/+$/,'').replace(/\/responses$/,'')+endpoint;
  let forwarded:Buffer, restore:(value:unknown)=>unknown;
  try{
   const formatted=endpoint==='/responses/compact'?raw:validationJSONRequest(raw,outputMode);
   const normalized=prepareResponsesInput(compatibility.nestedAnyOf&&endpoint!=='/responses/compact'?flattenedResponseSchema(formatted):formatted);
   const publicMessages=agentMessages?.prepare(normalized)??normalized;
   const plan=namespaceTools.prepare(endpoint==='/responses/compact'?publicMessages:validationSourceCitations(publicMessages,frozenReview),endpoint==='/responses/compact');forwarded=plan.body;
   restore=value=>{const restored=plan.restore(value);agentMessages?.observe(restored);return restored;};
  }
  catch(e){res.writeHead(400,{'content-type':'application/json'});res.end(JSON.stringify({error:e instanceof ResponsesInputError||e instanceof AgentMessageCompatibilityError?{code:e.code,param:e.path,message:e.message}:e instanceof SchemaUnionCompatibilityError?{code:e.code,param:'text.format.schema',message:e.message}:e instanceof NamespaceToolError?{code:'namespace_tool_incompatible',message:e.message}:{code:'validation_output_contract_unsupported',message:'JSON compatibility requires the recognized standalone validation contract.'}}));return;}
  const receipt=observeTurn?new UpstreamTurnDiagnostic(forwarded,'responses',observeTurn):null;
  const authorization=String(req.headers.authorization??'');
  const key=createHash('sha256').update(upstream+'\0'+String(body.model)+'\0'+authorization).digest('hex');
  let release:(()=>void)|undefined;
  let usageReceipt:ReturnType<ResponseUsageReceipts['begin']>|undefined;
  const startedAt=Date.now();let phase:'headers'|'stream'='headers';
  try{
   budget?.check();
   release=await gate.acquire(key,controller.signal);
   budget?.check();
   usageReceipt=usageReceipts.begin();
   const response=await upstreamFetch(url,{method:'POST',headers:{'content-type':'application/json',accept:'text/event-stream',authorization},body:forwarded,signal:controller.signal,dispatcher});
   receipt?.headers(response.status,response.headers.get('content-type')??'application/json');
   if(response.status===429){
    const bytes=Buffer.from(await response.arrayBuffer());receipt?.chunk(bytes);const text=bytes.toString('utf8');release();release=undefined;
    try{usageReceipt.observe(JSON.parse(text));}catch{}
    budget?.requestFailed(quotaExhausted(text)?'模型接口返回 HTTP 429：quota_exhausted':'模型接口返回 HTTP 429');
    if(quotaExhausted(text)){
     notice('模型额度已耗尽，停止限流重试');res.writeHead(402,{'content-type':'application/json'});res.end(JSON.stringify({error:{code:'quota_exhausted',message:'Model quota exhausted; update provider billing or free-tier-only settings.'}}));return;
    }
    const delay=retryDelay(response.headers.get('retry-after'),0);
    gate.cooldown(key,delay);notice(`模型限流：等待 ${Math.ceil(delay/1000)} 秒后由 SDK 决定重试`);
    // A single retry owner: pass this failure to native Codex, respecting the
    // same provider cooldown across Scanner instances. Never replay here.
    res.writeHead(429,{'content-type':'application/json','retry-after':response.headers.get('retry-after')??String(Math.ceil(delay/1000))});res.end(text);return;
   }
   const contentType=response.headers.get('content-type')??'application/json';
   if(!response.ok){const bytes=Buffer.from(await response.arrayBuffer());receipt?.chunk(bytes);const text=bytes.toString('utf8');try{usageReceipt.observe(JSON.parse(text));}catch{}budget?.requestFailed(`模型接口返回 HTTP ${response.status}`);res.writeHead(response.status,{'content-type':contentType});res.end(text);return;}
   phase='stream';
   if(endpoint==='/responses'&&contentType.includes('text/event-stream')){
    res.writeHead(200,{'content-type':contentType,'cache-control':'no-cache'});
    let terminal=false,usable=false;
    const failure=(code:string,message:string)=>{if(terminal)return;terminal=true;budget?.requestFailed(message+' ['+code+']');res.write(`event: response.failed\ndata: ${JSON.stringify({type:'response.failed',response:{id:'resp_'+randomUUID(),status:'failed',error:{code,message}}})}\n\n`);notice(message);};
    const hasOutput=(item:any):boolean=>item?.type==='function_call'||item?.type==='custom_tool_call'||(item?.type==='message'&&item.content?.some((p:any)=>typeof p.text==='string'&&p.text.trim()));
    const parser=new SseParser((event,payload)=>{
     if(terminal||payload==='[DONE]')return;
     let value:any;try{value=JSON.parse(payload)}catch{failure('upstream_invalid_sse','上游 Responses 流包含无效 JSON');return;}
     if(['response.completed','response.failed','response.incomplete'].includes(value.type))usageReceipt!.observe(value.response,value.type==='response.completed');
     if(value.type==='response.output_item.done')usable ||= hasOutput(value.item);
     if(value.type==='response.completed'){
      usable ||= (value.response?.output??[]).some(hasOutput);
      if(value.response?.status==='incomplete'){failure('upstream_output_truncated','上游输出被截断，不能作为已完成审计');return;}
      if(!usable){failure('upstream_empty_response','上游会话空返回，没有正文或工具调用');return;}terminal=true;
     }
     if(['response.failed','response.incomplete','error'].includes(value.type)||event==='error'){
      terminal=true;const diagnostic='模型响应失败 '+JSON.stringify(upstreamResponseDiagnostic(value,event,startedAt));budget?.requestFailed(diagnostic);notice(diagnostic);
     }
     try{const restored=restore(value);res.write(`event: ${event==='message'?(value.type??event):event}\ndata: ${JSON.stringify(restored)}\n\n`);}catch{terminal=false;failure('namespace_tool_incompatible',new NamespaceToolError().message);}
    });
    const decoder=new TextDecoder();
    if(response.body)for await(const chunk of response.body as any){receipt?.chunk(chunk);parser.push(decoder.decode(chunk,{stream:true}));}
    parser.push(decoder.decode());parser.end();
    if(!terminal)failure('upstream_stream_incomplete','上游连接结束但没有完成事件');
    }else if(contentType.includes('application/json')){
    const bytes=Buffer.from(await response.arrayBuffer());receipt?.chunk(bytes);const value:any=JSON.parse(bytes.toString('utf8'));
    usageReceipt.observe(value,endpoint==='/responses/compact');
    if(value.status==='failed'||value.status==='incomplete'||value.error)budget?.requestFailed('模型响应未完成 '+JSON.stringify(upstreamResponseDiagnostic(value,'message',startedAt)));
    const usable=Array.isArray(value.output)&&value.output.some((item:any)=>['function_call','custom_tool_call'].includes(item.type)||(item.type==='message'&&item.content?.some((p:any)=>typeof p.text==='string'&&p.text.trim())));
    if(endpoint==='/responses'&&value.status==='completed'&&!usable){value.status='failed';value.error={code:'upstream_empty_response',message:'Upstream response contains no answer or tool calls'};budget?.requestFailed('上游会话空返回，没有正文或工具调用');notice('上游会话空返回，没有正文或工具调用');}
    let restored;try{restored=restore(value);}catch{res.writeHead(502,{'content-type':'application/json'});res.end(JSON.stringify({error:{code:'namespace_tool_incompatible',message:new NamespaceToolError().message}}));return;}
    res.writeHead(200,{'content-type':contentType,'cache-control':'no-cache'});res.write(JSON.stringify(restored));
   }else if(response.body){res.writeHead(200,{'content-type':contentType});for await(const chunk of response.body as any){receipt?.chunk(chunk);res.write(chunk);}}
   res.end();
  }catch(e){
   if(e instanceof TokenBudgetError){res.writeHead(402,{'content-type':'application/json'});res.end(JSON.stringify({error:{code:e.code,message:e.message}}));return;}
   if(!controller.signal.aborted){
    const diagnostic=upstreamTransportDiagnostic(e,phase,startedAt);
    budget?.requestFailed('模型请求传输中断：'+String(diagnostic.code));
    notice('模型请求传输中断 '+JSON.stringify(diagnostic));
    if(!res.destroyed&&!res.writableEnded){
     if(!res.headersSent){res.writeHead(502,{'content-type':'application/json'});res.end(JSON.stringify({error:diagnostic}));}
     else res.end('event: response.failed\ndata: '+JSON.stringify({type:'response.failed',response:{id:'resp_'+randomUUID(),status:'failed',error:diagnostic}})+'\n\n');
    }
   }
  }
  finally{
   if(budget&&usageReceipt){budget.observe(usageReceipts.summary(budget.assignment.execution,false),usageReceipt.summary(budget.assignment.execution));if(budget.error)notice(budget.error.message);}
   receipt?.finish(controller.signal.aborted&&!res.writableEnded);release?.();
  }
 })().catch(()=>{if(!res.destroyed){res.statusCode=500;res.end();}});});
 server.requestTimeout=0;server.headersTimeout=0;server.timeout=0;
 await new Promise<void>(r=>server.listen(0,'127.0.0.1',r));const address=server.address();
 return {...(budget?{budgetSignal:budget.signal}:{}),tokenUsage:(execution,complete)=>usageReceipts.summary(execution,complete),baseUrl:`http://127.0.0.1:${typeof address==='object'&&address?address.port:0}`,upstreamBaseUrl:upstream,close:async()=>{await new Promise<void>(r=>server.close(()=>r()));gate.close();await dispatcher.close();}};
}
