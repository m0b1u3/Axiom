import type {SDKLifecycle} from '../contracts';

/** Optional extension: old snapshots stay readable; malformed/new versions are
 * reported separately rather than displayed as a successful authentication. */
export function sdkLifecycleView(value:unknown): {value:SDKLifecycle|null;invalid:boolean} {
 if(value===undefined)return {value:null,invalid:false};
 const bad={value:null,invalid:true};
 if(!value||typeof value!=='object'||Array.isArray(value))return bad;
 const v=value as SDKLifecycle;
 if(v.schemaVersion!==1||typeof v.observedAt!=='string'||!Number.isFinite(Date.parse(v.observedAt)))return bad;
 if(!['outputReady','outputArchived','scanStarted'].every(k=>typeof (value as Record<string,unknown>)[k]==='boolean'))return bad;
 if(v.trustedAccess!==null&&!['granted','not_granted','unknown'].includes(v.trustedAccess))return bad;
 if(v.authentication!==null){
  const a=v.authentication;
  if(!a||!['command','api_key','stored_credentials','aws_credentials'].includes(a.method)||a.verified!==false)return bad;
  if(a.credentialType!==undefined&&!['api_key','chatgpt'].includes(a.credentialType))return bad;
 }
 if(v.budgetApproaching!==null){const b=v.budgetApproaching;
  if(!b||b.action!=='retain_limit'||!Number.isFinite(b.maxCostUsd)||b.maxCostUsd<=0||typeof b.observedAt!=='string'||!Number.isFinite(Date.parse(b.observedAt)))return bad;
  if(b.estimatedUsd!==null&&(!Number.isFinite(b.estimatedUsd)||b.estimatedUsd<0))return bad;
 }
 return {value:v,invalid:false};
}
export const authenticationMethods={command:'认证命令',api_key:'API Key',stored_credentials:'已保存凭据',aws_credentials:'AWS 凭据'};
export const trustedAccessStates={granted:'已授予',not_granted:'未授予',unknown:'未知'};
