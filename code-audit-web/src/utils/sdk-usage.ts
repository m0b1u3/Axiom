import type {SDKUsage,ExecutionRef} from '../contracts';

/** Token observations are not a bill, a price or proof of complete accounting. */
export function sdkUsageView(value:unknown,execution?:ExecutionRef):{value:SDKUsage|null;invalid:boolean}{
 if(value===undefined)return {value:null,invalid:false};
 const bad={value:null,invalid:true};
 if(!value||typeof value!=='object'||Array.isArray(value))return bad;
 const v=value as SDKUsage;
 if(v.schemaVersion!==1||v.sdkVersion!=='0.5.0'||v.source!=='sdk_on_usage'||v.measurement!=='observed_tokens'||!Number.isFinite(Date.parse(v.observedAt)))return bad;
 if(!v.execution||typeof v.execution.taskId!=='string'||!Number.isSafeInteger(v.execution.attempt)||v.execution.attempt<1)return bad;
 if(execution&&(v.execution.taskId!==execution.taskId||v.execution.attempt!==execution.attempt))return bad;
 if(v.componentIndex!==undefined&&(!Number.isSafeInteger(v.componentIndex)||v.componentIndex<0))return bad;
 if(v.cacheWriteInputTokensReported!==undefined&&typeof v.cacheWriteInputTokensReported!=='boolean')return bad;
 if(v.cacheWriteInputTokensReported===false&&v.cacheWriteInputTokens!==null)return bad;
 for(const k of ['inputTokens','cachedInputTokens','cacheWriteInputTokens','outputTokens','reasoningOutputTokens','totalTokens'] as const){
  if(v[k]!==null&&(!Number.isSafeInteger(v[k])||Number(v[k])<0))return bad;
 }
 return {value:v,invalid:false};
}
