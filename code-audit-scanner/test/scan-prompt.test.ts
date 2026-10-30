import assert from "node:assert/strict";
import { test } from "node:test";
import {
  buildScanPrompt,
  normalizeFocuses,
} from "../src/contract/scan-prompt.js";

test("always requires Simplified Chinese output", () => {
  const prompt = buildScanPrompt();
  assert.match(prompt, /简体中文/);
  assert.match(prompt, /漏洞标题必须是中文/);
});

test('audit orchestration and checkpoint policy belong to the SDK workflow',()=>{
 const prompt=buildScanPrompt();
 assert.match(prompt,/子任务编排、检查点与完成判定遵循当前 SDK 原生工作流/);
 assert.doesNotMatch(prompt,/wait_agent|artifacts\/checkpoints|最多请求该子任务补交一次/);
});

test("uses the 0.5 guidance helper without requesting removed Python helpers", () => {
  const prompt = buildScanPrompt();
  assert.match(prompt, /launch_codex_security_mcp --helper resolve-security-md/);
  assert.doesNotMatch(prompt, /config_preflight\.py|resolve_security_md\.py/);
  assert.match(prompt, /优先复用宿主/);
  assert.match(prompt, /按指定行范围精确提取/);
  assert.match(prompt, /不得去缩进、截句、省略或改写/);
});

test("stacks selected focus segments and ignores duplicates", () => {
  const prompt = buildScanPrompt(["authz", "injection", "authz"]);
  assert.match(prompt, /越权/);
  assert.match(prompt, /SQL\/NoSQL/);
  assert.equal(prompt.includes("多租户"), false);
});

test("rejects unknown focus keys", () => {
  assert.throws(() => normalizeFocuses(["authz", "nope"]), /unknown scan focus/);
});

test("empty and blank entries collapse to no extra focus", () => {
  assert.deepEqual(normalizeFocuses(["", "  "]), []);
  assert.deepEqual(normalizeFocuses(undefined), []);
});
