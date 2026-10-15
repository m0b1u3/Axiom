import type { ScanTask } from "../contract/scan-task.js";
import {nativeOpenAI} from "../provider/model-provider.js";

const CHATGPT_AMBIENT_KEYS = [
  "OPENAI_API_KEY",
  "CODEX_API_KEY",
  "CODEX_HOME",
  "CHATGPT_ACCESS_TOKEN",
  "CHATGPT_ACCOUNT_ID",
] as const;

/**
 * 第三方 provider 扫描时剥掉本机 ChatGPT / OpenAI 登录态。
 * Codex Security 在隔离 home 为空时会 import 环境里的 ChatGPT refresh token，
 * 上次 DeepSeek 扫描因此被 401 invalid_refresh_token 污染。
 *
 * 不把 API Key 写入返回对象的值日志；调用方只拿去 spawn。
 */
export function isolatedScanEnvironment(
  task: ScanTask,
  source: NodeJS.ProcessEnv = process.env,
): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = { ...source };
  if (task.mock || nativeOpenAI(task)) {
    return env;
  }
  for (const key of CHATGPT_AMBIENT_KEYS) {
    if(key===task.credentialEnv)continue;
    delete env[key];
  }
  return env;
}

/** Apply isolated env to process.env for the duration of `fn`, then restore. */
export async function withIsolatedScanEnvironment<T>(
  task: ScanTask,
  fn: () => Promise<T>,
): Promise<T> {
  if (task.mock || nativeOpenAI(task)) {
    return fn();
  }
  const keys=CHATGPT_AMBIENT_KEYS.filter(key=>key!==task.credentialEnv);
  const previous: Array<[string, string | undefined]> = keys.map((key) => [
    key,
    process.env[key],
  ]);
  for (const key of keys) {
    delete process.env[key];
  }
  try {
    return await fn();
  } finally {
    for (const [key, value] of previous) {
      if (value === undefined) delete process.env[key];
      else process.env[key] = value;
    }
  }
}

export { CHATGPT_AMBIENT_KEYS };
