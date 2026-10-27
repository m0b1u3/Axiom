// One bridge to the generated portable types. The Web domain types keep their
// editable status and database identity separate from the scanner's facts.
export type {ScanResult,ExecutionCompletion,Finding as ContractFinding,ArtifactRef,JsonValue,ProgressSnapshotV2,ScanEventV2,ActivityV2,WorkerObservation,ContractError,AgentRuntime,InputCapabilities,ResultCapabilities,ObservationCapabilities} from '../../contracts/ts/src/types.gen';
export type {TaskBudgetSnapshot,AttemptCost} from '../../contracts/ts/src/types.gen';

export type {StorageSnapshot,StoragePolicy,CleanupJob,CleanupItem} from '../../contracts/ts/src/types.gen';

export type {SDKLifecycle} from '../../contracts/ts/src/types.gen';
export type {SDKUsage} from '../../contracts/ts/src/types.gen';

export type {ScanInput,KnowledgeDocument} from '../../contracts/ts/src/types.gen';
export type {ExecutionRef,SDKNativeResult,SDKRepositoryFinding,SDKNativeExport,SDKCostEstimate,SDKDiagnostics,SDKDiagnostic} from '../../contracts/ts/src/types.gen';

export type {SDKOperation,SDKOperationResult,SDKSessionEvent,SDKSessionSummary} from '../../contracts/ts/src/types.gen';
export type {ScanRecoverySnapshot,ScanRecoveryAction,RecoveredResult} from '../../contracts/ts/src/types.gen';
export type {FindingEvidence,EvidenceGraph,EvidenceNode,EvidenceRelation,EvidenceLocation} from '../../contracts/ts/src/types.gen';

export type {GitDiffTarget,GitDiffAssignment,GitDiffFile,GitDiffPreview,GitDiffContent} from '../../contracts/ts/src/types.gen';

export type {ComponentSource,ComponentPlanDocument,ComponentPlanCoverage,ComponentPlanSnapshot, ComponentPlanView,ComponentPlanVersion,ComponentPlanningOperation,ComponentPlanningResult} from '../../contracts/ts/src/types.gen';

export type {ComponentScanRequest,ComponentScanReceipt,ComponentScanMatching,ComponentScanSnapshot,ComponentScanResult,ComponentFindingBinding} from '../../contracts/ts/src/types.gen';

export type {SDKIdentityRef,SDKIdentitySnapshot,SDKIdentityOperation,SDKTaskIdentity} from '../../contracts/ts/src/types.gen';

export type {ReasoningEffort,SDKScanParameters,SDKParametersAssignment,ComponentSDKInput} from "../../contracts/ts/src/types.gen";

export type {DeepResumeSnapshot,DeepResumeAction,DeepResumeInspection,DeepResumeAssignment} from '../../contracts/ts/src/types.gen';

export type {InterimCandidate,InterimDocument,InterimDocumentRef,InterimObservation,InterimSnapshot,InterimPage} from '../../contracts/ts/src/types.gen';

export type {InterimReviewInput,InterimReviewEnvelope,InterimReviewRequest,InterimReviewOutcome,InterimReviewArchive,InterimReviewMember} from '../../contracts/ts/src/types.gen';

export type { InterimReviewSourceCheck } from "../../contracts/ts/src/types.gen";

export type {StructuredDiagnosticCatalog,StructuredDiagnosticRequest,StructuredDiagnosticRecord,StructuredDiagnosticPage,StructuredDiagnosticCheck,StructuredDiagnosticProfile,StructuredDiagnosticProtocol,StructuredDiagnosticScenario} from '../../contracts/ts/src/types.gen';

export type {ModelPricing,ModelPricingSnapshot,ExecutionTokenUsage,TokenCostEstimate} from '../../contracts/ts/src/types.gen';

export {validate} from "../../contracts/ts/src/index";
