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
import {TokenBudgetError} from '../runtime/token-budget.js';
import {isSDKRuntimeError} from '../runtime/sdk-runtime.js';

export interface MappedEngineError {
  status: ScanResultStatus;
  code: ScanErrorCode;
  message: string;
}

export function mapEngineError(error: unknown): MappedEngineError {
  if(error instanceof TokenBudgetError){
    if(error.upstreamFailure){const original=classifyMessage(error.upstreamFailure);return {...original,code:original.code==='unknown'?'provider_error':original.code,message:error.message};}
    return {status:error.code==='budget_price_missing'?'failed':'partial',code:error.code==='budget_exceeded'?'budget_exceeded':error.code==='budget_price_missing'?'invalid_input':'incomplete',message:error.message};
  }
  if(error instanceof InvalidSDKResultError || error instanceof ContractValidationError){return {status:'failed',code:'result_corrupt',message:error.message}}
  // ScanCostLimitExceededError extends ScanInterruptedError — check it first.
  if (error instanceof Error && (error instanceof ScanCostLimitExceededError||isSDKRuntimeError(error,'ScanCostLimitExceededError'))) {
    return { status: "partial", code: "budget_exceeded", message: error.message };
  }
  if (error instanceof Error && (error instanceof ScanInterruptedError||isSDKRuntimeError(error,'ScanInterruptedError'))) {
    return { status: "canceled", code: "canceled", message: error.message };
  }
  if (error instanceof Error && (error instanceof AuthenticationRequiredError||isSDKRuntimeError(error,'AuthenticationRequiredError'))) {
    return { status: "failed", code: "auth_failed", message: error.message };
  }
  if (error instanceof Error && (error instanceof IncompleteScanError||isSDKRuntimeError(error,'IncompleteScanError'))) {
    return { status: "partial", code: "incomplete", message: error.message };
  }
  if (error instanceof Error && (error instanceof CodexSecurityError||isSDKRuntimeError(error,'CodexSecurityError'))) {
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
  if (/namespace_tool_incompatible|tools\[\d+\]\.type:type is illegal/.test(lower)) {
    return {status:'failed',code:'provider_error',message:'模型接口的工具格式不兼容；本次未形成有效复核结论。原始错误：'+message};
  }
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
