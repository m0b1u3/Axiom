import type { Finding } from "./finding.js";
import type {SDKDiagnostics} from '@axiom/contracts';

export type ScanResultStatus =
  | "completed"
  | "partial"
  | "failed"
  | "canceled"
  | "timeout";

export type CompatibilityTier = "supported" | "degraded" | "unsupported";

export interface ScanEngineInfo {
  name: "codex-security";
  version: string;
  pluginVersion?: string;
}

export interface ScanModelInfo {
  provider: string;
  model: string;
  baseUrl?: string;
}

export interface ScanCoverageInfo {
  [key: string]: unknown;
  completeness?: "complete" | "partial" | "unknown";
  notes?: string;
}

export interface ScanCostInfo {
  /** 上游估算，单位 USD。非官方模型经常为 null。 */
  estimatedUsd?: number;
  inputTokens?: number;
  outputTokens?: number;
}

export interface ScanErrorInfo {
  code: ScanErrorCode;
  message: string;
}

/**
 * Scanner 退出语义（同时用于进程 exit code 与 result.error.code）。
 *
 * 0 completed
 * 2 invalid input / incomplete coverage / runtime error（对齐上游 CLI）
 * 3 model auth / 401
 * 4 model 429
 * 5 model 5xx / timeout from provider
 * 6 result missing / invalid JSON
 * 7 canceled
 * 8 wall-clock timeout
 * 10 unknown
 */
export type ScanErrorCode =
  | "ok"
  | "invalid_input"
  | "incomplete"
  | "auth_failed"
  | "rate_limited"
  | "budget_exceeded"
  | "quota_exhausted"
  | "provider_error"
  | "result_corrupt"
  | "canceled"
  | "timeout"
  | "unknown";

export interface ScanResult {
  componentMatchingEstimate?:import('@axiom/contracts').TokenCostEstimate;
  executionUsage?:import('@axiom/contracts').ExecutionTokenUsage|undefined;
  interimReviewOutcome?:import("@axiom/contracts").InterimReviewOutcome;
  componentScan?:import("@axiom/contracts").ComponentScanResult;
  componentPlanning?:import("@axiom/contracts").ComponentPlanningResult;
  sdkOperation?:import('@axiom/contracts').SDKOperationResult;
  sdkSession?:import('@axiom/contracts').SDKSessionSummary;
  sdkDiagnostics?: SDKDiagnostics;
  sdkRaw?: Record<string,unknown>;
  lastObservedCost?: Record<string,unknown>;
  sdkRawSource?: "sdk_to_json"|"recovered_artifacts";
  taskId: number | string;

  status: ScanResultStatus;

  startedAt: string;
  finishedAt: string;

  engine: ScanEngineInfo;
  model: ScanModelInfo;

  findings: Finding[];

  coverage?: ScanCoverageInfo;
  cost?: ScanCostInfo;
  error?: ScanErrorInfo;

  /** 相对 outputPath 的 raw 目录，默认 raw/codex-security。 */
  rawArtifactDir: string;

  /** 相对 outputPath 的 normalized 文件，默认 normalized/result.json。 */
  normalizedResultPath: string;
}

export const SCAN_EXIT_CODES: Record<ScanResultStatus, number> = {
  completed: 0,
  partial: 2,
  failed: 2,
  canceled: 7,
  timeout: 8,
};
