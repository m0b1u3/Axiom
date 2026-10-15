import type { ScanTask } from "../contract/scan-task.js";
import type { ScanResult } from "../contract/scan-result.js";

/**
 * 可替换扫描引擎。业务层只认识这个接口。
 *
 * 取消：把 AbortSignal abort 掉。Scanner 必须尽快停模型请求并写 result。
 * 进度：只认 outputPath 下的 SCANNER_LAYOUT，不要解析 stderr。
 */
export interface AuditEngine {
  readonly name: string;

  scan(task: ScanTask, signal?: AbortSignal): Promise<ScanResult>;
}
