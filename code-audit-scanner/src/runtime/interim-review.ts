import {createHash} from 'node:crypto';
import {decode,type InterimReviewEnvelope,type InterimReviewInput,type ExecutionRef} from '@axiom/contracts';
export const reviewDigest=(raw:string)=>createHash('sha256').update(raw,'utf8').digest('hex');
export function frozenInterimReview(raw:unknown,source:string,origin:ExecutionRef|null):{envelope:InterimReviewEnvelope;input:InterimReviewInput}|null {
 if(!raw||typeof raw!=='object'||!('axiomInterimReview' in raw))return null;
 const envelope=decode('InterimReviewEnvelope',JSON.stringify((raw as Record<string,unknown>).axiomInterimReview));
 if(reviewDigest(envelope.inputJson)!==envelope.inputSha256)throw new Error('Frozen candidate review input digest mismatch');
 const input=decode('InterimReviewInput',envelope.inputJson);
 if(input.sourceContentHash!==source||!origin||!input.members.some(m=>m.execution.taskId===origin.taskId&&m.execution.attempt===origin.attempt))throw new Error('Frozen candidate review source or origin mismatch');
 const docs=new Map<string,ReturnType<typeof decode<'InterimDocument'>>>();
 for(const d of input.documents){
  const key=[d.execution.taskId,d.execution.attempt,d.artifact.sha256].join(':');
  if(docs.has(key)||d.artifact.kind!=='engine_raw'||d.artifact.relativePath!==`checkpoints/interim/objects/${d.artifact.sha256}.json`||Buffer.byteLength(d.documentJson,'utf8')!==d.artifact.sizeBytes||reviewDigest(d.documentJson)!==d.artifact.sha256)throw new Error('Frozen candidate evidence integrity mismatch');
  const value=decode('InterimDocument',d.documentJson,d.execution);if(value.sourceContentHash!==source)throw new Error('Frozen candidate evidence source mismatch');docs.set(key,value);
 }
 const seen=new Set<string>();
 for(const m of input.members){const key=[m.execution.taskId,m.execution.attempt,m.candidate.artifact].join(':'),doc=docs.get(key),id=key+':'+m.candidate.id;if(!doc||seen.has(id)||reviewDigest(doc.sourcePath+'\0'+m.candidate.pointer)!==m.candidate.id)throw new Error('Frozen candidate provenance mismatch');seen.add(id)}
 if(input.grouping==='manual'&&!input.groupingReason.trim())throw new Error('Manual review grouping lacks rationale');
 return {envelope,input};
}

/** Full documents stay frozen for provenance. Send only selected records to
 * validate(), and collapse identical facts while retaining every origin ref. */
export function interimReviewModelFinding(review:{input:InterimReviewInput;envelope:InterimReviewEnvelope}, inventory?:Awaited<ReturnType<typeof interimReviewSourceInventory>>) {
 const input=review.input,documents=new Map(input.documents.map(d=>[[d.execution.taskId,d.execution.attempt,d.artifact.sha256].join(':'),decode('InterimDocument',d.documentJson,d.execution)]));
 const candidates:Array<{finding:InterimReviewInput['members'][number]['candidate'];selectedRecord:unknown;evidenceState:'selected_record'|'projection_only';origins:Array<{execution:ExecutionRef;candidateId:string;artifactSha256:string;sourceSha256:string;pointer:string}>}>=[];
 const groups=new Map<string,number>();
 for(const member of input.members){
  const c=member.candidate;
  const doc=documents.get([member.execution.taskId,member.execution.attempt,c.artifact].join(':'))!;
  const origin={execution:member.execution,candidateId:c.id,artifactSha256:c.artifact,sourceSha256:doc.sourceSha256,pointer:c.pointer};
  let selectedRecord:unknown=null;
  try{const fenced=/^\s*```(?:json)?\s*\n([\s\S]*)\n```\s*$/.exec(doc.content);let value:unknown=JSON.parse(fenced?.[1]??doc.content);for(const escaped of c.pointer.split('/').slice(1)){const part=escaped.replaceAll('~1','/').replaceAll('~0','~');if(!value||typeof value!=='object'||!Object.hasOwn(value,part))throw new Error('Selected record unavailable');value=(value as Record<string,unknown>)[part]};selectedRecord=value;}catch{/* Preserve the explicit candidate projection and mark the missing record. */}
  // A matching projection alone is insufficient: retain differing native evidence.
  const key=JSON.stringify([c.title.trim(),c.summary?.trim()??'',c.reportedSeverity??'',[...c.evidence].sort(),c.truncated,c.omittedEvidence,selectedRecord]);
  const prior=groups.get(key);if(prior!==undefined){candidates[prior]!.origins.push(origin);continue;}
  groups.set(key,candidates.length);candidates.push({finding:c,selectedRecord,evidenceState:selectedRecord===null?'projection_only':'selected_record',origins:[origin]});
 }
 return {schemaVersion:1,...(inventory?{frozenSourceInventory:inventory}:{}),title:input.members[0]!.candidate.title,sourceContentHash:input.sourceContentHash,inputSha256:review.envelope.inputSha256,grouping:input.grouping,groupingReason:input.groupingReason,candidates,reproduced:false,evidenceBoundary:'Selected saved records only. Complete hashed documents remain in the host archive. Source-only review; no runtime reproduction.'};
}

/** Bounded host observations, not model claims. No source content or credentials. */
export async function interimReviewSourceInventory(root:string){
 const {readdir}=await import('node:fs/promises');const path=await import('node:path');const files:string[]=[];let complete=true,bytes=0;
 async function visit(relative:string,depth:number):Promise<void>{
  if(depth>16){complete=false;return;}
  for(const item of (await readdir(path.join(root,relative),{withFileTypes:true})).sort((a,b)=>a.name.localeCompare(b.name))){
   if(item.name==='.git')continue;const next=relative?relative+'/'+item.name:item.name;
   if(item.isSymbolicLink()){complete=false;continue;}
   if(files.length>=128||bytes+next.length>16384){complete=false;return;}
   if(item.isDirectory())await visit(next,depth+1);else if(item.isFile()){files.push(next);bytes+=next.length;}
  }
 }
 await visit('',0);return {schemaVersion:1,complete,files,meaning:'Host-observed paths in this frozen source. Cite only files actually read; an injected interface does not prove its implementation or runtime behavior. A partial list does not establish file absence.'};
}
