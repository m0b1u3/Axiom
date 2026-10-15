import {decode, validate} from '../src/index.js';
import type {ExecutionRef, Finding, ScanEvent} from '../src/index.js';

const ref: ExecutionRef={taskId:'9007199254740993',attempt:1};
// @ts-expect-error database IDs cannot cross the boundary as JS numbers
const numeric: ExecutionRef={taskId:42,attempt:1};
const input=decode('ScannerJob','{}');
input.policy.mode;
// @ts-expect-error ScannerJob has no database row fields
input.projectName;
const unknown: unknown={};
validate('ScanResult',unknown);
unknown.findings;
function narrow(event: ScanEvent) {
  if(event.type==='scan.phase_changed') {
    event.data.phase;
    // @ts-expect-error a phase event cannot carry cost payload fields
    event.data.estimatedUsd;
  }
}
const valid: Finding={fingerprint:'fixture',title:'fixture',severity:'info',description:'fixture'};
// @ts-expect-error severity is an enum, not an arbitrary display string
const invalid: Finding={fingerprint:'fixture',title:'fixture',severity:'urgent',description:'fixture'};
void [ref,numeric,valid,invalid,narrow];
