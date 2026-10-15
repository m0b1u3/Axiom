import {createServer} from 'node:http';
import {createHash,randomUUID} from 'node:crypto';
import {ChatSseParser,type ChatShim,upstreamDispatcher} from './chat-shim.js';
import {RequestGate,retryDelay,quotaExhausted} from './request-gate.js';

/** Protocol-neutral transport guard; native Responses receives an additional terminal check. */
export async function startReliableUpstream(upstream:string,gateFile:string,notice:(value:string)=>void=()=>{}):Promise<ChatShim>{
 const gate=new RequestGate(gateFile);
 const server=createServer((req,res)=>{void(async()=>{
  const controller=new AbortController();res.on('close',()=>controller.abort());
  const chunks:Buffer[]=[];let size=0;
  for await(const chunk of req){size+=chunk.length;if(size>32*1024*1024){res.writeHead(413);res.end();return;}chunks.push(chunk);}
  const raw=Buffer.concat(chunks);let body:Record<string,unknown>;
  try{body=JSON.parse(raw.toString('utf8'));}catch{res.writeHead(400);res.end();return;}
  const route=(req.url??'').split('?')[0];
  if(req.method!=='POST'||!['/responses','/v1/responses','/chat/completions','/v1/chat/completions','/responses/compact','/v1/responses/compact'].includes(route??'')){res.writeHead(404);res.end();return;}
  const endpoint=route!.endsWith('/chat/completions')?'/chat/completions':route!.endsWith('/responses/compact')?'/responses/compact':'/responses';
  const url=upstream.replace(/\/+$/,'').replace(/\/(responses|chat\/completions)$/,'')+endpoint;
  const authorization=String(req.headers.authorization??'');
  const key=createHash('sha256').update(upstream+'\0'+String(body.model)+'\0'+authorization).digest('hex');
  let release:(()=>void)|undefined;
  const dispatcher=upstreamDispatcher(upstream);
  try{
   let response:Response|undefined;
   for(let attempt=0;attempt<3;attempt++){
    release=await gate.acquire(key,controller.signal);
    response=await fetch(url,{method:'POST',headers:{'content-type':'application/json',accept:'text/event-stream',authorization},body:raw,signal:controller.signal,...(dispatcher?{dispatcher}:{})} as unknown as RequestInit);
    if(response.status!==429)break;
    const text=await response.text();release();release=undefined;
    if(quotaExhausted(text)){
     notice('模型额度已耗尽，停止限流重试');res.writeHead(402,{'content-type':'application/json'});res.end(JSON.stringify({error:{code:'quota_exhausted',message:'Model quota exhausted; update provider billing or free-tier-only settings.'}}));return;
    }
    const delay=retryDelay(response.headers.get('retry-after'),attempt);
    gate.cooldown(key,Math.min(delay,3600000));notice(`模型限流：请求退避 ${Math.ceil(delay/1000)} 秒，第 ${attempt+1}/3 次`);
    if(attempt===2||delay>120000){res.writeHead(429,{'content-type':'application/json','retry-after':String(Math.ceil(delay/1000))});res.end(text);return;}
   }
   if(!response)throw new Error('missing upstream response');
   const contentType=response.headers.get('content-type')??'application/json';
   if(!response.ok){const text=await response.text();res.writeHead(response.status,{'content-type':contentType});res.end(text);return;}
   res.writeHead(200,{'content-type':contentType,'cache-control':'no-cache'});
   if(endpoint==='/responses'&&contentType.includes('text/event-stream')){
    let terminal=false,usable=false;
    const failure=(code:string,message:string)=>{if(terminal)return;terminal=true;res.write(`event: response.failed\ndata: ${JSON.stringify({type:'response.failed',response:{id:'resp_'+randomUUID(),status:'failed',error:{code,message}}})}\n\n`);notice(message);};
    const hasOutput=(item:any):boolean=>item?.type==='function_call'||item?.type==='custom_tool_call'||(item?.type==='message'&&item.content?.some((p:any)=>typeof p.text==='string'&&p.text.trim()));
    const parser=new ChatSseParser((event,payload)=>{
     if(terminal||payload==='[DONE]')return;
     let value:any;try{value=JSON.parse(payload)}catch{failure('upstream_invalid_sse','上游 Responses 流包含无效 JSON');return;}
     if(value.type==='response.output_item.done')usable ||= hasOutput(value.item);
     if(value.type==='response.completed'){
      usable ||= (value.response?.output??[]).some(hasOutput);
      if(value.response?.status==='incomplete'){failure('upstream_output_truncated','上游输出被截断，不能作为已完成审计');return;}
      if(!usable){failure('upstream_empty_response','上游会话空返回，没有正文或工具调用');return;}terminal=true;
     }
     if(['response.failed','response.incomplete','error'].includes(value.type)||event==='error')terminal=true;
     res.write(`event: ${event==='message'?(value.type??event):event}\ndata: ${payload}\n\n`);
    });
    const decoder=new TextDecoder();
    if(response.body)for await(const chunk of response.body as any){parser.push(decoder.decode(chunk,{stream:true}));}
    parser.push(decoder.decode());parser.end();
    if(!terminal)failure('upstream_stream_incomplete','上游连接结束但没有完成事件');
    }else if(endpoint==='/responses'&&contentType.includes('application/json')){
    const value:any=await response.json();
    const usable=Array.isArray(value.output)&&value.output.some((item:any)=>['function_call','custom_tool_call'].includes(item.type)||(item.type==='message'&&item.content?.some((p:any)=>typeof p.text==='string'&&p.text.trim())));
    if(value.status==='completed'&&!usable){value.status='failed';value.error={code:'upstream_empty_response',message:'Upstream response contains no answer or tool calls'};notice('上游会话空返回，没有正文或工具调用');}
    res.write(JSON.stringify(value));
   }else if(response.body){for await(const chunk of response.body as any)res.write(chunk);}
   res.end();
  }catch(e){if(!res.destroyed){if(!res.headersSent){res.writeHead(502,{'content-type':'application/json'});res.end(JSON.stringify({error:{code:'upstream_transport_error',message:'Upstream transport interrupted'}}));}else{res.end('event: response.failed\ndata: '+JSON.stringify({type:'response.failed',response:{id:'resp_'+randomUUID(),status:'failed',error:{code:'upstream_transport_error',message:'Upstream transport interrupted'}}})+'\n\n');}}}
  finally{release?.();await dispatcher?.close();}
 })().catch(()=>{if(!res.destroyed){res.statusCode=500;res.end();}});});
 server.requestTimeout=0;server.headersTimeout=0;server.timeout=0;
 await new Promise<void>(r=>server.listen(0,'127.0.0.1',r));const address=server.address();
 return {baseUrl:`http://127.0.0.1:${typeof address==='object'&&address?address.port:0}`,upstreamBaseUrl:upstream,close:()=>new Promise<void>(r=>server.close(()=>{gate.close();r();}))};
}
