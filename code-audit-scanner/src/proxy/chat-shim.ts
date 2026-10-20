/**
 * Responses ↔ Chat Completions 本地转换层（cc-switch「路由接管」的 Axiom 版）。
 *
 * 背景：codex-security 0.1.25 捆绑的 codex 0.149.1 二进制只实现 Responses
 * wire API（二进制里 0 处 chat/completions 引用）。上游只提供 Chat
 * Completions 的供应商（如 Kimi）必须经过转换才能工作。
 *
 * 做法与 cc-switch 一致：起 127.0.0.1 环回代理，Codex 以 Responses 协议
 * 打代理，代理转换后打上游 /chat/completions，SSE 流式转回 Responses 事件。
 * 凭证不经代理存储 —— 直接转发 Codex 带来的 Authorization 头。
 *
 * 转换语义移植自 cc-switch src-tauri/src/proxy/providers/transform_codex_chat.rs
 * 与 streaming_codex_chat.rs（Apache-2.0），裁剪到 Codex CLI 实际请求形态：
 * instructions + input items + function tools + function_call 多轮。
 * Moonshot 的 $ref-sibling schema 重写（transform_codex_chat_moonshot_schema.rs）
 * 针对 Codex Desktop 工具集；Codex CLI 自身工具不产生该形态（cc 注释实证），
 * 故不移植 —— M0 若暴露 schema 400 再补。
 */

import { createServer, type Server } from "node:http";
import { randomUUID } from "node:crypto";
import { ProxyAgent } from "undici";

/** 企业代理：HTTPS_PROXY/HTTP_PROXY 存在且上游非环回时经代理（Node fetch 默认忽略代理环境变量） */
export function upstreamDispatcher(upstreamBaseUrl: string): ProxyAgent | undefined {
  const proxy =
    process.env["HTTPS_PROXY"] ??
    process.env["https_proxy"] ??
    process.env["HTTP_PROXY"] ??
    process.env["http_proxy"];
  if (!proxy) return undefined;
  try {
    const host = new URL(upstreamBaseUrl).hostname;
    if (host === "127.0.0.1" || host === "localhost") return undefined;
  } catch {
    return undefined;
  }
  return new ProxyAgent(proxy);
}

type Json = Record<string, unknown>;

/* ------------------------------------------------------------------ */
/* 请求：Responses → Chat Completions                                  */
/* ------------------------------------------------------------------ */

/** instructions 可能是字符串或 content part 数组 */
function instructionText(value: unknown): string {
  if (typeof value === "string") return value;
  if (Array.isArray(value)) {
    return value
      .map((p) => (p && typeof p === "object" ? String((p as Json)["text"] ?? "") : ""))
      .filter(Boolean)
      .join("\n");
  }
  return "";
}

function chatRole(role: unknown): string {
  switch (role) {
    case "system":
    case "developer":
      return "system";
    case "assistant":
      return "assistant";
    case "tool":
      return "tool";
    default:
      return "user"; // user / latest_reminder / 未知
  }
}

/** message item 的 content → chat content（文本拼接；input_text/output_text） */
function chatContentText(content: unknown): string {
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";
  const parts: string[] = [];
  for (const p of content) {
    if (!p || typeof p !== "object") continue;
    const t = (p as Json)["type"];
    if (t === "input_text" || t === "output_text" || t === "text") {
      parts.push(String((p as Json)["text"] ?? ""));
    }
    // input_image / input_file：扫描场景不走多模态，丢弃
  }
  return parts.join("");
}

/**
 * input items → chat messages。
 * function_call 连续累积成一条 assistant.tool_calls；function_call_output
 * 触发 flush 并生成 role=tool 消息（cc 同语义）。
 */
function appendInputItems(input: unknown, messages: Json[]): void {
  let pendingToolCalls: Json[] = [];
  const flush = (): void => {
    if (pendingToolCalls.length > 0) {
      messages.push({ role: "assistant", content: null, tool_calls: pendingToolCalls });
      pendingToolCalls = [];
    }
  };
  const handle = (item: unknown): void => {
    if (typeof item === "string") {
      flush();
      messages.push({ role: "user", content: item });
      return;
    }
    if (!item || typeof item !== "object") return;
    const it = item as Json;
    switch (it["type"]) {
      case "function_call":
      case "custom_tool_call":
        pendingToolCalls.push({
          id: String(it["call_id"] ?? it["id"] ?? `call_${randomUUID()}`),
          type: "function",
          function: {
            name: String(it["name"] ?? ""),
            arguments: typeof it["arguments"] === "string" ? it["arguments"] : JSON.stringify(it["input"] ?? {}),
          },
        });
        return;
      case "function_call_output":
      case "custom_tool_call_output": {
        flush();
        const out = it["output"];
        messages.push({
          role: "tool",
          tool_call_id: String(it["call_id"] ?? it["id"] ?? ""),
          content: typeof out === "string" ? out : JSON.stringify(out ?? ""),
        });
        return;
      }
      case "message": {
        flush();
        const role = chatRole(it["role"]);
        messages.push({ role, content: chatContentText(it["content"]) });
        return;
      }
      case "reasoning":
        // 上游 chat 侧不消费 reasoning 项（kimi 等会拒收 encrypted_content），跳过
        return;
      default:
        return; // item_reference 等未知类型跳过
    }
  };
  if (typeof input === "string") {
    handle(input);
  } else if (Array.isArray(input)) {
    for (const item of input) handle(item);
  } else if (input && typeof input === "object") {
    handle(input);
  }
  flush();
}

/** responses function/custom tool → chat function tool */
function toChatTools(tools: unknown): Json[] {
  if (!Array.isArray(tools)) return [];
  const out: Json[] = [];
  for (const t of tools) {
    if (!t || typeof t !== "object") continue;
    const tool = t as Json;
    const type = tool["type"];
    if (type === "function") {
      out.push({
        type: "function",
        function: {
          name: String(tool["name"] ?? ""),
          ...(tool["description"] !== undefined ? { description: tool["description"] } : {}),
          parameters: tool["parameters"] ?? { type: "object", properties: {} },
        },
      });
    } else if (type === "custom") {
      // codex custom tool（如 apply_patch freeform）：降级为 function 形态，
      // grammar/format 信息并入 description（cc serialize_tool_definition 语义）
      const desc = [
        String(tool["description"] ?? ""),
        tool["format"] !== undefined ? `format: ${JSON.stringify(tool["format"])}` : "",
      ]
        .filter(Boolean)
        .join("\n");
      out.push({
        type: "function",
        function: {
          name: String(tool["name"] ?? ""),
          ...(desc ? { description: desc } : {}),
          parameters: { type: "object", properties: {} },
        },
      });
    }
    // web_search 等 hosted tool 无 chat 对应物，跳过
  }
  return out;
}

function toChatToolChoice(choice: unknown): unknown {
  if (typeof choice === "string") return choice; // auto/none/required
  if (choice && typeof choice === "object") {
    const c = choice as Json;
    if (c["type"] === "function" && typeof c["name"] === "string") {
      return { type: "function", function: { name: c["name"] } };
    }
    if (c["type"] === "allowed_tools") return "auto";
  }
  return "auto";
}

/** 与 cc EXTRA_CHAT_PASSTHROUGH_FIELDS 对齐的直通字段 */
const CHAT_PASSTHROUGH = [
  "frequency_penalty",
  "logit_bias",
  "n",
  "parallel_tool_calls",
  "presence_penalty",
  "response_format",
  "seed",
  "stop",
] as const;

export function convertResponsesRequestToChat(body: Json, explicitReasoningEffort?:string): Json {
  const result: Json = {};
  if (body["model"] !== undefined) result["model"] = body["model"];

  const messages: Json[] = [];
  const instructions = instructionText(body["instructions"]);
  if (instructions) messages.push({ role: "system", content: instructions });
  appendInputItems(body["input"], messages);
  result["messages"] = messages;

  if (body["max_output_tokens"] !== undefined) result["max_tokens"] = body["max_output_tokens"];
  for (const key of ["temperature", "top_p", "stream"] as const) {
    if (body[key] !== undefined) result[key] = body[key];
  }
  // Preserve historical defaults. Explicit Web selection must never disappear
  // in the Responses→Chat adapter; unsupported upstreams return their own error.
  if(explicitReasoningEffort!==undefined)result["reasoning_effort"]=(body["reasoning"] as Json|undefined)?.["effort"]??explicitReasoningEffort;

  const format = (body["text"] as Json | undefined)?.["format"] as Json | undefined;
  if (format?.["type"] === "json_schema") {
    result["response_format"] = { type: "json_schema", json_schema: {
      name: format["name"], schema: format["schema"],
      ...(format["strict"] !== undefined ? { strict: format["strict"] } : {}),
      ...(format["description"] !== undefined ? { description: format["description"] } : {}),
    } };
  } else if (format?.["type"] === "json_object") result["response_format"] = { type: "json_object" };
  const tools = toChatTools(body["tools"]);
  if (tools.length > 0) {
    result["tools"] = tools;
    if (body["tool_choice"] !== undefined) result["tool_choice"] = toChatToolChoice(body["tool_choice"]);
  }
  // 严格上游（vLLM/企业网关）拒绝 无 tools 却带 tool_choice/parallel_tool_calls（cc 实证）
  for (const key of CHAT_PASSTHROUGH) {
    if (body[key] !== undefined) result[key] = body[key];
  }
  if (tools.length === 0) {
    delete result["tool_choice"];
    delete result["parallel_tool_calls"];
  }
  // 流式 usage：OpenAI 兼容上游默认不吐 usage chunk，必须显式声明（cc 实证，
  // 缺了会导致 token/成本全为 0）
  if (result["stream"] === true) {
    const so = (result["stream_options"] as Json | undefined) ?? {};
    if (so["include_usage"] === undefined) so["include_usage"] = true;
    result["stream_options"] = so;
  }
  return result;
}

/* ------------------------------------------------------------------ */
/* 响应：Chat → Responses                                              */
/* ------------------------------------------------------------------ */

function mapUsage(u: unknown): Json | undefined {
  if (!u || typeof u !== "object") return undefined;
  const usage = u as Json;
  const out: Json = {
    input_tokens: usage["prompt_tokens"] ?? 0,
    output_tokens: usage["completion_tokens"] ?? 0,
    total_tokens: usage["total_tokens"] ?? 0,
  };
  const details = usage["prompt_tokens_details"] as Json | undefined;
  if (details?.["cached_tokens"] !== undefined) {
    out["input_tokens_details"] = { cached_tokens: details["cached_tokens"] };
  }
  const completionDetails = usage["completion_tokens_details"] as Json | undefined;
  if (completionDetails?.["reasoning_tokens"] !== undefined) {
    out["output_tokens_details"] = { reasoning_tokens: completionDetails["reasoning_tokens"] };
  }
  return out;
}

/** Non-streaming fallback uses identical terminal validation to streaming. */
export function convertChatCompletionToResponse(body: Json): Json {
  const stream = new ChatToResponsesStreamer();
  const choice = Array.isArray(body["choices"]) ? body["choices"][0] as Json | undefined : undefined;
  stream.feed({ ...body, choices: choice ? [{ ...choice, delta: choice["message"] }] : [] });
  const terminal = stream.finish().at(-1)!;
  return terminal.data["response"] as Json;
}

interface TextItemState {
  itemId: string;
  outputIndex: number;
  text: string;
}

interface ToolItemState {
  itemId: string;
  outputIndex: number;
  callId: string;
  name: string;
  arguments: string;
  added: boolean;
}

/**
 * 流式状态机：chat SSE chunk → Responses 事件序列。
 * 事件名与 codex-rs responses 客户端消费的一致（cc streaming_codex_chat.rs 同形态）。
 */
export class ChatToResponsesStreamer {
  private readonly responseId = `resp_${randomUUID()}`;
  private model = "";
  private started = false;
  private nextOutputIndex = 0;
  private text: TextItemState | null = null;
  private tools = new Map<number, ToolItemState>();
  private usage: Json | undefined;
  private failed: Json | null = null;
  private ended = false;
  private finishReason: string | null = null;
  private reasoningCharacters = 0;

  fail(code: string, message: string): void {
    this.failed ??= { code, message };
  }

  /** Metadata only: never log prompts, tool arguments, reasoning, or credentials. */
  diagnostics(): Json {
    return { responseId: this.responseId, finishReason: this.finishReason,
      textCharacters: this.text?.text.length ?? 0, toolCalls: this.tools.size,
      reasoningCharacters: this.reasoningCharacters, errorCode: this.failed?.["code"] ?? null };
  }

  /** 喂一个 chat chunk（已解析 JSON）；返回应下发的 Responses SSE 事件 */
  feed(chunk: Json): Array<{ event: string; data: Json }> {
    const out: Array<{ event: string; data: Json }> = [];
    if (this.ended) return out;
    if (typeof chunk["model"] === "string" && !this.model) this.model = chunk["model"];

    const err = chunk["error"] as Json | undefined;
    if (err && typeof err === "object") {
      this.failed = { code: err["code"] ?? null, message: String(err["message"] ?? "upstream error") };
      return out;
    }
    if (chunk["usage"]) this.usage = mapUsage(chunk["usage"]);

    if (!this.started) {
      this.started = true;
      const r = this.responseShell("in_progress");
      out.push({ event: "response.created", data: { type: "response.created", response: r } });
      out.push({ event: "response.in_progress", data: { type: "response.in_progress", response: r } });
    }

    const choice = Array.isArray(chunk["choices"]) ? (chunk["choices"][0] as Json | undefined) : undefined;
    if (typeof choice?.["finish_reason"] === "string") this.finishReason = choice["finish_reason"];
    const delta = (choice?.["delta"] as Json | undefined) ?? {};
    const reasoning = delta["reasoning_content"] ?? delta["reasoning"];
    if (typeof reasoning === "string") this.reasoningCharacters += reasoning.length;
    if (delta["refusal"]) this.fail("upstream_refusal", "Upstream refused the request.");

    if (typeof delta["content"] === "string" && delta["content"] !== "") {
      const st = this.ensureTextItem(out);
      st.text += delta["content"];
      out.push({
        event: "response.output_text.delta",
        data: {
          type: "response.output_text.delta",
          item_id: st.itemId,
          output_index: st.outputIndex,
          content_index: 0,
          delta: delta["content"],
        },
      });
    }

    const tcs = Array.isArray(delta["tool_calls"]) ? delta["tool_calls"] as Json[] : [];
    for (const tc of tcs) {
      let idx = typeof tc["index"] === "number" ? tc["index"] : undefined;
      const id = typeof tc["id"] === "string" ? tc["id"] : "";
      if (idx === undefined && id) idx = [...this.tools].find(([, st]) => st.callId === id)?.[0];
      if (idx === undefined) {
        if (id) idx = this.tools.size ? Math.max(...this.tools.keys()) + 1 : 0;
        else if (this.tools.size === 1) idx = this.tools.keys().next().value;
        else { this.fail("upstream_invalid_tool_call", "Ambiguous tool fragment without index or call ID."); continue; }
      }
      if (idx === undefined) continue;
      let st = this.tools.get(idx);
      if (!st) {
        st = { itemId: `fc_${randomUUID()}`, outputIndex: this.nextOutputIndex++,
          callId: "", name: "", arguments: "", added: false };
        this.tools.set(idx, st);
      }
      const fn = (tc["function"] as Json | undefined) ?? {};
      if (id) st.callId = id;
      if (typeof fn["name"] === "string" && fn["name"].trim()) st.name = fn["name"];
      const arg = typeof fn["arguments"] === "string" ? fn["arguments"] : "";
      st.arguments += arg;
      if (st.added && arg) out.push(this.toolDelta(st, arg));
    }
    // Preserve item order when an earlier parallel call receives its identity late.
    for (const st of [...this.tools.values()].sort((a, b) => a.outputIndex - b.outputIndex)) {
      if (st.added) continue;
      if (!st.callId || !st.name) break;
      st.added = true;
      out.push({ event: "response.output_item.added", data: {
        type: "response.output_item.added", output_index: st.outputIndex,
        item: { id: st.itemId, type: "function_call", status: "in_progress", call_id: st.callId, name: st.name, arguments: "" },
      } });
      if (st.arguments) out.push(this.toolDelta(st, st.arguments));
    }
    return out;
  }

  private toolDelta(st: ToolItemState, delta: string): { event: string; data: Json } {
    return { event: "response.function_call_arguments.delta", data: {
      type: "response.function_call_arguments.delta", item_id: st.itemId, output_index: st.outputIndex, delta,
    } };
  }

  /** Axiom fails truncated turns explicitly: partial tool arguments must never execute. */
  finish(): Array<{ event: string; data: Json }> {
    const out: Array<{ event: string; data: Json }> = [];
    if (this.ended) return out;
    this.ended = true;
    if (this.finishReason === "length") this.fail("upstream_output_truncated", "Upstream output reached its token limit (finish_reason=length).");
    else if (this.finishReason === "content_filter") this.fail("upstream_content_filter", "Upstream content filter interrupted the response.");
    else if (this.finishReason === null) this.fail("upstream_stream_incomplete", "Upstream ended without finish_reason.");
    else if (!["stop", "tool_calls", "function_call"].includes(this.finishReason)) this.fail("upstream_unknown_finish_reason", "Upstream returned an unsupported finish reason.");
    if (!this.text?.text.trim() && this.tools.size === 0) this.fail("upstream_empty_response", "Upstream returned no answer or tool calls.");
    if (this.finishReason === "tool_calls" && this.tools.size === 0) this.fail("upstream_invalid_tool_call", "Upstream ended with tool_calls but supplied no tools.");
    for (const st of this.tools.values()) {
      try {
        const args = JSON.parse(st.arguments);
        if (!st.callId || !st.name.trim() || !args || typeof args !== "object" || Array.isArray(args)) throw new Error();
      } catch { this.fail("upstream_invalid_tool_call", "Upstream returned an incomplete tool identity or invalid JSON arguments."); }
    }
    if (this.failed !== null) {
      out.push({
        event: "response.failed",
        data: { type: "response.failed", response: { ...this.responseShell("failed"), error: this.failed } },
      });
      return out;
    }
    const items: Json[] = [];
    if (this.text !== null) {
      const st = this.text;
      out.push({
        event: "response.output_text.done",
        data: { type: "response.output_text.done", item_id: st.itemId, output_index: st.outputIndex, content_index: 0, text: st.text },
      });
      out.push({
        event: "response.content_part.done",
        data: {
          type: "response.content_part.done",
          item_id: st.itemId,
          output_index: st.outputIndex,
          content_index: 0,
          part: { type: "output_text", text: st.text, annotations: [] },
        },
      });
      const item: Json = {
        id: st.itemId,
        type: "message",
        status: "completed",
        role: "assistant",
        content: [{ type: "output_text", text: st.text, annotations: [] }],
      };
      out.push({
        event: "response.output_item.done",
        data: { type: "response.output_item.done", output_index: st.outputIndex, item },
      });
      items.push({ index: st.outputIndex, item } as unknown as Json);
    }
    for (const st of [...this.tools.values()].sort((a, b) => a.outputIndex - b.outputIndex)) {
      out.push({
        event: "response.function_call_arguments.done",
        data: { type: "response.function_call_arguments.done", item_id: st.itemId, output_index: st.outputIndex, arguments: st.arguments },
      });
      const item: Json = {
        id: st.itemId,
        type: "function_call",
        status: "completed",
        call_id: st.callId,
        name: st.name,
        arguments: st.arguments,
      };
      out.push({
        event: "response.output_item.done",
        data: { type: "response.output_item.done", output_index: st.outputIndex, item },
      });
      items.push({ index: st.outputIndex, item } as unknown as Json);
    }
    const output = items
      .sort((a, b) => (a["index"] as number) - (b["index"] as number))
      .map((x) => x["item"] as Json);
    out.push({
      event: "response.completed",
      data: { type: "response.completed", response: { ...this.responseShell("completed"), output } },
    });
    return out;
  }

  private ensureTextItem(out: Array<{ event: string; data: Json }>): TextItemState {
    if (this.text !== null) return this.text;
    const st: TextItemState = { itemId: `msg_${randomUUID()}`, outputIndex: this.nextOutputIndex++, text: "" };
    this.text = st;
    out.push({
      event: "response.output_item.added",
      data: {
        type: "response.output_item.added",
        output_index: st.outputIndex,
        item: { id: st.itemId, type: "message", status: "in_progress", role: "assistant", content: [] },
      },
    });
    out.push({
      event: "response.content_part.added",
      data: {
        type: "response.content_part.added",
        item_id: st.itemId,
        output_index: st.outputIndex,
        content_index: 0,
        part: { type: "output_text", text: "", annotations: [] },
      },
    });
    return st;
  }

  private responseShell(status: string): Json {
    return {
      id: this.responseId,
      object: "response",
      created_at: Math.floor(Date.now() / 1000),
      status,
      model: this.model,
      output: [],
      ...(this.usage !== undefined ? { usage: this.usage } : {}),
    };
  }
}

/* ------------------------------------------------------------------ */
/* 环回代理服务                                                        */
/* ------------------------------------------------------------------ */

/** Incremental SSE framing: LF/CRLF/CR, split UTF-8, multiline data and final tail. */
export class ChatSseParser {
  private buffer = "";
  private data: string[] = [];
  private event = "message";
  constructor(private readonly emit: (event: string, payload: string) => void) {}
  push(text: string): void {
    this.buffer += text;
    if (this.buffer.length > 8 * 1024 * 1024) throw new Error("SSE frame too large");
    for (;;) {
      const i = this.buffer.search(/[\r\n]/);
      if (i < 0 || (this.buffer[i] === "\r" && i === this.buffer.length - 1)) return;
      const line = this.buffer.slice(0, i);
      const width = this.buffer[i] === "\r" && this.buffer[i + 1] === "\n" ? 2 : 1;
      this.buffer = this.buffer.slice(i + width);
      this.line(line);
    }
  }
  end(): void {
    if (this.buffer) this.line(this.buffer.replace(/\r$/, ""));
    this.buffer = "";
    this.line("");
  }
  private line(line: string): void {
    if (!line) {
      if (this.data.length || this.event === "error") this.emit(this.event, this.data.join("\n"));
      this.data = []; this.event = "message"; return;
    }
    if (line.startsWith(":")) return;
    const colon = line.indexOf(":");
    const field = colon < 0 ? line : line.slice(0, colon);
    const value = colon < 0 ? "" : line.slice(colon + 1).replace(/^ /, "");
    if (field === "data") this.data.push(value);
    else if (field === "event") this.event = value;
  }
}

export interface ChatShim {
  /** 给 Codex 用的 base_url（http://127.0.0.1:<port>） */
  readonly baseUrl: string;
  readonly upstreamBaseUrl: string;
  close(): Promise<void>;
}

function writeSse(res: import("node:http").ServerResponse, event: string, data: Json): void {
  res.write(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`);
}

/**
 * 启动 Responses→Chat 环回代理。
 * 只接受路径以 /responses 结尾的 POST；Authorization 头原样转发给上游
 * （Codex 侧 env_key 已注入真实 key，代理不持有凭证）。
 */
export async function startChatShim(upstreamBaseUrl: string, explicitReasoningEffort?:string): Promise<ChatShim> {
  const upstream = upstreamBaseUrl.replace(/\/+$/, "");
  const server: Server = createServer((req, res) => {
    void (async () => {
      if (req.method !== "POST" || !(req.url ?? "").endsWith("/responses")) {
        res.writeHead(404, { "content-type": "application/json" });
        res.end(JSON.stringify({ error: { message: "chat-shim: only POST */responses" } }));
        return;
      }
      const chunks: Buffer[] = [];
      req.on("data", (c: Buffer) => chunks.push(c));
      const raw = await new Promise<Buffer>((resolvePromise, reject) => {
        req.on("end", () => resolvePromise(Buffer.concat(chunks)));
        req.on("error", reject);
      });
      let body: Json;
      try {
        body = JSON.parse(raw.toString("utf8")) as Json;
      } catch {
        res.writeHead(400, { "content-type": "application/json" });
        res.end(JSON.stringify({ error: { message: "invalid json body" } }));
        return;
      }

      const chatBody = convertResponsesRequestToChat(body,explicitReasoningEffort);
      chatBody["stream"] = true; // Codex 侧恒流式
      const headers: Record<string, string> = { "content-type": "application/json", accept: "text/event-stream" };
      const auth = req.headers["authorization"];
      if (typeof auth === "string" && auth) headers["authorization"] = auth;

      const controller = new AbortController();
      res.on("close", () => controller.abort());
      let upstreamResp: Response;
      try {
        const dispatcher = upstreamDispatcher(upstream);
        upstreamResp = await fetch(upstream.endsWith("/chat/completions") ? upstream : `${upstream}/chat/completions`, {
          method: "POST",
          headers,
          body: JSON.stringify(chatBody),
          signal: controller.signal,
          // undici 私有扩展字段，DOM 类型不含 dispatcher（npm undici 与内建类型不兼容，经 unknown 断言）
          ...(dispatcher !== undefined ? { dispatcher } : {}),
        } as unknown as RequestInit);
      } catch (error) {
        res.writeHead(502, { "content-type": "application/json" });
        res.end(JSON.stringify({ error: { message: `chat-shim upstream unreachable: ${(error as Error).message}` } }));
        return;
      }
      if (!upstreamResp.ok || upstreamResp.body === null) {
        // 错误原样透传（Codex 对非 2xx 读取 body 报错）
        const text = await upstreamResp.text();
        res.writeHead(upstreamResp.status, { "content-type": "application/json" });
        res.end(text.slice(0, 4096));
        return;
      }

      res.writeHead(200, {
        "content-type": "text/event-stream",
        "cache-control": "no-cache",
        connection: "keep-alive",
      });
      const streamer = new ChatToResponsesStreamer();
      let sawDone = false;
      let frames = 0;
      const startedAt = Date.now();
      try {
        if ((upstreamResp.headers.get("content-type") ?? "").includes("application/json")) {
          const json = await upstreamResp.json() as Json;
          const choice = Array.isArray(json["choices"]) ? json["choices"][0] as Json | undefined : undefined;
          for (const ev of streamer.feed({ ...json, choices: choice ? [{ ...choice, delta: choice["message"] }] : [] })) writeSse(res, ev.event, ev.data);
        } else {
          const parser = new ChatSseParser((event, payload) => {
            if (sawDone) return;
            if (payload.trim() === "[DONE]") { sawDone = true; return; }
            frames++;
            if (event === "error") { streamer.fail("upstream_stream_error", "Upstream emitted an SSE error event."); return; }
            let chunk: Json;
            try {
              const parsed: unknown = JSON.parse(payload);
              if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error();
              chunk = parsed as Json;
            } catch { streamer.fail("upstream_invalid_sse", "Upstream sent malformed SSE JSON."); return; }
            for (const ev of streamer.feed(chunk)) writeSse(res, ev.event, ev.data);
          });
          const reader = upstreamResp.body.getReader();
          const decoder = new TextDecoder();
          try {
            for (;;) {
              const { done, value } = await reader.read();
              if (done) break;
              parser.push(decoder.decode(value, { stream: true }));
              if (sawDone) { await reader.cancel(); break; }
            }
            parser.push(decoder.decode());
            parser.end();
          } finally { reader.releaseLock(); }
        }
      } catch {
        streamer.fail(controller.signal.aborted ? "client_cancelled" : "upstream_stream_error",
          controller.signal.aborted ? "Client cancelled the request." : "Upstream stream interrupted or returned invalid JSON.");
      }
      const terminal = streamer.finish();
      if (!res.destroyed && !res.writableEnded) {
        for (const ev of terminal) writeSse(res, ev.event, ev.data);
        res.end();
      }
      process.stderr.write(`chat-shim-diagnostic ${JSON.stringify({ ...streamer.diagnostics(),
        upstreamStatus: upstreamResp.status, sawDone, frames, durationMs: Date.now() - startedAt })}\n`);

    })().catch(() => {
      if (!res.writableEnded) {
        res.writeHead(500);
        res.end();
      }
    });
  });
  // 扫描 LLM 回合可能很长：关掉 Node 默认的请求超时
  server.requestTimeout = 0;
  server.headersTimeout = 0;
  server.timeout = 0;

  await new Promise<void>((resolvePromise, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => resolvePromise());
  });
  const addr = server.address();
  const port = typeof addr === "object" && addr !== null ? addr.port : 0;
  return {
    baseUrl: `http://127.0.0.1:${port}`,
    upstreamBaseUrl: upstream,
    close: () =>
      new Promise<void>((resolvePromise) => {
        server.close(() => resolvePromise());
      }),
  };
}
