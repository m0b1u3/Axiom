import assert from "node:assert/strict";
import { test } from "node:test";
import {
  CREDENTIAL_ENV,
  SCAN_EXIT_CODES,
  SCANNER_CONTAINER_PATHS,
  SCANNER_LAYOUT,
} from "../src/contract/index.js";

test("workspace layout is the only output contract", () => {
  assert.equal(SCANNER_LAYOUT.normalizedResult, "normalized/result.json");
  assert.equal(SCANNER_LAYOUT.rawDir, "raw/codex-security");
  assert.equal(SCANNER_LAYOUT.progress, "state/progress.json");
  assert.equal(SCANNER_LAYOUT.events, "logs/events.jsonl");
  assert.equal(SCANNER_CONTAINER_PATHS.source, "/workspace/source");
  assert.equal(SCANNER_CONTAINER_PATHS.output, "/workspace/output");
});

test("exit codes are stable for Agent", () => {
  assert.equal(SCAN_EXIT_CODES.completed, 0);
  assert.equal(SCAN_EXIT_CODES.partial, 2);
  assert.equal(SCAN_EXIT_CODES.failed, 2);
  assert.equal(SCAN_EXIT_CODES.canceled, 7);
  assert.equal(SCAN_EXIT_CODES.timeout, 8);
});

test("credential env names never live on ScanTask", () => {
  assert.equal(CREDENTIAL_ENV.deepseek, "DEEPSEEK_API_KEY");
  assert.equal(CREDENTIAL_ENV.openai, "OPENAI_API_KEY");
  assert.equal(CREDENTIAL_ENV.qwen, "DASHSCOPE_API_KEY");
});
