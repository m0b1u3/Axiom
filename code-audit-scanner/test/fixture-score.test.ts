import assert from "node:assert/strict";
import { test } from "node:test";
import { scoreFixtures } from "../src/eval/fixture-score.js";
import type { Finding } from "../src/contract/finding.js";

const fixtures = [
  { id: "sql-injection", cwe: ["CWE-89"], files: ["sql-injection/app.py"] },
  { id: "command-injection", cwe: ["CWE-78"], files: ["command-injection/app.py"] },
  { id: "path-traversal", cwe: ["CWE-22"], files: ["path-traversal/app.py"] },
  { id: "ssrf", cwe: ["CWE-918"], files: ["ssrf/app.py"] },
  { id: "hardcoded-secret", cwe: ["CWE-798"], files: ["hardcoded-secret/app.py"] },
];

test("scores by CWE even when paths are synthetic mock files", () => {
  const findings: Finding[] = [
    {
      fingerprint: "a",
      title: "sqli",
      severity: "high",
      description: "x",
      cwe: ["CWE-89"],
      filePath: "mock/sql-injection.ts",
    },
    {
      fingerprint: "b",
      title: "cmd",
      severity: "critical",
      description: "x",
      cwe: ["CWE-78"],
      filePath: "mock/command-injection.ts",
    },
    {
      fingerprint: "c",
      title: "path",
      severity: "high",
      description: "x",
      cwe: ["CWE-22"],
      filePath: "mock/path-traversal.ts",
    },
  ];
  const score = scoreFixtures(findings, fixtures);
  assert.equal(score.hits, 3);
  assert.equal(score.total, 5);
  assert.equal(score.results.find((item) => item.id === "sql-injection")?.hit, true);
  assert.equal(score.results.find((item) => item.id === "ssrf")?.hit, false);
});

test("falls back to expected file path when CWE is missing", () => {
  const findings: Finding[] = [
    {
      fingerprint: "d",
      title: "secret",
      severity: "high",
      description: "x",
      filePath: "hardcoded-secret/app.py",
    },
  ];
  const score = scoreFixtures(findings, fixtures);
  assert.equal(score.results.find((item) => item.id === "hardcoded-secret")?.hit, true);
  assert.equal(score.hits, 1);
});
