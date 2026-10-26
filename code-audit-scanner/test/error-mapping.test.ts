import assert from "node:assert/strict";
import { test } from "node:test";
import {
  AuthenticationRequiredError,
  IncompleteScanError,
  ScanCostLimitExceededError,
  ScanInterruptedError,
} from "@openai/codex-security";
import { mapEngineError } from "../src/engine/error-mapping.js";

test("cost limit is partial, not canceled", () => {
  const error = new ScanCostLimitExceededError(
    10,
    {
      model: "gpt-5.6-sol",
      inputTokens: 1,
      cachedInputTokens: 0,
      cacheWriteInputTokens: 0,
      outputTokens: 1,
      estimatedUsd: 11,
    },
    "/tmp/scan",
  );
  const mapped = mapEngineError(error);
  assert.equal(mapped.status, "partial");
  assert.equal(mapped.code, "budget_exceeded");
});

test("interrupt maps to canceled", () => {
  const mapped = mapEngineError(new ScanInterruptedError("killed", "/tmp/scan"));
  assert.equal(mapped.status, "canceled");
  assert.equal(mapped.code, "canceled");
});

test("auth error maps to auth_failed", () => {
  const mapped = mapEngineError(new AuthenticationRequiredError("invalid api key"));
  assert.equal(mapped.code, "auth_failed");
});

test("incomplete scan maps to partial", () => {
  const mapped = mapEngineError(new IncompleteScanError("coverage incomplete"));
  assert.equal(mapped.status, "partial");
  assert.equal(mapped.code, "incomplete");
});

test("message classifier covers 401 / 429 / 500 / timeout", () => {
  assert.equal(mapEngineError(new Error("HTTP 401 unauthorized")).code, "auth_failed");
  assert.equal(mapEngineError(new Error("429 rate limit")).code, "rate_limited");
  assert.equal(mapEngineError(new Error("upstream 500")).code, "provider_error");
  assert.equal(mapEngineError(new Error("request timed out")).code, "timeout");
  assert.equal(mapEngineError(new Error("aborted by user")).code, "canceled");
});

test('namespace compatibility failures retain the original error and identify provider format failure',()=>{
 for(const text of ['{"error":{"code":"1214","message":"tools[4].type:type is illegal"}}','namespace_tool_incompatible']){
  const mapped=mapEngineError(new Error(text));assert.equal(mapped.status,'failed');assert.equal(mapped.code,'provider_error');assert.match(mapped.message,/工具格式不兼容/);assert.ok(mapped.message.includes(text));
 }
});

test("stale workbench running row maps to incomplete, not unknown", () => {
  const mapped = mapEngineError(
    new Error("Could not save the Codex Security scan: Cannot archive the output of a running scan."),
  );
  assert.equal(mapped.status, "failed");
  assert.equal(mapped.code, "incomplete");
});

test("DeepSeek /models schema mismatch is provider_error, not auth_failed", () => {
  const mapped = mapEngineError(
    new Error("failed to decode models response: missing field `models` at line 1 column 233"),
  );
  assert.equal(mapped.code, "provider_error");
});
