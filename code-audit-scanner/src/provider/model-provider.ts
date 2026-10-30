/**
 * Provider 适配层。
 *
 * 预设 provider 的环境凭据名包括：
 *   OPENAI_API_KEY | CODEX_API_KEY | OPENROUTER_API_KEY | FIREWORKS_API_KEY | AWS_*
 *
 * SDK 0.5.0 的自定义兼容连接使用 provider-scoped auth command。接法是 Codex
 * `codexOverrides.model_providers.<id>`（OpenAI-compatible / Responses）。
 * 未通过 M0 不得把 readiness 改成 supported。
 */

import type { JsonObject } from "@openai/codex-security";
import {fileURLToPath} from "node:url";
import {sdkRuntimeId} from '../runtime/sdk-runtime.js';
import type { ScanProvider, ScanTask } from "../contract/scan-task.js";

export type ProviderId = ScanProvider;

export type ProviderReadiness = "m0-pending" | "supported" | "degraded" | "unsupported";

export type WireApi = "responses";

export interface ModelProvider {
  readonly id: ProviderId;
  readonly displayName: string;
  readonly readiness: ProviderReadiness;
  readonly credentialEnv: string;
  readonly defaultModel: string;
  readonly defaultBaseUrl?: string;
  readonly wireApi: WireApi;
  /** 官方 CLI --provider。openai 走默认，不传。 */
  readonly cliProvider?: string;
}

export const OpenAIProvider: ModelProvider = {
  id: "openai",
  displayName: "OpenAI",
  readiness: "supported",
  credentialEnv: "OPENAI_API_KEY",
  defaultModel: "gpt-5.6-sol",
  wireApi: "responses",
};

export const QwenProvider: ModelProvider = {
  id: "qwen",
  displayName: "Qwen",
  readiness: "m0-pending",
  credentialEnv: "DASHSCOPE_API_KEY",
  defaultModel: "qwen3.7-max",
  defaultBaseUrl: "https://dashscope.aliyuncs.com/compatible-mode/v1",
  wireApi: "responses",
};

export const DeepSeekProvider: ModelProvider = {
  id: "deepseek",
  displayName: "DeepSeek",
  readiness: "m0-pending",
  credentialEnv: "DEEPSEEK_API_KEY",
  defaultModel: "deepseek-v4-flash",
  defaultBaseUrl: "https://api.deepseek.com",
  wireApi: "responses",
};

export const KimiProvider: ModelProvider = {
  id: "kimi",
  displayName: "Kimi",
  readiness: "m0-pending",
  credentialEnv: "MOONSHOT_API_KEY",
  defaultModel: "kimi-k2.7-code",
  defaultBaseUrl: "https://api.moonshot.cn/v1",
  wireApi: "responses",
};

export const ZhipuProvider: ModelProvider = {
  id: "zhipu",
  displayName: "Zhipu GLM",
  readiness: "m0-pending",
  credentialEnv: "ZHIPU_API_KEY",
  defaultModel: "glm-5.3",
  // Codex 直连 Responses 必须打 /api/v1；/api/paas/v4 是 Chat 端点（旧网关会 400）
  defaultBaseUrl: "https://open.bigmodel.cn/api/v1",
  wireApi: "responses",
};

export const CustomProvider: ModelProvider = {
  id: "custom",
  displayName: "Custom OpenAI-compatible",
  readiness: "m0-pending",
  credentialEnv: "CUSTOM_API_KEY",
  defaultModel: "custom-model",
  wireApi: "responses",
};

export const PROVIDERS: Record<ProviderId, ModelProvider> = {
  openai: OpenAIProvider,
  qwen: QwenProvider,
  deepseek: DeepSeekProvider,
  kimi: KimiProvider,
  zhipu: ZhipuProvider,
  custom: CustomProvider,
};

export function resolveProvider(id: ProviderId): ModelProvider {
  const preset=PROVIDERS[id];
  if(!preset)throw new Error(`Unsupported legacy provider: ${id}`);
  return preset;
}

export function resolveTaskProvider(task: ScanTask): ModelProvider {
  if(task.credentialEnv!==undefined&&task.wireApi!==undefined) {
    if(task.wireApi!=="responses")throw new Error("Chat 协议适配已移除；请配置支持 Responses 的模型接口。");
    const baseUrl=task.baseUrl ?? (task.connectionType===undefined?PROVIDERS[task.provider]?.defaultBaseUrl:undefined);
    return {id:task.provider,displayName:task.displayName??task.provider,readiness:"m0-pending",credentialEnv:task.credentialEnv,
      defaultModel:task.model,wireApi:task.wireApi,...(baseUrl!==undefined?{defaultBaseUrl:baseUrl}:{})};
  }
  return resolveProvider(task.provider);
}

export function nativeOpenAI(task: ScanTask): boolean {
  return task.connectionType==="openai" || (task.connectionType===undefined&&task.provider==="openai"&&task.baseUrl===undefined&&(task.wireApi===undefined||task.wireApi==="responses"));
}

/**
 * 注入 Codex 隔离配置。openai 官方路径返回 undefined（用 SDK 默认）。
 * 不把 API Key 写进 overrides。
 */
export function buildCodexOverrides(task: ScanTask): JsonObject | undefined {
  if (task.mock) {
    return undefined;
  }
  if (task.wireApi !== undefined && task.wireApi !== "responses") throw new Error("Chat 协议适配已移除；请配置支持 Responses 的模型接口。");
  if (resolveTaskProvider(task).wireApi !== "responses") throw new Error("Only Responses model connections are supported");
  // The selected model must reach Codex even on its native OpenAI connection.
  const effort=task.sdkParameters?.parameters.reasoningEffort;
 if(nativeOpenAI(task))return {model:task.model,...(effort?{model_reasoning_effort:effort}:{})};
  const spec = resolveTaskProvider(task);
  const baseUrl = task.baseUrl ?? spec.defaultBaseUrl;
  if (!baseUrl) {
    throw new Error(`ScanTask.baseUrl is required for provider ${task.provider}`);
  }
  // 任务透传（Agent 注册表下发）优先，内置 PROVIDERS 兜底 → 新 provider 零 scanner 改动
  const overrides: JsonObject = {
    model: task.model,
    model_provider: task.provider,
    // Catalog defaults do not override the SDK default (detailed). Compatible
    // endpoints have not established support for reasoning summaries.
    model_reasoning_summary: "none",
 ...(effort?{model_reasoning_effort:effort}:{}),
    cli_auth_credentials_store: "file",
    model_providers: {
      [task.provider]: {
        name: task.displayName ?? spec.displayName,
        base_url: baseUrl,
        // SDK 0.5.0 only recognizes its preset env_key providers during scan
        // authentication. Use its native provider-scoped command interface for
        // every compatible gateway; no OpenAI login or ambient key is selected.
        auth: {
          command: process.execPath,
          cwd: fileURLToPath(new URL('../../',import.meta.url)),
          args: [fileURLToPath(new URL('../../runtime/provider-auth.mjs',import.meta.url)), task.credentialEnv ?? spec.credentialEnv],
        },
        requires_openai_auth: false,
        wire_api: task.wireApi ?? spec.wireApi,
      },
    },
  };
  // 对照组开关：AXIOM_NO_CATALOG=1 时不下发 model_catalog_json（诊断用）
  if (process.env["AXIOM_NO_CATALOG"] !== "1") {
    // 模型目录：catalog JSON 内联（TOML 字符串），避免文件名路径解析的不确定性。
    // 文件版（AXIOM_CATALOG_FILE）仍由 engine 落盘，双保险。
    overrides["model_catalog_json"] = JSON.stringify(buildModelCatalog(task));
  }
  return overrides;
}

/** catalog 文件名（位于隔离 CODEX_HOME 下） */
export const AXIOM_CATALOG_FILE = "axiom-model-catalog.json";

/**
 * 生成 Codex model catalog（{models:[...]}）。
 * 只包含本次任务使用的模型 —— 型号由用户在模型配置里填写，
 * scanner 不维护任何硬编码型号清单。
 */
export function buildModelCatalog(task: ScanTask): JsonObject {
  const variants = [task.model];
  return {
    models: variants.map((id) => ({
      slug: id,
      display_name: id,
      description: `${task.provider} ${id}`,
      visibility: "list",
      base_instructions:
        "You are Codex, a coding agent. You and the user share the same workspace and collaborate to achieve the user's goals.",
      context_window: 1_048_576,
      max_context_window: 1_048_576,
      effective_context_window_percent: 95,
      truncation_policy: { limit: 10_000, mode: "bytes" },
      default_reasoning_level: task.sdkParameters?.parameters.reasoningEffort??"high",
      default_reasoning_summary: "none",
      supported_reasoning_levels: [
        { description: "Fast responses with lighter reasoning", effort: "low" },
        { description: "Balances speed and reasoning depth for everyday tasks", effort: "medium" },
        { description: "Greater reasoning depth for complex problems", effort: "high" },
        { description: "Maximum reasoning depth", effort: "max" },
 ...(task.sdkParameters?.parameters.reasoningEffort && !["low","medium","high"].includes(task.sdkParameters.parameters.reasoningEffort)?[{description:"Explicit reasoning selection",effort:task.sdkParameters.parameters.reasoningEffort}]:[]),
      ],
      supports_parallel_tool_calls: true,
      supports_reasoning_summaries: false,
      supported_in_api: true,
      input_modalities: ["text"],
      experimental_supported_tools: [],
      service_tiers: [],
      additional_speed_tiers: [],
      availability_nux: null,
      upgrade: null,
      priority: 1000,
      shell_type: "unified_exec",
      support_verbosity: false,
      supports_image_detail_original: false,
      supports_search_tool: false,
      // V1 is scoped to an explicitly selected compatibility connection. Native
      // GPT and other connections retain the SDK's V2 tool surface.
      multi_agent_version: sdkRuntimeId(task)==='legacy-v1'?'v1':'v2',
    })),
  };
}

export function requiredCredentialEnv(provider: ProviderId): string {
  return resolveProvider(provider).credentialEnv;
}
