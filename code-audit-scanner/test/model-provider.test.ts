import assert from "node:assert/strict";
import { test } from "node:test";
import { buildCodexOverrides, buildModelCatalog, DeepSeekProvider } from "../src/provider/model-provider.js";
import type { ScanTask } from "../src/contract/scan-task.js";

function task(overrides: Partial<ScanTask> = {}): ScanTask {
  return {
    taskId: 1,
    sourcePath: "/src",
    outputPath: "/out",
    provider: "deepseek",
    model: DeepSeekProvider.defaultModel,
    scanMode: "standard",
    timeoutMinutes: 30,
    ...overrides,
  };
}

test("native OpenAI receives the selected model; mock does not emit provider overrides", () => {
  assert.deepEqual(
    buildCodexOverrides(task({ provider: "openai", model: "gpt-5.6-sol" })),
    {model:"gpt-5.6-sol"},
  );
  assert.equal(buildCodexOverrides(task({ mock: true })), undefined);
});

test("deepseek overrides use Responses wire_api and env_key, never the secret", () => {
  const overrides = buildCodexOverrides(task());
  assert.ok(overrides);
  assert.equal(overrides["model_provider"], "deepseek");
  assert.equal(overrides["model"], "deepseek-v4-flash");
  const providers = overrides["model_providers"] as Record<string, Record<string, string>>;
  const deepseek = providers["deepseek"];
  assert.ok(deepseek);
  assert.equal(deepseek["base_url"], "https://api.deepseek.com");
  assert.equal(deepseek["env_key"], "DEEPSEEK_API_KEY");
  assert.equal(deepseek["wire_api"], "responses");
  assert.equal(overrides["cli_auth_credentials_store"], "file");
  assert.equal(JSON.stringify(overrides).includes("sk-"), false);
});

test("custom provider requires baseUrl", () => {
  assert.throws(
    () => buildCodexOverrides(task({ provider: "custom", model: "x" })),
    /baseUrl is required/,
  );
});

test("deepseek overrides carry inline model_catalog_json with the configured model", () => {
  const overrides = buildCodexOverrides(task());
  // 内联 JSON（TOML 字符串），避免文件路径解析不确定性
  const inline = JSON.parse(String(overrides?.["model_catalog_json"])) as {
    models: Array<Record<string, unknown>>;
  };
  const ids = inline.models.map((m) => m["slug"]);
  // 型号完全来自用户配置，无硬编码变体
  assert.deepEqual(ids, ["deepseek-v4-flash"]);
  const catalog = buildModelCatalog(task({ model: "glm-4.6", provider: "zhipu" }));
  const zhipuIds = (catalog["models"] as Array<{ slug: string }>).map((m) => m.slug);
  assert.deepEqual(zhipuIds, ["glm-4.6"]);
  const entry = inline.models[0];
  assert.equal(entry?.["context_window"], 1_048_576);
  assert.ok(Array.isArray(entry?.["supported_reasoning_levels"]));
  // Codex 0.149.1 catalog 解析器要求（缺失会导致 os error 2/解析失败）：
  assert.equal(entry?.["visibility"], "list");
  assert.equal(typeof entry?.["base_instructions"], "string");
  assert.equal(entry?.["shell_type"], "unified_exec");
});

test("task-carried provider spec wins over built-in registry (decoupled)", () => {
  const overrides = buildCodexOverrides(
    task({ credentialEnv: "MOONSHOT_API_KEY", wireApi: "chat", displayName: "Kimi" }),
  );
  const providers = overrides?.["model_providers"] as Record<string, Record<string, string>>;
  const deepseek = providers["deepseek"];
  assert.equal(deepseek["env_key"], "MOONSHOT_API_KEY");
  assert.equal(deepseek["wire_api"], "chat");
  assert.equal(deepseek["name"], "Kimi");
});
