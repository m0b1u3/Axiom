import {
  AuthenticationRequiredError,
  CodexSecurityError,
  IncompleteScanError,
  ScanCostLimitExceededError,
  ScanInterruptedError,
} from "@openai/codex-security";
import type { ScanErrorCode, ScanResultStatus } from "../contract/scan-result.js";
import {InvalidSDKResultError} from '../artifact/result-publisher.js';
import {ContractValidationError} from '@axiom/contracts';

export interface MappedEngineError {
  status: ScanResultStatus;
  code: ScanErrorCode;
  message: string;
}

export function mapEngineError(error: unknown): MappedEngineError {
  if(error instanceof InvalidSDKResultError || error instanceof ContractValidationError){return {status:'failed',code:'result_corrupt',message:error.message}}
  // ScanCostLimitExceededError extends ScanInterruptedError — check it first.
  if (error instanceof ScanCostLimitExceededError) {
    return { status: "partial", code: "budget_exceeded", message: error.message };
  }
  if (error instanceof ScanInterruptedError) {
    return { status: "canceled", code: "canceled", message: error.message };
  }
  if (error instanceof AuthenticationRequiredError) {
    return { status: "failed", code: "auth_failed", message: error.message };
  }
  if (error instanceof IncompleteScanError) {
    return { status: "partial", code: "incomplete", message: error.message };
  }
  if (error instanceof CodexSecurityError) {
    const classified = classifyMessage(error.message);
    return classified;
  }
  if (error instanceof Error) {
    return classifyMessage(error.message);
  }
  return { status: "failed", code: "unknown", message: String(error) };
}

function classifyMessage(message: string): MappedEngineError {
  const lower = message.toLowerCase();
  // Codex /models schema mismatch is not an auth failure. DeepSeek returns
  // `{object,data}` while the runtime expects a `models` field.
  if (lower.includes("missing field `models`") || lower.includes("missing field \"models\"")) {
    return { status: "failed", code: "provider_error", message };
  }
  if (/\b401\b/.test(lower) || lower.includes("unauthorized") || lower.includes("invalid api key")) {
    return { status: "failed", code: "auth_failed", message };
  }
  if (/quota_exhausted|free quota exhausted|insufficient_quota|use free tier only|insufficient (balance|credit)/.test(lower)) {
    return { status: "failed", code: "quota_exhausted", message };
  }
  if (/\b429\b/.test(lower) || lower.includes("rate limit")) {
    return { status: "failed", code: "rate_limited", message };
  }
  if (/\b500\b/.test(lower) || /\b502\b/.test(lower) || /\b503\b/.test(lower)) {
    return { status: "failed", code: "provider_error", message };
  }
  if (lower.includes("cannot archive the output of a running scan")) {
    return { status: "failed", code: "incomplete", message };
  }
  if (lower.includes("timeout") || lower.includes("timed out")) {
    return { status: "timeout", code: "timeout", message };
  }
  if (lower.includes("abort") || lower.includes("cancel")) {
    return { status: "canceled", code: "canceled", message };
  }
  return { status: "failed", code: "unknown", message };
}
