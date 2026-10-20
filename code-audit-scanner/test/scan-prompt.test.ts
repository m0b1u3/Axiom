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

test("uses the 0.5 guidance helper without requesting removed Python helpers", () => {
  const prompt = buildScanPrompt();
  assert.match(prompt, /launch_codex_security_mcp --helper resolve-security-md/);
  assert.doesNotMatch(prompt, /config_preflight\.py|resolve_security_md\.py/);
  assert.match(prompt, /优先复用宿主/);
  assert.match(prompt, /不要逐行切片阅读/);
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
