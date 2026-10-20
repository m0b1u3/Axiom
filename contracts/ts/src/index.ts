import {Ajv2020} from 'ajv/dist/2020.js';
import formatsPlugin from 'ajv-formats';
import type {FormatsPlugin} from 'ajv-formats';
import type {ValidateFunction} from 'ajv';
import {SCHEMA_RESOURCES, SCHEMA_IDS} from './schemas.gen.js';
import {COMPATIBILITY} from './types.gen.js';
import type {ContractKind, ContractTypes, ExecutionRef, ErrorCode, TerminalStatus} from './types.gen.js';

export * from './types.gen.js';
export * from './matrix.gen.js';
export * from './component-scan.js';
export * from './sdk-concurrency.gen.js';
import {componentAllocations,componentReserved} from './component-scan.js';

export class ContractValidationError extends Error {
  constructor(public readonly code: string, public readonly kind: string) {
    super(kind + ': ' + code);
    this.name = 'ContractValidationError';
  }
}

const ajv = new Ajv2020({strict:true, strictTypes:false, strictRequired:false, allErrors:true, coerceTypes:false, useDefaults:false, removeAdditional:false});
(formatsPlugin as unknown as FormatsPlugin)(ajv);
for (const schema of Object.values(SCHEMA_RESOURCES)) ajv.addSchema(schema);
const validators = new Map<string,ValidateFunction>();
for (const [kind,id] of Object.entries(SCHEMA_IDS)) {
  const validator = ajv.getSchema(id);
  if (!validator) throw new Error('Missing contract schema: ' + kind);
  validators.set(kind,validator);
}
const byteLimit = (kind: string): number => ['ProgressSnapshot','ProgressSnapshotV2','ExecutionHeartbeat','SDKDiagnostics'].includes(kind) ? COMPATIBILITY.limits.progressBytes :
  ['ScanEvent','ScanEventV2','SDKDiagnostic'].includes(kind) ? COMPATIBILITY.limits.eventBytes : COMPATIBILITY.limits.documentBytes;
const isObject = (value: unknown): value is Record<string,unknown> => typeof value === 'object' && value !== null && !Array.isArray(value);
const sameExecution = (value: unknown,expected: ExecutionRef): boolean => isObject(value) &&
  value['taskId'] === expected.taskId && value['attempt'] === expected.attempt;
const sameJSONValue=(a:unknown,b:unknown):boolean=>a===b||(Array.isArray(a)&&Array.isArray(b)&&a.length===b.length&&a.every((v,i)=>sameJSONValue(v,b[i])))||(isObject(a)&&isObject(b)&&Object.keys(a).length===Object.keys(b).length&&Object.entries(a).every(([key,value])=>Object.hasOwn(b,key)&&sameJSONValue(value,b[key])));
function wellFormed(text: string): boolean {
  for (let i = 0; i < text.length; i++) {
    const c = text.charCodeAt(i);
    if (c >= 0xd800 && c <= 0xdbff) {
      const next = text.charCodeAt(++i);
      if (!(next >= 0xdc00 && next <= 0xdfff)) return false;
    } else if (c >= 0xdc00 && c <= 0xdfff) {
      return false;
    }
  }
  return true;
}

export function validate<K extends ContractKind>(kind: K,value: unknown,expected?: ExecutionRef): asserts value is ContractTypes[K] {
  const validator = validators.get(kind);
  if (!validator) throw new ContractValidationError('unsupported_kind',kind);
  if (!validator(value)) throw new ContractValidationError('invalid_contract',kind);
  const object = value as Record<string,unknown>;
  const execution = kind === 'ExecutionRef' ? object : object['execution'];
  if (expected && !sameExecution(execution,expected)) throw new ContractValidationError('identity_mismatch',kind);
  if(kind==='FindingEvidence'||kind==='EvidenceGraph')validateEvidenceFields(kind,object);
  if(kind==='ScanRecoverySnapshot'&&isObject(object['recovery'])&&isObject(object['recovery']['previousCompletion']))validate('ExecutionCompletion',object['recovery']['previousCompletion'],execution as ExecutionRef);
  if(kind==='ScanRecoverySnapshot'&&isObject(object['recovery'])&&isObject(object['recovery']['previousCost'])&&!sameExecution(object['recovery']['previousCost']['execution'],execution as ExecutionRef))throw new ContractValidationError('identity_mismatch',kind);
  if (kind === 'ExecutionCompletion' && isObject(object['result'])) {
    if (!sameExecution(object['result']['execution'],execution as ExecutionRef)) throw new ContractValidationError('identity_mismatch',kind);
  }
  if(kind==='ExecutionHeartbeat' && isObject(object['progress']) && !sameExecution(object['progress']['execution'],execution as ExecutionRef))throw new ContractValidationError('identity_mismatch',kind);
  if(kind==='ExecutionCompletion' && (object['process'] as Record<string,unknown>)['resultCommitted']!==Object.hasOwn(object,'result'))throw new ContractValidationError('invalid_contract',kind);
  if(kind==='SDKNativeResult'){
    validate('SDKCostEstimate',object['usageEstimate'],execution as ExecutionRef);
    validate('SDKDiagnostics',object['diagnostics'],execution as ExecutionRef);
  }
  if(kind==='SDKDiagnostics'){
    const value=object as unknown as ContractTypes['SDKDiagnostics'],total=value.warningCount+value.observerErrorCount;
    if(!Number.isSafeInteger(total)||value.omittedCount+value.entries.length!==total)throw new ContractValidationError('invalid_contract',kind);
    for(const [index,entry] of value.entries.entries()){
      validate('SDKDiagnostic',entry,execution as ExecutionRef);
      if(entry.sequence!==value.omittedCount+index+1)throw new ContractValidationError('invalid_contract',kind);
    }
    if(value.entries.filter(e=>e.source==='sdk_warning').length>value.warningCount||value.entries.filter(e=>e.source==='observer_error').length>value.observerErrorCount)throw new ContractValidationError('invalid_contract',kind);
  }
  if(kind==='SDKIdentitySnapshot'){
    const v=object as unknown as ContractTypes['SDKIdentitySnapshot'];if(v.state==='authenticated'?(v.authenticated!==true||v.checkedAt===null||v.cleanupPending):v.state==='signed_out'?v.authenticated!==false:v.authenticated!==null)throw new ContractValidationError('invalid_contract',kind);
  }
  if(kind==='SDKIdentityOperation'){
    const v=object as unknown as ContractTypes['SDKIdentityOperation'];if((v.status==='waiting')!==(v.instructions!==null)||(v.status==='waiting'&&v.action!=='login_device_code')||(v.action==='login_api_key'&&v.identity.kind!=='api_key')||(v.action==='login_device_code'&&v.identity.kind!=='chatgpt'))throw new ContractValidationError('invalid_contract',kind);
  }
  if(kind==='SDKIdentityClaim'){
    const v=object as unknown as ContractTypes['SDKIdentityClaim'];validate('SDKIdentityOperation',v.operation);if(v.operation.status!=='running'||Object.hasOwn(v,'apiKey')!==(v.operation.action==='login_api_key'))throw new ContractValidationError('invalid_contract',kind);
  }
  if(kind==='SDKIdentityReport'){
    const v=object as unknown as ContractTypes['SDKIdentityReport'];if((v.status==='waiting')!==(v.instructions!==null)||(v.status==='completed')!==(v.authenticated!==null)||(v.status==='completed'&&v.error!==null))throw new ContractValidationError('invalid_contract',kind);
  }
  validateComponentScan(kind,object,execution as ExecutionRef);
  if(kind==='ComponentPlanView'){
    const v=object as unknown as ContractTypes['ComponentPlanView'],op=v.operation;
    if(!op){if(v.operationTaskId!==null||v.operationStatus!==''||v.operationError!==null||v.result!==null||v.resultSha256!==null)throw new ContractValidationError('invalid_contract',kind);}
    else{
      if(op.execution.taskId!==v.operationTaskId||op.planId!==v.plan.id||!sameJSONValue(op.source,v.plan.source)||op.revision>v.plan.revision)throw new ContractValidationError('identity_mismatch',kind);
      if(v.operationStatus==='')throw new ContractValidationError('invalid_contract',kind);
      if(v.result){validate('ComponentPlanningResult',v.result,op.execution);if(!sameJSONValue(op,v.result.operation)||v.resultSha256===null)throw new ContractValidationError('identity_mismatch',kind);}
      else if(v.resultSha256!==null)throw new ContractValidationError('invalid_contract',kind);
    }
  }
  if(kind==='ScannerJobV7'||kind==='ComponentPlanningResult'){const key=kind==='ScannerJobV7'?'componentPlanning':'operation';validate('ComponentPlanningOperation',object[key],execution as ExecutionRef);if(kind==='ScannerJobV7'){const v=object as unknown as ContractTypes['ScannerJobV7'];if(v.componentPlanning.source.snapshotId!==v.source.snapshotId||v.componentPlanning.source.contentHash!==v.source.contentHash||v.policy.mode!=='standard'||v.policy.includePaths.length||v.policy.maxCostUsd!==undefined)throw new ContractValidationError('invalid_contract',kind);}}
  if(kind==='ComponentPlanCoverage'){const v=object as unknown as ContractTypes['ComponentPlanCoverage'],valid=v.fileCount>0&&v.coveredFileCount===v.fileCount&&!v.overlapCount&&!v.emptyComponents.length&&!v.duplicateNames.length;if(v.coveredFileCount+v.uncoveredPaths.length!==v.fileCount||v.overlaps.length!==Math.min(v.overlapCount,200)||v.valid!==valid)throw new ContractValidationError('invalid_contract',kind);}
  if(kind==='ComponentPlanningResult'&&object['coverage']!==null)validate('ComponentPlanCoverage',object['coverage']);
  if(kind==='ComponentPlanVersion'){validate('ComponentPlanCoverage',object['coverage']);if(!(object as unknown as ContractTypes['ComponentPlanVersion']).coverage.valid)throw new ContractValidationError('invalid_contract',kind);}
  if(kind==='ScannerJobV5'||(kind==='ScannerJobV6'&&object['operation']!==undefined))validate('SDKOperation',object['operation'],execution as ExecutionRef);
  if(kind==='GitDiffTarget'&&object['baseCommit']===object['headCommit'])throw new ContractValidationError('invalid_contract',kind);
  if(kind==='ScannerJobV6')validate('GitDiffTarget',object['gitTarget']);
  if(kind==='GitDiffAssignment'||kind==='GitDiffPreview'||kind==='GitDiffContent')validate('GitDiffTarget',object['target']);
  if(kind==='GitDiffPreview'){
    const v=object as unknown as ContractTypes['GitDiffPreview'],start=v.page-1<=v.total/50?(v.page-1)*50:v.total;
    if(v.files.length!==Math.min(50,v.total-start)||v.files.some((f,i)=>f.index!==start+i))throw new ContractValidationError('invalid_contract',kind);
  }
  if(kind==='SDKSessionSummary'){
    const v=object as unknown as ContractTypes['SDKSessionSummary'];
    if(v.persistedCount+v.omittedCount!==v.receivedCount||(v.state==='complete'&&(v.omittedCount!==0||v.reason!==null))||(v.state==='incomplete'&&!v.reason))throw new ContractValidationError('invalid_contract',kind);
  }
  if(kind==='SDKOperationResult'){
    const v=object as unknown as ContractTypes['SDKOperationResult'];
    if(v.error===null&&((v.kind==='preflight'&&v.preflight===null)||(v.kind==='validate'&&v.validation===null)))throw new ContractValidationError('invalid_contract',kind);
    if(v.kind==='preflight'&&v.preflight!==null&&(!isObject(v.preflight.authentication)||v.preflight.authentication.verified!==false))throw new ContractValidationError('invalid_contract',kind);
  }
  if (kind === 'ExecutionAssignment' && isObject(object['credential'])) {
    const model=object['model'] as Record<string,unknown>;
    if (object['credential']['envName']!==model['credentialEnv']) throw new ContractValidationError('credential_binding_mismatch',kind);
  }
  if (new TextEncoder().encode(JSON.stringify(value)).length > byteLimit(kind)) throw new ContractValidationError('payload_too_large',kind);
}

export function decode<K extends ContractKind>(kind: K,data: string | Uint8Array,expected?: ExecutionRef): ContractTypes[K] {
  if (!validators.has(kind)) throw new ContractValidationError('unsupported_kind',kind);
  const bytes = typeof data === 'string' ? new TextEncoder().encode(data) : data;
  if (bytes.length > byteLimit(kind)) throw new ContractValidationError('payload_too_large',kind);
  let text: string;
  let value: unknown;
  try {
    // Keep a BOM as a character so JSON.parse rejects it, just as Go does.
    text = typeof data === 'string' ? data : new TextDecoder('utf-8',{fatal:true,ignoreBOM:true}).decode(data);
    value = JSON.parse(text,(key: string,current: unknown)=>{
      if(!wellFormed(key) || (typeof current==='string' && !wellFormed(current)) || (typeof current==='number' && !Number.isFinite(current)))throw new SyntaxError('Non-interoperable JSON value');
      return current;
    });
  } catch {throw new ContractValidationError('invalid_json',kind);}
  validate(kind,value,expected);
  return value;
}

export function normalizeLegacyErrorCode(code: string): {code: ErrorCode; recognized: boolean} {
  const aliases: Record<string,ErrorCode> = COMPATIBILITY.legacyErrorAliases;
  if (Object.hasOwn(aliases,code)) return {code:aliases[code]!,recognized:true};
  // The canonical enum is validated through its schema, not inferred from a message.
  const validator=ajv.getSchema('https://axiom.invalid/contracts/v1/definitions.schema.json#/$defs/ErrorCode');
  if (validator?.(code)) return {code:code as ErrorCode,recognized:true};
  return {code:'unknown',recognized:false};
}
export function exitCode(status: TerminalStatus): number {return COMPATIBILITY.exitCodes[status];}

function validateEvidenceFields(kind:ContractKind,value:Record<string,unknown>):void {
  const nodes=value['nodes'] as ContractTypes['EvidenceGraph']['nodes'];
  const edges=value['edges'] as ContractTypes['EvidenceGraph']['edges'];
  const ids=new Set(nodes.map(n=>n.id)),relations=new Set(edges.map(e=>e.id));
  if(ids.size!==nodes.length||relations.size!==edges.length||edges.some(e=>!ids.has(e.from)||!ids.has(e.to)))throw new ContractValidationError('invalid_contract',kind);
  for(const node of nodes){
    if(node.location?.endLine!==null&&node.location?.endLine!==undefined&&(node.location.startLine===null||node.location.endLine<node.location.startLine))throw new ContractValidationError('invalid_contract',kind);
    if(node.state==='observed'&&(node.location?.startLine===null||node.code===null))throw new ContractValidationError('invalid_contract',kind);
  }
  if(kind==='EvidenceGraph'){
    const source=value['sourceFinding'];
    if(!isObject(source)||source['fingerprint']!==value['findingFingerprint'])throw new ContractValidationError('identity_mismatch',kind);
    for(const item of [...nodes,...edges])if(!evidencePointer(source,item.sourcePointer).found)throw new ContractValidationError('invalid_contract',kind);
    for(const node of nodes)if(node.kind==='code_evidence'){
      const origin=evidencePointer(source,node.sourcePointer).value;
      const reported=(v:unknown)=>typeof v==='string'&&v.length>0?v:null;
      if(!isObject(origin)||reported(origin['code'])!==node.code||(node.sdkEvidenceId!==null&&origin['id']!==node.sdkEvidenceId)||(node.location!==null&&(origin['path']!==node.location.path||(origin['startLine']??null)!==node.location.startLine||(origin['endLine']??null)!==node.location.endLine)))throw new ContractValidationError('invalid_contract',kind);
    }
    for(const edge of edges)if(edge.kind==='supports'){
      const from=nodes.find(n=>n.id===edge.from);
      if(from?.sdkEvidenceId===null||from?.sdkEvidenceId!==evidencePointer(source,edge.sourcePointer).value)throw new ContractValidationError('invalid_contract',kind);
    }
  }
}

/** Resolve an RFC 6901 pointer without following prototypes or interpreting text. */
export function evidencePointer(value:unknown,pointer:string):{found:boolean;value?:unknown}{
  if(pointer==='')return {found:true,value};
  if(!pointer.startsWith('/')||/~(?![01])/u.test(pointer))return {found:false};
  let current:unknown=value;
  for(const encoded of pointer.slice(1).split('/')){
    const key=encoded.replaceAll('~1','/').replaceAll('~0','~');
    if(Array.isArray(current)){if(!/^(0|[1-9][0-9]*)$/.test(key)||!Object.hasOwn(current,key))return {found:false};current=current[Number(key)];}
    else if(isObject(current)&&Object.hasOwn(current,key))current=current[key];else return {found:false};
  }
  return {found:true,value:current};
}

function validateComponentScan(kind:ContractKind,object:Record<string,unknown>,execution:ExecutionRef):void {
 const invalid=(code='invalid_contract'):never=>{throw new ContractValidationError(code,kind)};
 if(kind==='ComponentScanRequest'){
  const r=object as unknown as ContractTypes['ComponentScanRequest'];validate('ComponentPlanVersion',r.planVersion);
  if(r.selectedIndices.some((v,i)=>v>=r.planVersion.document.components.length||(i>0&&v<=r.selectedIndices[i-1]!))||componentAllocations(r).some(v=>v<1))invalid();
 }
 if(kind==='ComponentScanSnapshot'){
  const s=object as unknown as ContractTypes['ComponentScanSnapshot'];let sum=0,observed=0;
  const ids=new Set<number>();
  for(const r of s.components){if(ids.has(r.index))invalid();ids.add(r.index);sum+=r.allocatedMicros;observed+=r.observedMicros??0;
   if(!Number.isSafeInteger(sum)||!Number.isSafeInteger(observed)||(r.finalMicros!==null&&(r.observedMicros===null||r.finalMicros>r.observedMicros))||((r.scanId===null)!==(r.resultArtifact===null))||(['completed','partial'].includes(r.status)&&r.resultArtifact===null)||((r.status==='pending'||r.status==='running')&&r.finalMicros!==null))invalid();
  }
  if(sum>s.grantMicros||s.reservedMicros!==componentReserved(s.components)||s.observedMicros!==observed||((s.matching.status==='not_started')!==(s.matching.costState==='known'))||(s.phase==='finished'&&s.components.some(r=>r.status==='pending'||r.status==='running')))invalid();
 }
 if(kind==='ComponentScanResult'){
  const r=object as unknown as ContractTypes['ComponentScanResult'];validate('ComponentScanRequest',r.request,execution);validate('ComponentScanSnapshot',r.snapshot,execution);
  const s=r.snapshot,p=r.request.planVersion,a=componentAllocations(r.request);
  if(s.planId!==p.planId||s.version!==p.version||s.versionSha256!==p.sha256||!sameJSONValue(s.source,p.source)||s.grantMicros!==r.request.grantMicros||s.components.length!==a.length||s.components.some((c,i)=>c.index!==r.request.selectedIndices[i]||c.allocatedMicros!==a[i])||s.phase!=='finished')invalid('identity_mismatch');
 }
 if(kind==='ScannerJobV8'){
  const j=object as unknown as ContractTypes['ScannerJobV8'];validate('ComponentScanRequest',j.componentScan,execution);
  if(j.source.snapshotId!==j.componentScan.planVersion.source.snapshotId||j.source.contentHash!==j.componentScan.planVersion.source.contentHash||j.policy.maxCostUsd===undefined||Math.abs(j.policy.maxCostUsd*1e6-j.componentScan.grantMicros)>0.01)invalid('identity_mismatch');
 }
 if(kind==='ProgressSnapshotV2'&&isObject(object.extensions)&&object.extensions.componentScan!==undefined)validate('ComponentScanSnapshot',object.extensions.componentScan,execution);
 if(kind==='ScanResult'&&isObject(object.extensions)&&object.extensions.componentScan!==undefined)validate('ComponentScanResult',object.extensions.componentScan,execution);
}
