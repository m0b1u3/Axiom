import type {ScanRecoverySnapshot,ScanRecoveryAction} from '../contracts';

const object=(v:unknown):v is Record<string,unknown>=>v!==null&&typeof v==='object'&&!Array.isArray(v);
const digest=(v:unknown)=>typeof v==='string'&&/^[a-f0-9]{64}$/.test(v);
const reason=(v:unknown)=>typeof v==='string'&&v.length>0&&v.length<=2048;

// A future or mismatched recovery response must never enable a write. Keep
// historical readers independent from this new, optional capability.
export function recoveryView(value:unknown,taskId:number,attempt:number,status:string):ScanRecoverySnapshot|null {
 if(!object(value)||value.schemaVersion!==1||value.sdkVersion!=='0.1.25'||value.taskStatus!==status||!object(value.execution)||value.execution.taskId!==String(taskId)||value.execution.attempt!==attempt)return null;
 if(!Number.isSafeInteger(attempt)||attempt<0||typeof value.observedAt!=='string'||!Number.isFinite(Date.parse(value.observedAt)))return null;
 const checkpoint=value.checkpointResume,result=value.committedResult,restart=value.restart,interruption=value.interruption;
 if(!object(checkpoint)||checkpoint.supported!==false||!reason(checkpoint.reason)||!object(result)||!['recoverable','archived','missing','invalid','blocked','unsupported'].includes(String(result.state))||!reason(result.reason)||(result.sha256!==null&&!digest(result.sha256))||(result.state==='recoverable'&&!digest(result.sha256)))return null;
 if(!object(restart)||typeof restart.allowed!=='boolean'||!reason(restart.reason)||!object(interruption)||typeof interruption.allowed!=='boolean'||!reason(interruption.reason))return null;
 if(value.recovery!==null){const r=value.recovery;if(!object(r)||!digest(r.commitSha256)||typeof r.recoveredAt!=='string'||!Number.isFinite(Date.parse(r.recoveredAt))||!reason(r.recoveredBy)||typeof r.previousStatus!=='string'||typeof r.previousErrorCode!=='string'||typeof r.previousErrorMessage!=='string')return null;
  if(r.previousCompletion!==null&&(!object(r.previousCompletion)||!object(r.previousCompletion.execution)||r.previousCompletion.execution.taskId!==String(taskId)||r.previousCompletion.execution.attempt!==attempt))return null;
  if(r.previousCost!==null&&(!object(r.previousCost)||!object(r.previousCost.execution)||r.previousCost.execution.taskId!==String(taskId)||r.previousCost.execution.attempt!==attempt))return null;
 }
 return value as unknown as ScanRecoverySnapshot;
}

export function recoveryAction(snapshot:ScanRecoverySnapshot,action:ScanRecoveryAction['action']):ScanRecoveryAction|null {
 if(!['restore_result','restart','interrupt_expired'].includes(action))return null;
 if(action==='restore_result'&&(snapshot.committedResult.state!=='recoverable'||!snapshot.committedResult.sha256))return null;
 if(action==='restart'&&!snapshot.restart.allowed)return null;
 if(action==='interrupt_expired'&&!snapshot.interruption.allowed)return null;
 return {schemaVersion:1,execution:snapshot.execution,expectedStatus:snapshot.taskStatus,action,resultSha256:action==='restore_result'?snapshot.committedResult.sha256:null,acknowledgeReexecution:action==='restart'};
}
