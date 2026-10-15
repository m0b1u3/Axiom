import type {ProgressSnapshotV2} from '../contracts';
import type {Progress} from '../types';

// The Web consumes the generated contract. Legacy flattened observations are
// isolated here for tasks whose stored format predates the observation contract.
export function progressView(snapshot:ProgressSnapshotV2|null|undefined,legacy?:string):Progress|null {
 if(snapshot){return {taskId:snapshot.execution.taskId,phase:snapshot.phase,rawPhase:snapshot.rawPhase,filesCompleted:snapshot.files?.completed,filesTotal:snapshot.files?.total,activity:snapshot.activity?.description,updatedAt:snapshot.observedAt,progressSource:snapshot.source,progressReportedAt:snapshot.reportedAt??undefined,phaseProgress:snapshot.phaseCounts??undefined,deep:snapshot.deep??undefined,worker:snapshot.workerStatus??undefined,reconnect:snapshot.reconnect?{...snapshot.reconnect,updatedAt:snapshot.reconnectAt??snapshot.observedAt}:undefined,costUsd:snapshot.cost?.estimatedUsd,snapshot};}
 if(!legacy)return null;try{const p=JSON.parse(legacy);if(p.schemaVersion!==undefined)return null;return p as Progress;}catch{return null;}
}
export const progressSources:Record<string,string>={engine_callback:'SDK 回调',engine_observer:'SDK 工作台观察',scanner:'扫描器'};
