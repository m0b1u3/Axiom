import type {ContractFinding,ScanResult as ContractResult,ExecutionCompletion,ProgressSnapshotV2,ScanEventV2,ContractError,AgentRuntime,InputCapabilities,ResultCapabilities,ObservationCapabilities} from "./contracts";
export interface User {
  id: number;
  username: string;
  role: 'admin' | 'operator' | 'viewer';
  active: boolean;
  permissions: ('read' | 'operate' | 'admin')[];
}

export interface LoginSession {
  id: string;
  createdAt: string;
  expiresAt: string;
  revokedAt: string;
  current: boolean;
}
export type Account = Omit<User, 'permissions'>;
export interface OperationRecord {
  id: string;
  actorId: number | null;
  actorName: string;
  action: string;
  resource: string;
  resourceId: string;
  outcome: 'started' | 'succeeded' | 'failed' | 'denied';
  occurredAt: string;
  completedAt: string;
}

export interface Project {
  id: number;
  name: string;
  createdBy: number;
  snapshotCount: number;
  scanCount: number;
  createdAt: string;
}

export interface ScanAttempt {
 observationContractVersion?:number;progress?:ProgressSnapshotV2|null;progressDiagnostic?:ContractError;
 queuedAt: string | null; resultAvailable: boolean; findingCount: number;
  taskId: number; attempt: number; agentId: string; status: string;
  startedAt: string; finishedAt: string; errorCode: string; errorMessage: string; progressJson: string;
}

export interface ScanTask {
  maxCostUsd?: number | null;
  workers?: number | null;
  inputContractVersion?: number;
  resultContractVersion?: number;
  observationContractVersion?:number;
  progress?:ProgressSnapshotV2|null;
  progressDiagnostic?:ContractError;
  attempt: number;
  id: number;
  scanNo: string;
  sourcePath: string;
  provider: string;
  model: string;
  baseUrl: string;
  scanMode: string;
  timeoutMinutes: number;
  deepMaxDiscoveryRuns?: number;
  deepStopAfterNoNew?: number;
  validationMode?: string;
  businessContext?: string;
  status: string;
  currentStage: string;
  progressJson: string;
  findingCount: number;
  errorCode: string;
  errorMessage: string;
  projectId: number;
  projectName: string;
  createdAt: string;
  startedAt: string;
  finishedAt: string;
}

export interface Finding {
  normalized?:ContractFinding;
  id: number;
  scanId: number;
  fingerprint: string;
  title: string;
  severity: string;
  category: string;
  cwe: string[];
  filePath: string;
  startLine: number | null;
  endLine: number | null;
  description: string;
  evidence: string;
  remediation: string;
  confidence: number | null;
  status: string;
  /** 上游 Codex Security finding 完整 JSON（未裁剪，展示"全部内容"以它为准） */
  raw: unknown;
}

export interface FindingSummary {
  id: number;
  scanId: number;
  fingerprint: string;
  title: string;
  severity: string;
  category: string;
  cwe: string[];
  filePath: string;
  startLine: number | null;
  endLine: number | null;
  description: string;
  status: string;
  raw: unknown;
  firstSeenScanId: number;
  firstSeenAt: string;
  lastSeenAt: string;
}

export interface ScanCompare {
  previousScanId: number | null;
  added: Finding[];
  removed: Finding[];
  unchanged: number;
}

export interface DashboardStats {
  projects: number;
  scans: number;
  running: number;
  failed: number;
  critical: number;
  high: number;
  medium: number;
  low: number;
  info: number;
  openFindings: number;
  criticalOpen: number;
  successRate: number;
  avgDurationSec: number;
}

export interface SeverityCounts {
  critical: number;
  high: number;
  medium: number;
  low: number;
  info: number;
}

/** 概览最近记录/最近一次扫描：任务行 + 严重度计数 + 耗时 */
export interface DashboardScan extends ScanTask {
  sev: SeverityCounts;
  durationSec: number;
}

export interface TrendPoint extends SeverityCounts {
  date: string; // YYYY-MM-DD
}

export interface ProjectRisk {
  high: number;
  medium: number;
  low: number;
  none: number;
}

export interface EngineInfo {
  provider: string;
  model: string;
  baseUrl: string;
  avgDurationSec: number;
  durationSec: number;
  scanCount: number;
}

export interface DashboardDeltas {
  findings?: number;
  critical?: number;
  durationPct?: number;
}

export interface Dashboard {
  stats: DashboardStats;
  recent: DashboardScan[];
  latestDelta: number;
  latest: DashboardScan | null;
  deltas: DashboardDeltas | null;
  trend: TrendPoint[];
  projectRisk: ProjectRisk;
  engine: EngineInfo | null;
}

export interface ScanLogEvent {
 event?:ScanEventV2;
  n: number;
  timestamp: string;
  level: string;
  kind: string;
  message: string;
}

export interface Progress {
 snapshot?:ProgressSnapshotV2;
 rawPhase?:string;
  progressSource?: string;
  progressReportedAt?: string;
  phaseProgress?: {completed:number|null;total:number|null;unit:string|null;basis?:string};
  deep?: {completed:number|null;active:number|null;maximum:number|null;updatedAt?:string};
  worker?: {kind?:string;phase?:string;delegation?:string;configuredSlots?:number|null;planned?:number;started?:number};
  reconnect?: {attempt:number;maximum:number;reason:string;retryAfterSeconds?:number;updatedAt:string};
  taskId: number|string;
  phase: string;
  filesCompleted?: number|null;
  filesTotal?: number|null;
  activity?: string;
  costUsd?: number|null;
  updatedAt: string;
}

export interface QueueSettings { global: number; perModel: number; perProject: number; revision: number; updatedAt: string; updatedBy: string }
export interface AgentPresence { agentId: string; slots: number; lastSeen: string; online: boolean; runtimeReportedAt?:string; runtime?:AgentRuntime; inputCapabilities?:InputCapabilities; resultCapabilities?:ResultCapabilities; observationCapabilities?:ObservationCapabilities }
export interface TaskCompatibility {required:{input:number;result:number;observation:number;budget:boolean};profile:string;reason:string;candidates:{agentId:string;online:boolean;ready:boolean;missing:string[];diagnostic?:ContractError}[]}
export interface QueueTask { compatibility?:TaskCompatibility; taskId: number; status: string; agentId: string; queuedAt: string; startedAt: string; finishedAt: string; heartbeatAt: string; leaseExpiresAt: string; position: number; reason: string; automaticRetries: number }
export interface QueueOverview { maxRecoveries?:number; settings: QueueSettings; agents: AgentPresence[]; tasks: QueueTask[]; active: number; pending: number; onlineSlots: number; readySlots?: number; effectiveCapacity: number; serverTime: string }
export interface AttemptResult {contractVersion?:number;result?:ContractResult|null;completion?:ExecutionCompletion|null;archiveSha256?:string; available: boolean; findings: Finding[]; coverage?: Record<string,unknown>|null }
export interface CoverageResponse {available:boolean;coverage:Record<string,unknown>|null;attempt:number;status?:string;source:string;reason:string}
