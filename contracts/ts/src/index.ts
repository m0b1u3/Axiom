import {Ajv2020} from 'ajv/dist/2020.js';
import formatsPlugin from 'ajv-formats';
import type {FormatsPlugin} from 'ajv-formats';
import type {ValidateFunction} from 'ajv';
import {SCHEMA_RESOURCES, SCHEMA_IDS} from './schemas.gen.js';
import {COMPATIBILITY} from './types.gen.js';
import type {ContractKind, ContractTypes, ExecutionRef, ErrorCode, TerminalStatus} from './types.gen.js';

export * from './types.gen.js';
export * from './matrix.gen.js';

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
const byteLimit = (kind: string): number => ['ProgressSnapshot','ProgressSnapshotV2','ExecutionHeartbeat'].includes(kind) ? COMPATIBILITY.limits.progressBytes :
  ['ScanEvent','ScanEventV2'].includes(kind) ? COMPATIBILITY.limits.eventBytes : COMPATIBILITY.limits.documentBytes;
const isObject = (value: unknown): value is Record<string,unknown> => typeof value === 'object' && value !== null && !Array.isArray(value);
const sameExecution = (value: unknown,expected: ExecutionRef): boolean => isObject(value) &&
  value['taskId'] === expected.taskId && value['attempt'] === expected.attempt;
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
  if (kind === 'ExecutionCompletion' && isObject(object['result'])) {
    if (!sameExecution(object['result']['execution'],execution as ExecutionRef)) throw new ContractValidationError('identity_mismatch',kind);
  }
  if(kind==='ExecutionHeartbeat' && isObject(object['progress']) && !sameExecution(object['progress']['execution'],execution as ExecutionRef))throw new ContractValidationError('identity_mismatch',kind);
  if(kind==='ExecutionCompletion' && (object['process'] as Record<string,unknown>)['resultCommitted']!==Object.hasOwn(object,'result'))throw new ContractValidationError('invalid_contract',kind);
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
