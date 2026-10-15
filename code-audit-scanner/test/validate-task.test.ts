import assert from "node:assert/strict";
import { test } from "node:test";
import { validateScanTask } from "../src/contract/validate-task.js";
import type { ScanTask } from "../src/contract/scan-task.js";
import { flagEnabled, parseArgs } from "../src/cli/args.js";

function baseTask(overrides: Partial<ScanTask> = {}): ScanTask {
  return {
    taskId: 1,
    sourcePath: "/data/snapshots/abc",
    outputPath: "/data/scans/1",
    provider: "openai",
    model: "gpt-5.6-sol",
    scanMode: "standard",
    timeoutMinutes: 30,
    ...overrides,
  };
}

test("accepts a minimal valid task", () => {
  assert.doesNotThrow(() => validateScanTask(baseTask()));
});

test("rejects secret-shaped fields even if added dynamically", () => {
  const task = baseTask() as ScanTask & { api_key?: string };
  task.api_key = "sk-test";
  assert.throws(() => validateScanTask(task), /secret field/);
});

test("rejects mock + deep", () => {
  assert.throws(
    () => validateScanTask(baseTask({ mock: true, scanMode: "deep" })),
    /mock cannot be combined with deep/,
  );
});

test("rejects output equal to source", () => {
  assert.throws(
    () => validateScanTask(baseTask({ outputPath: "/data/snapshots/abc" })),
    /outside sourcePath/,
  );
});

test("rejects unknown scanFocus keys", () => {
  assert.throws(
    () => validateScanTask(baseTask({ scanFocus: ["authz", "nope"] })),
    /unknown scan focus/,
  );
});

test("parseArgs treats bare flags as true", () => {
  const args = parseArgs([
    "--source",
    "../test-fixtures",
    "--mock",
    "--timeout",
    "5",
  ]);
  assert.equal(args.source, "../test-fixtures");
  assert.equal(args.mock, "true");
  assert.equal(args.timeout, "5");
  assert.equal(flagEnabled(args.mock), true);
});
