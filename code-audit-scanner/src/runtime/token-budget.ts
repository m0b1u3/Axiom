import {lstat,readFile,realpath} from 'node:fs/promises';
import path from 'node:path';
import {decode,validate,COMPATIBILITY,type ExecutionTokenUsage,type ModelPricing,type TokenBudgetAssignment} from '@axiom/contracts';
import type {ScanTask} from '../contract/scan-task.js';

export class TokenBudgetError extends Error {
 constructor(readonly code:'budget_exceeded'|'budget_usage_unknown'|'budget_price_missing',message:string,readonly upstreamFailure?:string){super(message)}
}
const missingPrice=()=>new TokenBudgetError('budget_price_missing','预算需要本次执行冻结的完整 USD Token 单价；未发起模型请求。');

export function budgetPrices(pricing:ModelPricing|null):ModelPricing {
 if(!pricing||pricing.currency!=='USD'||pricing.inputPerMillion===null||pricing.outputPerMillion===null||
  (pricing.cacheMode!=='input_only'&&pricing.cacheReadPerMillion===null)||
  (pricing.cacheMode==='cache_read_write'&&pricing.cacheWritePerMillion===null))throw missingPrice();
 validate('ModelPricing',pricing);return pricing;
}
const rate=(s:string)=>{const [a,b='']=s.split('.');return BigInt(a!)*1000000n+BigInt(b.padEnd(6,'0'))};

/** Same decimal arithmetic and one upward rounding as Server's token estimate. */
export function budgetMicros(p:ModelPricing,u:ExecutionTokenUsage):bigint {
 budgetPrices(p);validate('ExecutionTokenUsage',u,u.execution);
 const unknown=()=>new TokenBudgetError('budget_usage_unknown','模型返回的必要 Token 用量缺失或不一致，完整费用未知；已停止继续请求。');
 if(u.inputTokens===null||u.outputTokens===null)throw unknown();
 const input=BigInt(u.inputTokens),output=BigInt(u.outputTokens);
 if(u.totalTokens!==null&&BigInt(u.totalTokens)!==input+output)throw unknown();
 if(u.reasoningOutputTokens!==null&&u.reasoningOutputTokens>u.outputTokens)throw unknown();
 if(p.cacheMode!=='input_only'&&u.cachedInputTokens===null)throw unknown();
 if(p.cacheMode==='cache_read_write'&&u.cacheWriteInputTokens===null)throw unknown();
 const cached=p.cacheMode==='input_only'?0n:BigInt(u.cachedInputTokens!),write=p.cacheMode==='cache_read_write'?BigInt(u.cacheWriteInputTokens!):0n;
 if(cached+write>input)throw unknown();
 const n=(input-cached-write)*rate(p.inputPerMillion!)+output*rate(p.outputPerMillion!)+
  (p.cacheMode==='input_only'?0n:cached*rate(p.cacheReadPerMillion!))+
  (p.cacheMode==='cache_read_write'?write*rate(p.cacheWritePerMillion!):0n);
 return (n+999999n)/1000000n;
}

export class TokenBudget {
 private controller=new AbortController();
 private stopped:TokenBudgetError|undefined;
 private highest=0n;
 private previous:ExecutionTokenUsage|undefined;
 readonly pricing:ModelPricing;
 constructor(readonly assignment:TokenBudgetAssignment){
  this.assignment=structuredClone(assignment);
  validate('TokenBudgetAssignment',this.assignment,assignment.execution);
  if(assignment.pricingSnapshot.execution.taskId!==assignment.execution.taskId||assignment.pricingSnapshot.execution.attempt!==assignment.execution.attempt)throw missingPrice();
  if(assignment.pricingSnapshot.reason==='model_price_binding_mismatch')throw new TokenBudgetError('budget_price_missing','模型配置的模型或接口地址已更改，本任务的冻结连接没有匹配单价；未发起模型请求，请使用匹配的配置创建新任务。');
  this.pricing=budgetPrices(this.assignment.pricingSnapshot.pricing);
 }
 get signal():AbortSignal{return this.controller.signal}
 get error():TokenBudgetError|undefined{return this.stopped}
 check():void {if(this.stopped)throw this.stopped}
 private stop(error:TokenBudgetError):void {
  if(this.stopped)return;this.stopped=error;
  // Let the terminal response and its usage reach the SDK before cancellation.
  setImmediate(()=>this.controller.abort(error));
 }
 requestFailed(message:string):void {
  this.stop(new TokenBudgetError('budget_usage_unknown',message+'；已停止继续请求，本次请求的完整费用未知。',message));
 }
 observe(total:ExecutionTokenUsage|undefined,request?:ExecutionTokenUsage):void {
  if(this.stopped)return;
  try{
   if(!total||request&&!request.complete)throw new TokenBudgetError('budget_usage_unknown','模型请求结束后没有完整 Token 用量，完整费用未知；已停止继续请求。');
   if(request)budgetMicros(this.pricing,request);
   const required:('inputTokens'|'outputTokens'|'cachedInputTokens'|'cacheWriteInputTokens')[]=['inputTokens','outputTokens'];
   if(this.pricing.cacheMode!=='input_only')required.push('cachedInputTokens');
   if(this.pricing.cacheMode==='cache_read_write')required.push('cacheWriteInputTokens');
   for(const key of required){
    const prior=this.previous?.[key];
    if(prior!==undefined&&prior!==null&&(total[key]===null||total[key]!<prior))throw new TokenBudgetError('budget_usage_unknown','累计 Token 用量发生回退或缺失，完整费用未知；已停止继续请求。');
   }
   const amount=budgetMicros(this.pricing,total);this.previous=structuredClone(total);this.highest=amount>this.highest?amount:this.highest;
   if(this.highest>=BigInt(this.assignment.limitMicros))this.stop(new TokenBudgetError('budget_exceeded','本次执行已达到配置单价估算预算，已停止继续请求并保留已有结果。'));
  }catch(e){this.stop(e instanceof TokenBudgetError?e:new TokenBudgetError('budget_usage_unknown','Token 用量无法通过校验，完整费用未知；已停止继续请求。'))}
 }
}

export function tokenBudget(task:ScanTask):TokenBudget|undefined {
 if(!task.tokenBudget)return;
 checkBudgetBinding(task);
 return new TokenBudget(task.tokenBudget);
}

function checkBudgetBinding(task:ScanTask):void {
 if(!task.tokenBudget)return;
 const v=task.tokenBudget;
 if(v.execution.taskId!==String(task.taskId)||v.execution.attempt!==(task.executionAttempt??1)||v.pricingSnapshot.provider!==task.provider||v.pricingSnapshot.model!==task.model)throw missingPrice();
 if(task.maxCostUsd===undefined||Number(v.limitMicros)/1000000>task.maxCostUsd+Number.EPSILON*Math.max(1,task.maxCostUsd))throw missingPrice();
}

export async function loadTokenBudget(file:string,task:ScanTask):Promise<void> {
 const st=await lstat(file),root=await realpath(task.outputPath),runtime=path.join(root,'runtime');
 if(!st.isFile()||st.isSymbolicLink()||st.size>COMPATIBILITY.limits.documentBytes||await realpath(path.dirname(file))!==runtime||await realpath(file)!==path.join(runtime,'token-budget.json'))throw new Error('预算单价文件不属于本次执行');
 task.tokenBudget=decode('TokenBudgetAssignment',await readFile(file),{taskId:String(task.taskId),attempt:task.executionAttempt??1});
 checkBudgetBinding(task);
}
