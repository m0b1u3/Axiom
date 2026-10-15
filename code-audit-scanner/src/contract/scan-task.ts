import type {ResolvedModel} from "@axiom/contracts";
/**
 * Scanner 输入合同。
 *
 * 硬约束：
 * - 不携带 API Key / Token / 密码
 * - 不携带 Git credential
 * - 不携带数据库密码 / JWT / master key
 *
 * Secret 只允许由 Agent 在启动容器时注入环境变量。
 */
export type ScanProvider = string;

export type ScanMode = "standard" | "deep";

export interface ScanTask {
  resultSchemaVersion?: 1;
  progressSchemaVersion?: 2;
  eventSchemaVersion?: 2;
  resolvedModel?: ResolvedModel;
  /** 业务层任务 ID，写入 normalized result，不传给上游 SDK 当 scan id。 */
  taskId: number | string;
  executionAttempt?: number;
  configurationRevision?: string;
  connectionType?: "openai" | "openai_compatible";
  timeBudget?: {scanSeconds:number;finalizationGraceSeconds:number};

  /** 已物化的只读源码目录。必须是 Source Snapshot，禁止指向 git branch HEAD。 */
  sourcePath: string;

  /** 产物目录，必须在源码树之外。 */
  outputPath: string;

  provider: ScanProvider;

  /** 模型名，例如 gpt-5.6-sol / qwen-plus / deepseek-chat。 */
  model: string;

  /**
   * OpenAI-compatible base URL。
   * openai 官方路径可留空；qwen / deepseek / custom 必填。
   * 不得包含 query 中的 key。
   */
  baseUrl?: string;

  scanMode: ScanMode;

  /**
   * 路径子集扫描（可选）。相对 sourcePath 的目录/文件白名单，
   * 透传 Codex Security ScanTarget（readonly string[] 形态）。
   * 空缺 = 全量扫描。元素必须是相对路径，禁止绝对路径与 `..`。
   */
  includePaths?: string[];

  /**
   * 发现重点（可选）。可叠加的场景键：authz / injection / ssrf / secrets / tenant / upload。
   * 空缺 = 通用审计。中文产出约束始终注入，不走本字段。
   */
  scanFocus?: string[];
 validationMode?: "default" | "exploitability";
 businessContext?: string;

  timeoutMinutes: number;

  /** 透传 Codex Security maxCostUsd。非 OpenAI 模型上可能只是尽力而为。 */
  maxCostUsd?: number;

  deepMaxDiscoveryRuns?: number;
  deepStopAfterNoNew?: number;

  /** 未设置时保留 SDK 默认 worker 数。 */
  workers?: number;

  /**
   * 走 Codex Security `--mock`：合成 finding，不调模型。
   * 只用于安装冒烟和合同测试。Agent 生产路径禁止置位。
   */
  mock?: boolean;

  /**
   * Provider 接入描述透传（解耦：Agent 从 pkg/providers 注册表下发）。
   * 设置后优先于 scanner 内置 PROVIDERS 注册表 —— 新增 provider 零 scanner 改动。
   * credentialEnv 只是环境变量「名」，永远不是 key 本身。
   */
  credentialEnv?: string;
  wireApi?: "responses" | "chat";
  displayName?: string;
}
