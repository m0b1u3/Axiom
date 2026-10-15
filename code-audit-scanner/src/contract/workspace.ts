/**
 * Scanner 工作区布局。相对 ScanTask.outputPath。
 *
 * Agent / 容器只允许按这些路径读写。禁止再发明第三套目录。
 */
export const SCANNER_LAYOUT = {
  normalizedResult: "normalized/result.json",
  rawDir: "raw/codex-security",
  progress: "state/progress.json",
  events: "logs/events.jsonl",
} as const;

export type ScannerLayoutPath = (typeof SCANNER_LAYOUT)[keyof typeof SCANNER_LAYOUT];

/**
 * 容器内约定（M2 才强制；本机扫描仍用 ScanTask.sourcePath / outputPath 绝对路径）。
 */
export const SCANNER_CONTAINER_PATHS = {
  source: "/workspace/source",
  output: "/workspace/output",
} as const;

/**
 * 各 provider 扫描进程必须看到的 API Key 环境变量。
 * ScanTask 本身禁止携带密钥。
 */
export const CREDENTIAL_ENV = {
  openai: "OPENAI_API_KEY",
  qwen: "DASHSCOPE_API_KEY",
  deepseek: "DEEPSEEK_API_KEY",
  kimi: "MOONSHOT_API_KEY",
  zhipu: "ZHIPU_API_KEY",
  custom: "CUSTOM_API_KEY",
} as const;
