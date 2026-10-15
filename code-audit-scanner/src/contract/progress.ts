/**
 * 扫描进度合同。Agent 轮询文件，不要解析 stderr。
 *
 * 路径：
 * - state/progress.json  当前快照（整文件覆盖）
 * - logs/events.jsonl    追加事件，一行一个 JSON
 */

export type ScanPhase =
  | "preparing"
  | "preflight"
  | "threat_model"
  | "discovery"
  | "validation"
  | "attack_path"
  | "reporting"
  | "processing"
  | "partial"
  | "completed"
  | "failed"
  | "canceled";

export interface ScanProgressSnapshot {
  taskId: number | string;
  phase: ScanPhase;
  filesCompleted?: number;
  filesTotal?: number;
  deep?: { completed: number; active: number; maximum: number; updatedAt: string };
  worker?: Record<string, unknown>;
  reconnect?: { attempt: number; maximum: number; reason: string; retryAfterSeconds?: number; updatedAt: string };
  activity?: string;
  costUsd?: number;
  updatedAt: string;
}

export type ScanLogLevel = "info" | "warn" | "error";

export type ScanLogKind =
  | "progress"
  | "activity"
  | "session" // SDK onSessionEvent（thread/worker 维度，含推理摘要）
  | "worker"
  | "cost"
  | "warning"
  | "system";

export interface ScanLogEvent {
  timestamp: string;
  level: ScanLogLevel;
  kind: ScanLogKind;
  message: string;
}
