import type {ExecutionCompletion,JsonValue,ScanResult} from '../contracts';

export function estimatedCost(value:number|null|undefined):string{
  return value==null?'未知':`$${value.toLocaleString('en-US',{minimumFractionDigits:2,maximumFractionDigits:6})}`;
}
export function reportedModel(sdk:JsonValue|undefined):string{
  if(!sdk || typeof sdk!=='object' || Array.isArray(sdk))return '未知';
  const turn=sdk.turn;
  return turn && typeof turn==='object' && !Array.isArray(turn) && typeof turn.model==='string'?turn.model:'未知';
}
export function processMismatch(completion:ExecutionCompletion|null|undefined):boolean{
  if(!completion?.result)return false;
  if(completion.process.code==null&&completion.process.signal==null)return false;
  const codes={completed:0,partial:2,failed:2,canceled:7,timeout:8};
  return completion.process.signal!=null || completion.process.code!==codes[completion.result.status];
}

export function nativeUsageSections(result:ScanResult):Array<{title:string;rows:Array<{key:string;label:string;value:string}>}>{
  const object=(v:JsonValue|undefined):Record<string,JsonValue>|null=>v && typeof v==='object' && !Array.isArray(v)?v:null;
  const sdk=object(result.extensions?.sdk),turn=object(sdk?.turn);
  const usage=object(turn?.usage);
  const labels:Record<string,string>={estimatedUsdRange:'Token 估算范围（非账单）',pricing:'SDK 定价依据',estimatedUsd:'估算基准（USD）',model:'计费模型',inputTokens:'输入 Token',outputTokens:'输出 Token',cachedInputTokens:'缓存输入 Token',cacheWriteInputTokens:'缓存写入 Token',input_tokens:'输入 Token',output_tokens:'输出 Token',cached_input_tokens:'缓存输入 Token',cache_write_input_tokens:'缓存写入 Token',reasoning_output_tokens:'推理输出 Token',total_tokens:'总 Token',observedAt:'观测时间',source:'来源'};
  const rows=(v:Record<string,JsonValue>)=>Object.entries(v).map(([key,value])=>({key,label:labels[key]??key,value:value===null?'未知':typeof value==='object'?JSON.stringify(value):String(value)}));
  const sections=[];
  if(usage)sections.push({title:'SDK 原生 Token 用量',rows:rows(usage)});
  return sections;
}
