/** SDK 0.5 can report a baseline plus wider or unbounded token-cost bounds.
 * Preserve the native object; the baseline alone cannot settle a total. */
export function settledSDKCost(cost:unknown):number|null{
 if(!cost||typeof cost!=='object'||Array.isArray(cost))return null;
 const c=cost as Record<string,unknown>,value=c.estimatedUsd;
 if(typeof value!=='number'||!Number.isFinite(value)||value<0)return null;
 if(c.estimatedUsdRange!==undefined){
  const r=c.estimatedUsdRange;
  if(!r||typeof r!=='object'||Array.isArray(r))return null;
  const range=r as Record<string,unknown>;
  if(range.min!==value||range.max!==value)return null;
 }
 return value;
}
