import {validate,type ExecutionRef,type ExecutionTokenUsage} from '@axiom/contracts';
const record=(v:unknown):Record<string,unknown>|null=>v!==null&&typeof v==='object'&&!Array.isArray(v)?v as Record<string,unknown>:null;
const count=(v:unknown):number|null=>typeof v==='number'&&Number.isSafeInteger(v)&&v>=0?v:null;
function checkedUsage(value:ExecutionTokenUsage):ExecutionTokenUsage {
 if(value.inputTokens===null||value.outputTokens===null)value.complete=false;
 try{validate('ExecutionTokenUsage',value,value.execution);return value}
 catch{return {...value,complete:false,inputTokens:null,cachedInputTokens:null,cacheWriteInputTokens:null,outputTokens:null,reasoningOutputTokens:null,totalTokens:null}}
}

export function sdkExecutionUsage(execution:ExecutionRef,value:unknown,complete:boolean):ExecutionTokenUsage|undefined {
 const v=record(value);if(!v)return;
 return checkedUsage({schemaVersion:1,execution,source:'sdk_on_usage',complete,
  inputTokens:count(v.input_tokens),cachedInputTokens:count(v.cached_input_tokens),
  cacheWriteInputTokens:v.cache_write_input_tokens_reported===false?null:count(v.cache_write_input_tokens),
  outputTokens:count(v.output_tokens),reasoningOutputTokens:count(v.reasoning_output_tokens),
  totalTokens:count(v.total_tokens),requestCount:null});
}

type Receipt={id:string|null;complete:boolean;usage:Omit<ExecutionTokenUsage,'schemaVersion'|'execution'|'source'|'complete'|'requestCount'>|null};

/** Metadata only. No prompts, answers, credentials, prices or model requests. */
export class ResponseUsageReceipts {
 private receipts:Receipt[]=[];
 begin():{observe:(response:unknown,complete?:boolean)=>void} {
  const receipt:Receipt={id:null,complete:false,usage:null};this.receipts.push(receipt);
  return {observe:(response,complete=false)=>{
   const r=record(response),u=record(r?.usage);if(!r||!u)return;
   const input=record(u.input_tokens_details),output=record(u.output_tokens_details);
   receipt.id=typeof r.id==='string'?r.id:null;
   receipt.complete=complete||r.status==='completed';
   const i=count(u.input_tokens),o=count(u.output_tokens);
   receipt.usage={inputTokens:i,cachedInputTokens:count(input?.cached_tokens??u.cached_input_tokens),
    cacheWriteInputTokens:count(u.cache_write_input_tokens),outputTokens:o,
    reasoningOutputTokens:count(output?.reasoning_tokens??u.reasoning_output_tokens),
    totalTokens:count(u.total_tokens)};
  }};
 }
 summary(execution:ExecutionRef,finished:boolean):ExecutionTokenUsage|undefined {
  if(!this.receipts.length)return;
  const unique=new Map<string,Receipt>();let conflict=false,missing=false;
  for(const [index,r] of this.receipts.entries()){
   if(!r.usage){missing=true;continue}
   const key=r.id??'request-'+index,prior=unique.get(key);
   if(prior&&JSON.stringify(prior)!==JSON.stringify(r))conflict=true;else unique.set(key,r);
  }
  const sum=(key:keyof NonNullable<Receipt['usage']>):number|null=>{
   let n=0;for(const r of unique.values()){const amount=r.usage![key];if(amount===null||!Number.isSafeInteger(n+amount))return null;n+=amount}return unique.size?n:null;
  };
  return checkedUsage({schemaVersion:1,execution,source:'responses_usage',complete:finished&&!missing&&!conflict&&[...unique.values()].every(r=>r.complete),
   inputTokens:conflict?null:sum('inputTokens'),cachedInputTokens:conflict?null:sum('cachedInputTokens'),
   cacheWriteInputTokens:conflict?null:sum('cacheWriteInputTokens'),outputTokens:conflict?null:sum('outputTokens'),
   reasoningOutputTokens:conflict?null:sum('reasoningOutputTokens'),totalTokens:conflict?null:sum('totalTokens'),
   requestCount:this.receipts.length});
 }
}
