import type {ScanOptions} from '@openai/codex-security';
import type {SDKLifecycle} from '@axiom/contracts';

export type LifecycleReporter = (snapshot: SDKLifecycle, message: string, warning: boolean) => void;
type LifecycleCallbacks = Pick<ScanOptions, 'onAuthentication' | 'onTrustedAccessStatus' | 'onScanStarted' | 'onOutputDirReady' | 'onOutputArchived' | 'onBudgetApproaching'>;

/** Public SDK callbacks only. Never retain credentials, account details or host
 * paths. An authentication source is not proof that authentication succeeded. */
export function sdkLifecycleCallbacks(report: LifecycleReporter): LifecycleCallbacks {
  const state: SDKLifecycle = {schemaVersion:1, observedAt:new Date().toISOString(), authentication:null,
    trustedAccess:null, outputReady:false, outputArchived:false, scanStarted:false, budgetApproaching:null};
  const emit = (message:string, warning=false) => {
    state.observedAt=new Date().toISOString();
    report(structuredClone(state), message, warning);
  };
  return {
    onAuthentication(authentication) {
      state.authentication={method:authentication.method, verified:false,
        ...(authentication.method==='stored_credentials' && authentication.credentialType ? {credentialType:authentication.credentialType} : {})};
      const labels={command:'认证命令',api_key:'API Key',stored_credentials:'已保存凭据',aws_credentials:'AWS 凭据'};
      emit(`SDK 已识别认证来源：${labels[authentication.method]}；尚未验证访问权限`);
    },
    onTrustedAccessStatus(status) {
      state.trustedAccess=status;
      emit(`SDK Trusted Access：${{granted:'已授予',not_granted:'未授予',unknown:'未知'}[status]}`);
    },
    onOutputDirReady() {state.outputReady=true;emit('SDK 扫描产物目录已就绪');},
    onOutputArchived() {state.outputArchived=true;emit('SDK 已归档原输出目录');},
    onScanStarted() {state.scanStarted=true;emit('SDK 已正式开始扫描');},
    onBudgetApproaching(budget) {
      // Returning a new amount would raise the SDK limit behind the task ledger.
      // Keep the approved per-attempt limit; an unknown estimate is never zero.
      state.budgetApproaching={maxCostUsd:budget.maxCostUsd,
        estimatedUsd:Number.isFinite(budget.cost.estimatedUsd)&&budget.cost.estimatedUsd>=0?budget.cost.estimatedUsd:null,
        action:'retain_limit',observedAt:new Date().toISOString()};
      emit('SDK 估算费用接近本次上限；保持已批准的额度',true);
      return undefined;
    },
  };
}
