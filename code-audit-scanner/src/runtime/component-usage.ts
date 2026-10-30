import {validate,type ExecutionTokenUsage,type ModelPricingSnapshot,type TokenCostEstimate} from '@axiom/contracts';
import {budgetMicros} from './token-budget.js';

/** Components already require a frozen USD budget. Reuse its exact decimal
 * calculator for matching; this is an estimate, not an extra spending limit. */
export function componentMatchingEstimate(snapshot:ModelPricingSnapshot,usage:ExecutionTokenUsage|undefined):TokenCostEstimate {
 validate('ModelPricingSnapshot',snapshot,snapshot.execution);
 if(usage)validate('ExecutionTokenUsage',usage,snapshot.execution);
 const price=snapshot.pricing;
 const estimate:TokenCostEstimate={schemaVersion:1,execution:snapshot.execution,method:'configured_token_prices',pricingSnapshot:structuredClone(snapshot),
  usage:usage?structuredClone(usage):null,status:'unknown',currency:price?.currency??null,amountMicros:null,reason:snapshot.reason||'price_missing',computedAt:new Date().toISOString()};
 if(!price||price.currency!=='USD'||price.inputPerMillion===null||price.outputPerMillion===null||
  (price.cacheMode!=='input_only'&&price.cacheReadPerMillion===null)||(price.cacheMode==='cache_read_write'&&price.cacheWritePerMillion===null))return estimate;
 estimate.reason='usage_missing';
 if(!usage||usage.inputTokens===null||usage.outputTokens===null)return estimate;
 if(price.cacheMode!=='input_only'&&usage.cachedInputTokens===null){estimate.reason='cache_read_usage_missing';return estimate;}
 if(price.cacheMode==='cache_read_write'&&usage.cacheWriteInputTokens===null){estimate.reason='cache_write_usage_missing';return estimate;}
 try{
  const amount=budgetMicros(price,usage);
  if(amount>BigInt(Number.MAX_SAFE_INTEGER)){estimate.reason='amount_out_of_range';return estimate;}
  estimate.amountMicros=Number(amount);estimate.status=usage.complete?'estimated':'partial';estimate.reason=usage.complete?'':'usage_incomplete';
 }catch{estimate.reason='usage_invalid';}
 validate('TokenCostEstimate',estimate,snapshot.execution);
 return estimate;
}
