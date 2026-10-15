import assert from "node:assert/strict";
import { test } from "node:test";
import { isolatedScanEnvironment } from "../src/runtime/scan-environment.js";
import type { ScanTask } from "../src/contract/scan-task.js";

const deepseek: ScanTask = {
  taskId: 1,
  sourcePath: "/src",
  outputPath: "/out",
  provider: "deepseek",
  model: "deepseek-v4-flash-vision-exp",
  scanMode: "standard",
  timeoutMinutes: 30,
};

test("strips ChatGPT ambient credentials for DeepSeek", () => {
  const env = isolatedScanEnvironment(deepseek, {
    DEEPSEEK_API_KEY: "ds-key",
    OPENAI_API_KEY: "sk-openai",
    CODEX_API_KEY: "sk-codex",
    CODEX_HOME: "C:\\Users\\me\\.codex",
    PATH: "/usr/bin",
  });
  assert.equal(env["DEEPSEEK_API_KEY"], "ds-key");
  assert.equal(env["PATH"], "/usr/bin");
  assert.equal(env["OPENAI_API_KEY"], undefined);
  assert.equal(env["CODEX_API_KEY"], undefined);
  assert.equal(env["CODEX_HOME"], undefined);
});

test("keeps OpenAI credentials for official provider", () => {
  const env = isolatedScanEnvironment(
    { ...deepseek, provider: "openai", model: "gpt-5.6-sol" },
    { OPENAI_API_KEY: "sk-openai", DEEPSEEK_API_KEY: "ds-key" },
  );
  assert.equal(env["OPENAI_API_KEY"], "sk-openai");
});
