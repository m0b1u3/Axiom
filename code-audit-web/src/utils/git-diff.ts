import type {GitDiffAssignment,GitDiffTarget} from '../contracts';
const object=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v);
export function gitTarget(v:unknown):v is GitDiffTarget{
 return object(v)&&v.schemaVersion===1&&v.kind==='refs'&&v.objectFormat==='sha1'&&['baseCommit','headCommit','baseTree','headTree'].every(k=>typeof v[k]==='string'&&/^[a-f0-9]{40}$/.test(v[k] as string))&&v.baseCommit!==v.headCommit&&['diffSha256','headContentHash'].every(k=>typeof v[k]==='string'&&/^[a-f0-9]{64}$/.test(v[k] as string));
}
export function sameGitTarget(a:unknown,b:unknown):boolean{
 return gitTarget(a)&&gitTarget(b)&&(['schemaVersion','kind','objectFormat','baseCommit','headCommit','baseTree','headTree','diffSha256','headContentHash'] as const).every(k=>a[k]===b[k]);
}
export function gitEvidenceBinding(value:unknown,taskId:number,attempt:number,sourceHash?:string,target?:GitDiffTarget):GitDiffAssignment|null{
 if(!object(value)||value.schemaVersion!==1||value.evidenceRevision!=='head'||!object(value.execution)||value.execution.taskId!==String(taskId)||value.execution.attempt!==attempt||typeof value.sourceSnapshotId!=='string'||!/^[1-9][0-9]{0,18}$/.test(value.sourceSnapshotId)||typeof value.sourceContentHash!=='string'||!/^[a-f0-9]{64}$/.test(value.sourceContentHash)||(sourceHash&&value.sourceContentHash!==sourceHash)||!gitTarget(value.target)||(target&&!sameGitTarget(target,value.target)))return null;
 return value as GitDiffAssignment;
}
