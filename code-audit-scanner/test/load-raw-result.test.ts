import assert from "node:assert/strict";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import { loadRawCodexResult } from "../src/artifact/load-raw-result.js";
import { convertCodexResult } from "../src/converter/codex-finding-converter.js";
import type { ScanTask } from "../src/contract/scan-task.js";

const rawDir = path.join(path.dirname(fileURLToPath(import.meta.url)), "fixtures", "raw-codex-security");

const task: ScanTask = {
  taskId: 9,
  sourcePath: "/src",
  outputPath: "/out",
  provider: "deepseek",
  model: "deepseek-v4-flash-vision-exp",
  scanMode: "standard",
  timeoutMinutes: 30,
};

test("recovers findings.json after the SDK process fails", async () => {
  const recovered = await loadRawCodexResult(rawDir);
  assert.ok(recovered);
  const converted = convertCodexResult(task, recovered, {
    startedAt: "t0",
    finishedAt: "t1",
    rawArtifactDir: "raw/codex-security",
    normalizedResultPath: "normalized/result.json",
  });
  assert.equal(converted.findings.length, 1);
  assert.equal(converted.findings[0]?.cwe?.[0], "CWE-78");
  assert.equal(converted.status, "partial");
  assert.equal(converted.coverage?.completeness, "complete");
});

test("returns undefined when findings.json is missing", async () => {
  const recovered = await loadRawCodexResult(path.join(rawDir, "does-not-exist"));
  assert.equal(recovered, undefined);
});
