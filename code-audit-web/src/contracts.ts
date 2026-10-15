// One bridge to the generated portable types. The Web domain types keep their
// editable status and database identity separate from the scanner's facts.
export type {ScanResult,ExecutionCompletion,Finding as ContractFinding,ArtifactRef,JsonValue,ProgressSnapshotV2,ScanEventV2,ActivityV2,WorkerObservation,ContractError,AgentRuntime,InputCapabilities,ResultCapabilities,ObservationCapabilities} from '../../contracts/ts/src/types.gen';
export type {TaskBudgetSnapshot,AttemptCost} from '../../contracts/ts/src/types.gen';

export type {StorageSnapshot,StoragePolicy,CleanupJob,CleanupItem} from '../../contracts/ts/src/types.gen';
