import type {InterimReviewSourceCheck,InterimReviewInput,InterimReviewOutcome,InterimReviewArchive,InterimReviewMember} from '../contracts';
export interface InterimReviewHistory {taskId:number;attempt:number;status:string;inputSha256:string;outcome:InterimReviewOutcome|null;report:string;error:string}
export interface InterimReviewView {groupId:number;sourceCheck?:InterimReviewSourceCheck|null;input:InterimReviewInput;inputSha256:string;history:InterimReviewHistory[];sources:{execution:{taskId:string;attempt:number};inputSha256:string;members:InterimReviewMember[]}[];archive:InterimReviewArchive|null;findingId:number}
const digest=(v:unknown)=>typeof v==='string'&&/^[a-f0-9]{64}$/.test(v);
export function reviewView(v:InterimReviewView,group:number):InterimReviewView|null {
 if(!v||v.groupId!==group||!v.input||v.input.schemaVersion!==1||v.input.reproduced!==false||!digest(v.input.sourceContentHash)||!digest(v.inputSha256)||!Array.isArray(v.input.members)||!v.input.members.length||!Array.isArray(v.history)||!Array.isArray(v.sources))return null;
 for(const h of v.history){if(!Number.isSafeInteger(h.taskId)||h.taskId<1||!Number.isSafeInteger(h.attempt)||!digest(h.inputSha256)||typeof h.report!=='string')return null;const o=h.outcome;if(o&&(o.schemaVersion!==1||o.execution.taskId!==String(h.taskId)||o.execution.attempt!==h.attempt||o.sourceContentHash!==v.input.sourceContentHash||o.inputSha256!==h.inputSha256||o.reproduced!==false||o.assessment!=='source_review'||!digest(o.reportSha256)||!['reportable','suppressed','not_applicable','deferred'].includes(o.disposition)))return null}
 const check=v.sourceCheck;if(check&&(check.schemaVersion!==1||check.sourceContentHash!==v.input.sourceContentHash||check.reportSha256!==v.history[0]?.outcome?.reportSha256||!['checked','blocked','unavailable','no_references'].includes(check.state)||!Array.isArray(check.references)))return null;
 if(v.archive&&(v.archive.groupId!==group||v.archive.reproduced!==false||v.archive.assessment!=='sdk_reportable'||v.archive.sourceContentHash!==v.input.sourceContentHash||v.archive.disposition!=='reportable'||!v.history.some(h=>h.taskId===Number(v.archive!.execution.taskId)&&h.attempt===v.archive!.execution.attempt&&h.outcome?.reportSha256===v.archive!.reportSha256)))return null;
 return v;
}
// getRandomValues is available on the current HTTP deployment; randomUUID and
// subtle are secure-context APIs and cannot be assumed on an IP over HTTP.
export function reviewRequestId():string {const b=crypto.getRandomValues(new Uint8Array(16));b[6]=(b[6]!&15)|64;b[8]=(b[8]!&63)|128;const h=Array.from(b,n=>n.toString(16).padStart(2,'0')).join('');return `${h.slice(0,8)}-${h.slice(8,12)}-${h.slice(12,16)}-${h.slice(16,20)}-${h.slice(20)}`;}
