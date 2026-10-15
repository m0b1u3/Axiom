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

test("permits required SDK helpers and avoids sliced file reads", () => {
  const prompt = buildScanPrompt();
  assert.match(prompt, /config_preflight\.py/);
  assert.match(prompt, /resolve_security_md\.py/);
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
