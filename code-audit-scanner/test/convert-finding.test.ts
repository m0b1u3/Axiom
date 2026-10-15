import assert from "node:assert/strict";
import { test } from "node:test";
import { convertCodexResult, fallbackFingerprint } from "../src/converter/codex-finding-converter.js";
import type { ScanTask } from "../src/contract/scan-task.js";

const task: ScanTask = {
  taskId: 10001,
  sourcePath: "/tmp/source",
  outputPath: "/tmp/output",
  provider: "openai",
  model: "gpt-5.6-sol",
  scanMode: "standard",
  timeoutMinutes: 30,
};

test("converts official example finding into business Finding", () => {
  const result = convertCodexResult(
    task,
    {
      pluginVersion: "0.1.94",
      manifest: {
        scan: {
          status: "completed",
          startedAt: "2026-05-31T18:00:00Z",
          completedAt: "2026-05-31T18:09:00Z",
        },
      },
      coverage: { completeness: "complete" },
      cost: { estimatedUsd: 0, inputTokens: 0, outputTokens: 0 },
      findings: {
        findings: [
          {
            findingId: "csf_852f90d6e1177502ff113d4a",
            occurrenceId: "occ_e79cb19591e696572a1c22be",
            ruleId: "path-traversal.archive-extraction",
            fingerprints: {
              primary:
                "codex-security/v1:sha256:990a4a6a2ec18440dd47eac4d7256c0ee2c02db1b43104720cab3cbe9db706ca",
            },
            title: "Unsafe archive extraction can escape the output directory",
            summary: "An attacker-controlled path reaches a filesystem write without containment validation.",
            severity: { level: "high" },
            confidence: { level: "high" },
            taxonomy: { category: "path-traversal", cwe: ["CWE-22"] },
            locations: [
              { path: "src/extract.py", startLine: 41, endLine: 44, role: "sink" },
            ],
            remediation: "Normalize destinations and reject entries that escape the extraction root.",
            codeEvidence: [
              {
                path: "src/extract.py",
                startLine: 41,
                code: "open(os.path.join(out, name))",
              },
            ],
          },
        ],
      },
    },
    {
      startedAt: "2026-05-31T18:00:00Z",
      finishedAt: "2026-05-31T18:09:00Z",
      rawArtifactDir: "raw/codex-security",
      normalizedResultPath: "normalized/result.json",
    },
  );

  assert.equal(result.taskId, 10001);
  assert.equal(result.status, "completed");
  assert.equal(result.findings.length, 1);
  const finding = result.findings[0];
  assert.ok(finding);
  assert.equal(
    finding.fingerprint,
    "codex-security/v1:sha256:990a4a6a2ec18440dd47eac4d7256c0ee2c02db1b43104720cab3cbe9db706ca",
  );
  assert.equal(finding.severity, "high");
  assert.equal(finding.filePath, "src/extract.py");
  assert.equal(finding.startLine, 41);
  assert.equal(finding.endLine, 44);
  assert.deepEqual(finding.cwe, ["CWE-22"]);
  assert.equal(finding.confidence, 0.9);
  assert.equal(result.coverage?.completeness, "complete");
  assert.equal(result.cost?.estimatedUsd, 0);
  assert.equal(result.engine.name, "codex-security");
  assert.equal(result.engine.pluginVersion, "0.1.94");
  assert.equal(result.engine.version, "0.1.25");
});

test("keeps full upstream finding as raw and maps rich attack path", () => {
  const result = convertCodexResult(
    task,
    {
      manifest: { scan: { status: "completed" } },
      findings: {
        findings: [
          {
            findingId: "csf_x",
            occurrenceId: "occ_x",
            ruleId: "command-injection.os-system",
            title: "OS command injection in /ping",
            summary: "host param reaches os.system directly.",
            severity: { level: "high", score: 8.8, scoringSystem: "CVSS:3.1" },
            confidence: { level: "high", rationale: "Direct source trace: source -> concat -> sink." },
            taxonomy: { category: "command-injection", cwe: ["CWE-78"] },
            locations: [
              { path: "app.py", startLine: 8, role: "source" },
              { path: "app.py", startLine: 19, role: "sink" },
            ],
            rootCause: "Untrusted input builds a shell command string.",
            attackPath: {
              summary: "HTTP query param reaches os.system",
              entrypoint: "GET /ping?host=...",
              source: "request.args.get('host')",
              dataflow: "host -> concat -> os.system",
              sink: "os.system",
              reachability: "handler is registered and reachable",
              impact: { rationale: "Arbitrary command execution" },
              likelihood: { level: "high" },
              steps: ["1. 攻击者请求 /ping?host=...", "2. host 注入 ; id"],
              preconditions: ["服务可达"],
            },
            codeEvidence: [
              {
                id: "e1",
                label: "Untrusted host source",
                path: "app.py",
                startLine: 8,
                code: "host = request.args.get('host')",
                explanation: "host 未校验",
              },
              {
                id: "e2",
                label: "Shell execution sink",
                path: "app.py",
                startLine: 19,
                code: "os.system('ping -c 1 ' + host)",
                explanation: "shell 拼接",
              },
            ],
            remediationTests: ["测试 ';' 注入被拒绝"],
            preventiveControls: ["使用 subprocess 参数列表"],
          },
        ],
      },
    },
    {
      startedAt: "t0",
      finishedAt: "t1",
      rawArtifactDir: "raw/codex-security",
      normalizedResultPath: "normalized/result.json",
    },
  );

  const f = result.findings[0];
  assert.ok(f);
  // 具名映射
  assert.equal(f.severityScore, 8.8);
  assert.equal(f.scoringSystem, "CVSS:3.1");
  assert.equal(f.confidenceRationale, "Direct source trace: source -> concat -> sink.");
  assert.equal(f.rootCause, "Untrusted input builds a shell command string.");
  assert.equal(f.entrypoint, "GET /ping?host=...");
  assert.equal(f.source, "request.args.get('host')");
  assert.equal(f.dataflow, "host -> concat -> os.system");
  assert.equal(f.sink, "os.system");
  assert.equal(f.reachability, "handler is registered and reachable");
  assert.equal(f.impact, "Arbitrary command execution");
  assert.equal(f.likelihood, "high");
  assert.deepEqual(f.attackSteps, ["1. 攻击者请求 /ping?host=...", "2. host 注入 ; id"]);
  assert.deepEqual(f.preconditions, ["服务可达"]);
  assert.equal(f.codeEvidence?.length, 2);
  assert.equal(f.codeEvidence?.[1]?.code, "os.system('ping -c 1 ' + host)");
  assert.deepEqual(f.remediationTests, ["测试 ';' 注入被拒绝"]);
  assert.deepEqual(f.preventiveControls, ["使用 subprocess 参数列表"]);
  // 原样透传：raw 保留全部上游字段
  const raw = f.raw as Record<string, unknown>;
  assert.ok(raw);
  assert.equal(raw["findingId"], "csf_x");
  assert.equal((raw["attackPath"] as Record<string, unknown>)["entrypoint"], "GET /ping?host=...");
  assert.equal((raw["codeEvidence"] as Array<Record<string, unknown>>)[0]["label"], "Untrusted host source");
});

test("maps informational severity to info and prefers sink location", () => {
  const result = convertCodexResult(
    task,
    {
      manifest: { scan: { status: "completed" } },
      findings: {
        findings: [
          {
            title: "Note",
            summary: "Informational",
            severity: { level: "informational" },
            locations: [
              { path: "src/a.py", startLine: 1, role: "source" },
              { path: "src/b.py", startLine: 9, role: "sink" },
            ],
          },
        ],
      },
    },
    {
      startedAt: "t0",
      finishedAt: "t1",
      rawArtifactDir: "raw/codex-security",
      normalizedResultPath: "normalized/result.json",
    },
  );
  const finding = result.findings[0];
  assert.ok(finding);
  assert.equal(finding.severity, "info");
  assert.equal(finding.filePath, "src/b.py");
  assert.equal(finding.startLine, 9);
  assert.match(finding.fingerprint, /^axiom\/v1:sha256:/);
});

test("fallback fingerprint is stable, path-normalized, and independent of scan id", () => {
  const a = fallbackFingerprint({
    filePath: ".\\SQL-Injection\\App.py",
    ruleId: "CWE-89",
    startLine: 12,
  });
  const b = fallbackFingerprint({
    filePath: "sql-injection/app.py",
    ruleId: "cwe-89",
    startLine: 12,
  });
  assert.equal(a, b);
  assert.doesNotMatch(a, /10001|scan/i);
});

test("missing upstream status with findings becomes partial", () => {
  const result = convertCodexResult(
    task,
    {
      findings: {
        findings: [{ title: "x", summary: "y", severity: { level: "low" } }],
      },
    },
    {
      startedAt: "t0",
      finishedAt: "t1",
      rawArtifactDir: "raw/codex-security",
      normalizedResultPath: "normalized/result.json",
    },
  );
  assert.equal(result.status, "partial");
});
