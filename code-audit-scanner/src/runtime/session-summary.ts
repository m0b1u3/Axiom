import { redactSecrets } from "../security/secret-redactor.js";

/**
 * 把 Codex Security onSessionEvent 压成活动流行。
 * 协议骨架（token_count / session_meta / 完整 prompt）不进日志。
 * 返回 null = 这条不要写 events.jsonl。
 */
export function summarizeSessionEvent(event: {
  threadId: string;
  parentThreadId: string | null;
  worker?: number;
  event: Record<string, unknown>;
}): string | null {
  const raw = event.event ?? {};
  const type = asString(raw["type"]);
  const payload = asRecord(raw["payload"]);
  const ptype = payload ? asString(payload["type"]) : "";

  if (type === "session_meta" || type === "world_state" || type === "turn_context") return null;
  if (ptype === "token_count") return null;

  if (type === "event_msg") {
    if (ptype === "task_complete" && payload?.["error"]) {
      const error = asRecord(payload["error"]);
      const message = asString(error?.["message"]) || asString(payload["error"]) || "Unknown worker error";
      return `[error] 工作线程失败：${clip(message, 4096)}`;
    }
    if (ptype === "task_started") return event.parentThreadId
      ? `[system] 子任务执行轮次开始 · ${event.threadId}`
      : "[system] 主任务执行轮次开始（不代表重新扫描）";
    if (ptype.includes("reason")) {
      const text = asString(payload?.["text"] ?? payload?.["delta"] ?? payload?.["summary"]);
      return text ? `[reasoning] ${clip(text)}` : null;
    }
    // agent_message / item_completed / CommandExecution 已由 onActivity 覆盖
    return null;
  }

  if (type === "response_item") {
    if (!payload) return null;
    if (ptype === "reasoning") {
      const text = extractText(payload["summary"] ?? payload["content"]);
      return text ? `[reasoning] ${clip(text)}` : null;
    }
    return null;
  }

  return null;
}

/** 活动行：status kind 描述。命令剥掉 powershell.exe 包装。 */
export function formatActivityLine(activity: {
  status: string;
  kind: string;
  description: string;
}): string {
  // 保留 SDK 提供的内容，仅脱敏和有界限长；展示摘要由前端负责。
  const desc = redactSecrets(activity.description.trim());
  const bounded = desc.length > 16384 ? `${desc.slice(0, 16384)}\n[日志超过 16 KiB，已截断]` : desc;
  return `${activity.status} ${activity.kind} ${bounded}`;
}

export function cleanCommand(command: string): string {
  let s = command.replaceAll(/\s+/gu, " ").trim();
  s = s.replace(/^.*powershell\.exe"?\s+(?:-NoProfile\s+)?-Command\s+/iu, "");
  if (
    (s.startsWith("'") && s.endsWith("'") && s.length >= 2) ||
    (s.startsWith('"') && s.endsWith('"') && s.length >= 2)
  ) {
    s = s.slice(1, -1);
  }
  return s;
}

function extractText(value: unknown): string {
  const direct = asString(value);
  if (direct) return direct;
  if (Array.isArray(value)) {
    const parts: string[] = [];
    for (const item of value) {
      const rec = asRecord(item);
      if (!rec) continue;
      const t = asString(rec["text"] ?? rec["message"]);
      if (t) parts.push(t);
    }
    return parts.join(" ").trim();
  }
  const rec = asRecord(value);
  if (!rec) return "";
  return asString(rec["text"] ?? rec["message"] ?? rec["summary"]);
}

function clip(text: string, max = 240): string {
  const redacted = redactSecrets(text.replaceAll("\n", " ").trim());
  return redacted.length > max ? `${redacted.slice(0, max - 1)}…` : redacted;
}

function asString(v: unknown): string {
  return typeof v === "string" ? v : "";
}

function asRecord(v: unknown): Record<string, unknown> | null {
  if (v && typeof v === "object" && !Array.isArray(v)) return v as Record<string, unknown>;
  return null;
}
