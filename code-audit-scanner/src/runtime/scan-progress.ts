import { appendFile, mkdir, writeFile, rename } from "node:fs/promises";
import path from "node:path";
import { redactSecrets } from "../security/secret-redactor.js";
import { SCANNER_LAYOUT } from "../contract/workspace.js";
import type { ScanLogEvent, ScanPhase, ScanProgressSnapshot } from "../contract/progress.js";
import type { ScanResultStatus } from "../contract/scan-result.js";

export type { ScanLogEvent, ScanPhase, ScanProgressSnapshot } from "../contract/progress.js";

export class ScanProgressSink {
  readonly progressPath: string;
  readonly eventsPath: string;
  private snapshot: ScanProgressSnapshot;
  private writes: Promise<void> = Promise.resolve();

  constructor(outputPath: string, taskId: number | string) {
    this.progressPath = path.join(outputPath, SCANNER_LAYOUT.progress);
    this.eventsPath = path.join(outputPath, SCANNER_LAYOUT.events);
    this.snapshot = {
      taskId,
      phase: "preparing",
      updatedAt: new Date().toISOString(),
    };
  }

  async init(): Promise<void> {
    await mkdir(path.dirname(this.progressPath), { recursive: true, mode: 0o700 });
    await mkdir(path.dirname(this.eventsPath), { recursive: true, mode: 0o700 });
    // 同一 taskId 重试时截断旧 jsonl，活动流不能把上次扫描叠上来。
    await writeFile(this.eventsPath, "", "utf8");
    await this.flush();
    await this.event("system", "info", "scan started");
  }

  async setPhase(phase: ScanPhase): Promise<void> {
    this.snapshot.phase = phase;
    this.snapshot.updatedAt = new Date().toISOString();
    await this.flush();
    await this.event("progress", "info", `phase=${phase}`);
  }

  async progress(input: {
    phase?: string;
    filesCompleted?: number;
    filesTotal?: number;
  }): Promise<void> {
    if (input.phase) this.snapshot.phase = mapUpstreamPhase(input.phase);
    if (input.filesCompleted !== undefined) this.snapshot.filesCompleted = input.filesCompleted;
    if (input.filesTotal !== undefined) this.snapshot.filesTotal = input.filesTotal;
    this.snapshot.updatedAt = new Date().toISOString();
    await this.flush();
    const files =
      this.snapshot.filesCompleted !== undefined && this.snapshot.filesTotal !== undefined
        ? ` files=${this.snapshot.filesCompleted}/${this.snapshot.filesTotal}`
        : "";
    await this.event("progress", "info", `phase=${this.snapshot.phase}${files}`);
  }

  async deepProgress(input: {completed: number; active: number; maximum: number}): Promise<void> {
    if (![input.completed,input.active,input.maximum].every(n => Number.isInteger(n) && n >= 0)) return;
    const before = this.snapshot.deep;
    if (before && before.completed === input.completed && before.active === input.active && before.maximum === input.maximum) return;
    this.snapshot.deep = {...input, updatedAt: new Date().toISOString()};
    if (["preparing","preflight","threat_model"].includes(this.snapshot.phase) && input.active + input.completed > 0) this.snapshot.phase = "discovery";
    this.snapshot.updatedAt = new Date().toISOString();
    await this.flush();
    await this.event("worker", "info", `深度扫描：已完成 ${input.completed}，活跃 ${input.active}，发现次数上限 ${input.maximum}`);
  }

  async workerStatus(input: object): Promise<void> {
    const value = JSON.parse(redactSecrets(JSON.stringify(input))) as Record<string, unknown>;
    if (JSON.stringify(this.snapshot.worker) === JSON.stringify(value)) return;
    this.snapshot.worker = value;
    this.snapshot.updatedAt = new Date().toISOString();
    await this.flush();
    await this.event("worker", "info", JSON.stringify(value));
  }

  async reconnect(attempt: number, maximum: number, details?: {reason: string; retryAfterSeconds?: number}): Promise<void> {
    this.snapshot.reconnect = {attempt, maximum, reason: details?.reason ?? "unknown", updatedAt: new Date().toISOString(), ...(details?.retryAfterSeconds !== undefined ? {retryAfterSeconds: details.retryAfterSeconds} : {})};
    this.snapshot.updatedAt = new Date().toISOString();
    await this.flush();
    await this.event("system", "warn", `SDK 连接重试 ${attempt}/${maximum}：${details?.reason ?? "unknown"}${details?.retryAfterSeconds !== undefined ? `，建议等待 ${details.retryAfterSeconds} 秒` : ""}`);
  }

  async activity(description: string): Promise<void> {
    const safe = redactSecrets(description);
    const summary = safe.replaceAll(/\s+/gu, " ");
    this.snapshot.activity = summary.length > 240 ? `${summary.slice(0, 239)}…` : summary;
    this.snapshot.updatedAt = new Date().toISOString();
    await this.flush();
    await this.event("activity", /^failed\s/iu.test(safe) ? "error" : "info", safe);
  }

  async cost(estimatedUsd: number): Promise<void> {
    this.snapshot.costUsd = estimatedUsd;
    this.snapshot.updatedAt = new Date().toISOString();
    await this.flush();
    await this.event("cost", "info", `usd=${estimatedUsd}`);
  }

  async warning(message: string): Promise<void> {
    await this.event("warning", "warn", redactSecrets(message));
  }

  /** SDK onSessionEvent：线程/worker 维度的会话事件（含推理摘要）。 */
  async session(threadId: string, worker: number | undefined, detail: string): Promise<void> {
    const where = `thread=${threadId}${worker !== undefined ? ` w${worker}` : ""}`;
    await this.event("session", detail.startsWith("[error]") ? "error" : "info", redactSecrets(`${where} ${detail}`));
  }

  async finish(phase: Extract<ScanPhase, "completed" | "partial" | "failed" | "canceled">): Promise<void> {
    this.snapshot.phase = phase;
    this.snapshot.updatedAt = new Date().toISOString();
    await this.flush();
    await this.event("system", phase === "failed" ? "error" : "info", `scan ${phase}`);
  }

  private async flush(): Promise<void> {
    const data = `${JSON.stringify(this.snapshot, null, 2)}\n`;
    await this.enqueue(async () => {
      const temporary = `${this.progressPath}.tmp`;
      await writeFile(temporary, data, "utf8");
      await rename(temporary, this.progressPath);
    });
  }

  private enqueue(write: () => Promise<void>): Promise<void> {
    const next = this.writes.then(write);
    this.writes = next.catch(() => undefined);
    return next;
  }

  private async event(
    kind: ScanLogEvent["kind"],
    level: ScanLogEvent["level"],
    message: string,
  ): Promise<void> {
    const line: ScanLogEvent = {
      timestamp: new Date().toISOString(),
      level,
      kind,
      message: redactSecrets(message),
    };
    await this.enqueue(() => appendFile(this.eventsPath, `${JSON.stringify(line)}\n`, "utf8"));
  }
}

export function mapUpstreamPhase(phase: string): ScanPhase {
  switch (phase) {
    case "preflight":
    case "threat_model":
    case "discovery":
    case "validation":
    case "attack_path":
    case "reporting":
      return phase;
    default:
      return "discovery";
  }
}

export function finishPhaseFromStatus(
  status: ScanResultStatus,
): Extract<ScanPhase, "completed" | "partial" | "failed" | "canceled"> {
  if (status === "canceled") return "canceled";
  if (status === "completed" || status === "partial") return status;
  return "failed";
}
