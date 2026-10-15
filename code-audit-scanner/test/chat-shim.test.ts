import assert from "node:assert/strict";
import { test } from "node:test";
import { createServer } from "node:http";
import {
  convertResponsesRequestToChat,
  convertChatCompletionToResponse,
  ChatToResponsesStreamer,
  ChatSseParser,
  startChatShim,
} from "../src/proxy/chat-shim.js";

/* ---------- 请求转换（cc transform_codex_chat.rs 同语义） ---------- */

test("instructions + string input → system/user messages", () => {
  const out = convertResponsesRequestToChat({
    model: "kimi-k2.7-code",
    instructions: "You are Codex.",
    input: "scan this repo",
    stream: true,
  });
  assert.equal(out["model"], "kimi-k2.7-code");
  assert.deepEqual(out["messages"], [
    { role: "system", content: "You are Codex." },
    { role: "user", content: "scan this repo" },
  ]);
  // 流式 usage 注入（缺了 token 统计全为 0 —— cc 实证）
  assert.deepEqual(out["stream_options"], { include_usage: true });
});

test("function_call / function_call_output 多轮 → assistant.tool_calls + tool 消息", () => {
  const out = convertResponsesRequestToChat({
    model: "m",
    input: [
      { type: "message", role: "user", content: [{ type: "input_text", text: "hi" }] },
      { type: "function_call", call_id: "c1", name: "shell", arguments: "{\"cmd\":\"ls\"}" },
      { type: "function_call_output", call_id: "c1", output: "file.txt" },
      { type: "message", role: "assistant", content: [{ type: "output_text", text: "done" }] },
    ],
  });
  assert.deepEqual(out["messages"], [
    { role: "user", content: "hi" },
    { role: "assistant", content: null, tool_calls: [
      { id: "c1", type: "function", function: { name: "shell", arguments: "{\"cmd\":\"ls\"}" } },
    ] },
    { role: "tool", tool_call_id: "c1", content: "file.txt" },
    { role: "assistant", content: "done" },
  ]);
});

test("developer role → system；reasoning 项跳过；max_output_tokens → max_tokens", () => {
  const out = convertResponsesRequestToChat({
    model: "m",
    max_output_tokens: 2048,
    input: [
      { type: "message", role: "developer", content: [{ type: "input_text", text: "rule" }] },
      { type: "reasoning", summary: [{ text: "thinking" }] },
    ],
  });
  assert.deepEqual(out["messages"], [{ role: "system", content: "rule" }]);
  assert.equal(out["max_tokens"], 2048);
  assert.equal(out["reasoning_effort"], undefined);
});

test("tools 转换 + tool_choice；无 tools 时剥掉 tool_choice/parallel_tool_calls", () => {
  const withTools = convertResponsesRequestToChat({
    model: "m",
    input: "x",
    tools: [{ type: "function", name: "read", description: "d", parameters: { type: "object" } }],
    tool_choice: { type: "function", name: "read" },
    parallel_tool_calls: true,
  });
  assert.deepEqual(withTools["tools"], [
    { type: "function", function: { name: "read", description: "d", parameters: { type: "object" } } },
  ]);
  assert.deepEqual(withTools["tool_choice"], { type: "function", function: { name: "read" } });

  const noTools = convertResponsesRequestToChat({
    model: "m", input: "x", tool_choice: "auto", parallel_tool_calls: true,
  });
  assert.equal(noTools["tool_choice"], undefined);
  assert.equal(noTools["parallel_tool_calls"], undefined);
});

/* ---------- 响应转换 ---------- */

test("非流式 chat completion → Responses 对象", () => {
  const r = convertChatCompletionToResponse({
    id: "chatcmpl-1",
    model: "kimi-k2.7-code",
    choices: [{ finish_reason: "stop", message: { role: "assistant", content: "found 2 issues",
      tool_calls: [{ id: "c9", type: "function", function: { name: "report", arguments: "{}" } }] } }],
    usage: { prompt_tokens: 10, completion_tokens: 5, total_tokens: 15, prompt_tokens_details: { cached_tokens: 4 } },
  });
  assert.equal(r["status"], "completed");
  const output = r["output"] as Array<Record<string, unknown>>;
  assert.equal(output[0]?.["type"], "message");
  assert.equal(output[1]?.["type"], "function_call");
  assert.equal(output[1]?.["call_id"], "c9");
  const usage = r["usage"] as Record<string, unknown>;
  assert.equal(usage["input_tokens"], 10);
  assert.deepEqual(usage["input_tokens_details"], { cached_tokens: 4 });
});

test("流式：text delta + tool call 累积 → completed 带完整 output", () => {
  const s = new ChatToResponsesStreamer();
  let events = s.feed({ model: "m", choices: [{ delta: { content: "Hel" } }] });
  assert.equal(events[0]?.event, "response.created");
  assert.ok(events.some((e) => e.event === "response.output_text.delta" && (e.data as { delta: string }).delta === "Hel"));
  events = s.feed({ choices: [{ delta: { tool_calls: [{ index: 0, id: "c1", function: { name: "shell", arguments: "{\"a\":" } }] } }] });
  assert.ok(events.some((e) => e.event === "response.output_item.added"));
  events = s.feed({ choices: [{ delta: { tool_calls: [{ index: 0, function: { arguments: "1}" } }] } }] });
  assert.ok(events.some((e) => e.event === "response.function_call_arguments.delta"));
  s.feed({ usage: { prompt_tokens: 3, completion_tokens: 2, total_tokens: 5 }, choices: [] });
  s.feed({choices: [{delta: {}, finish_reason: "tool_calls"}]});
  const fin = s.finish();
  const completed = fin.find((e) => e.event === "response.completed");
  assert.ok(completed);
  const resp = completed.data["response"] as Record<string, unknown>;
  assert.equal(resp["status"], "completed");
  const output = resp["output"] as Array<Record<string, unknown>>;
  assert.equal(output.length, 2);
  assert.equal((output[0]?.["content"] as Array<Record<string, unknown>>)[0]?.["text"], "Hel");
  assert.equal(output[1]?.["arguments"], "{\"a\":1}");
  assert.deepEqual(resp["usage"], { input_tokens: 3, output_tokens: 2, total_tokens: 5 });
});

test("上游错误 chunk → response.failed", () => {
  const s = new ChatToResponsesStreamer();
  s.feed({ error: { message: "boom", code: "x" } });
  const fin = s.finish();
  assert.equal(fin[0]?.event, "response.failed");
});

/* ---------- 环回代理端到端 ---------- */

test("shim: POST /responses 转换为 chat/completions 并流回 Responses SSE", async () => {
  // 假上游：记录收到的 body，回一条 chat SSE 流
  let upstreamBody = "";
  let upstreamAuth = "";
  const upstream = createServer((req, res) => {
    upstreamAuth = String(req.headers["authorization"] ?? "");
    const chunks: Buffer[] = [];
    req.on("data", (c: Buffer) => chunks.push(c));
    req.on("end", () => {
      upstreamBody = Buffer.concat(chunks).toString("utf8");
      res.writeHead(200, { "content-type": "text/event-stream" });
      res.write('data: {"model":"kimi-k2.7-code","choices":[{"delta":{"content":"ok"}}]}\n\n');
      res.write('data: {"choices":[],"usage":{"prompt_tokens":7,"completion_tokens":1,"total_tokens":8}}\n\n');
      res.write('data: {"choices":[{"delta":{},"finish_reason":"stop"}]}\n\n');
      res.write("data: [DONE]\n\n");
      res.end();
    });
  });
  await new Promise<void>((r) => upstream.listen(0, "127.0.0.1", r));
  const upAddr = upstream.address();
  const upPort = typeof upAddr === "object" && upAddr !== null ? upAddr.port : 0;

  const shim = await startChatShim(`http://127.0.0.1:${upPort}/v1`);
  try {
    const resp = await fetch(`${shim.baseUrl}/v1/responses`, {
      method: "POST",
      headers: { "content-type": "application/json", authorization: "Bearer sk-test" },
      body: JSON.stringify({ model: "kimi-k2.7-code", instructions: "sys", input: "hi", stream: true }),
    });
    assert.equal(resp.status, 200);
    const text = await resp.text();
    // 上游收到的是 chat 格式
    const sent = JSON.parse(upstreamBody) as Record<string, unknown>;
    assert.deepEqual(sent["messages"], [
      { role: "system", content: "sys" },
      { role: "user", content: "hi" },
    ]);
    assert.equal(sent["stream"], true);
    assert.equal(upstreamAuth, "Bearer sk-test");
    // 下游收到 Responses SSE
    assert.match(text, /event: response\.created/);
    assert.match(text, /event: response\.output_text\.delta\ndata: .*"delta":"ok"/);
    assert.match(text, /event: response\.completed/);
  } finally {
    await shim.close();
    upstream.close();
  }
});

for (const [reason, code] of [["length", "upstream_output_truncated"], ["content_filter", "upstream_content_filter"], [null, "upstream_stream_incomplete"], ["other", "upstream_unknown_finish_reason"]]) {
  test(`never complete a partial turn: ${reason}`, () => {
    const s = new ChatToResponsesStreamer();
    s.feed({choices: [{delta: {content: "partial"}, finish_reason: reason}]});
    const terminal = s.finish();
    assert.equal(terminal.at(-1)?.event, "response.failed");
    assert.equal(((terminal.at(-1)?.data["response"] as any).error).code, code);
    assert.deepEqual(s.finish(), []);
  });
}

test("reasoning-only response is not a completed scan answer", () => {
  const s = new ChatToResponsesStreamer();
  s.feed({choices: [{delta: {reasoning_content: "internal analysis"}, finish_reason: "stop"}]});
  assert.equal(s.finish().at(-1)?.event, "response.failed");
  assert.equal(s.diagnostics()["reasoningCharacters"], 17);
  assert.doesNotMatch(JSON.stringify(s.diagnostics()), /internal analysis/);
});

test("late identity, empty continuation and parallel call IDs are preserved", () => {
  const s = new ChatToResponsesStreamer();
  assert.ok(!s.feed({choices: [{delta: {tool_calls: [{index: 0, function: {arguments: '{"x":'}}]}}]}).some(e => e.event === "response.output_item.added"));
  s.feed({choices: [{delta: {tool_calls: [{index: 0, id: "first", function: {name: "read", arguments: "1}"}}]}}]});
  s.feed({choices: [{delta: {tool_calls: [{index: 0, id: "", function: {name: "", arguments: ""}}, {id: "second", function: {name: "list", arguments: "{}"}}]}, finish_reason: "tool_calls"}]});
  const result = s.finish().at(-1)?.data["response"] as any;
  assert.equal(result.status, "completed");
  assert.deepEqual(result.output.map((t: any) => [t.call_id, t.name, t.arguments]), [["first", "read", '{"x":1}'], ["second", "list", "{}"]]);
});

for (const fn of [{arguments: "{}"}, {name: "shell", arguments: "{"}]) {
  test(`malformed tool never executes: ${JSON.stringify(fn)}`, () => {
    const s = new ChatToResponsesStreamer();
    s.feed({choices: [{delta: {tool_calls: [{index: 0, id: "c", function: fn}]}, finish_reason: "tool_calls"}]});
    const events = s.finish();
    assert.equal(events.at(-1)?.event, "response.failed");
    assert.ok(!events.some(e => e.event === "response.function_call_arguments.done"));
  });
}

test("validation JSON schema is forwarded to Chat", () => {
  const format = {type: "json_schema", name: "validation", strict: true, schema: {type: "object", additionalProperties: false}};
  assert.deepEqual(convertResponsesRequestToChat({text: {format}})["response_format"], {type: "json_schema", json_schema: {name: format.name, strict: true, schema: format.schema}});
});

const frame = (value: unknown) => `data: ${JSON.stringify(value)}\r\n\r\n`;
const answer = {choices: [{delta: {content: "验证完成"}, finish_reason: "stop"}], usage: {prompt_tokens: 4, completion_tokens: 2, total_tokens: 6}};
for (const fixture of [
  {name: "CRLF byte chunks and usage on final choice", body: frame(answer) + "data: [DONE]\r\n\r\n", ok: true},
  {name: "final frame without blank line", body: frame(answer).trimEnd(), ok: true},
  {name: "multiline data and comments", body: ': heartbeat\r\ndata: {"choices":\r\ndata: [{"delta":{"content":"ok"},"finish_reason":"stop"}]}\r\n\r\n', ok: true},
  {name: "EOF without finish", body: frame({choices: [{delta: {content: "unfinished"}}]}), ok: false},
  {name: "malformed SSE", body: 'data: {bad}\n\n' + frame(answer), ok: false},
  {name: "named SSE error", body: 'event: error\ndata: {"message":"broken"}\n\n', ok: false},
  {name: "JSON fallback", json: true, body: JSON.stringify({choices: [{message: {content: "ok"}, finish_reason: "stop"}]}), ok: true},
  {name: "JSON error", json: true, body: JSON.stringify({error: {code: "bad", message: "failed"}}), ok: false},
  {name: "JSON empty", json: true, body: JSON.stringify({choices: [{message: {}, finish_reason: "stop"}]}), ok: false},
  {name: "truncated tool", body: frame({choices: [{delta: {tool_calls: [{index: 0, id: "c", function: {name: "shell", arguments: "{"}}]}, finish_reason: "length"}]}), ok: false},
]) {
  test(`HTTP conversion: ${fixture.name}`, async () => {
    const server = createServer((_req, res) => {
      res.writeHead(200, {"content-type": fixture.json ? "application/json" : "text/event-stream"});
      // Send through real HTTP and split every byte, including UTF-8 and CRLF.
      for (const byte of Buffer.from(fixture.body)) res.write(Buffer.from([byte]));
      res.end();
    });
    await new Promise<void>(r => server.listen(0, "127.0.0.1", r));
    const port = (server.address() as any).port;
    const shim = await startChatShim(`http://127.0.0.1:${port}/chat/completions`);
    try {
      const res = await fetch(`${shim.baseUrl}/responses`, {method: "POST", body: JSON.stringify({input: "fixture", stream: true})});
      const text = await res.text();
      assert.match(text, fixture.ok ? /event: response.completed/ : /event: response.failed/);
      assert.doesNotMatch(text, fixture.ok ? /event: response.failed/ : /event: response.completed/);
    } finally { await shim.close(); server.closeAllConnections(); await new Promise<void>(r => server.close(() => r())); }
  });
}

test("SSE parser preserves frames across every possible character split", () => {
  const data = ': ping\r\nevent: message\r\ndata: {"a":\r\ndata: 1}\r\n\r\ndata: [DONE]';
  for (let split = 0; split <= data.length; split++) {
    const frames: string[] = [];
    const parser = new ChatSseParser((_event, payload) => frames.push(payload));
    parser.push(data.slice(0, split)); parser.push(data.slice(split)); parser.end();
    assert.deepEqual(frames, ['{"a":\n1}', '[DONE]']);
  }
});

test("parallel tools are published in order when identity arrives late", () => {
  const s = new ChatToResponsesStreamer();
  const events = [
    ...s.feed({choices: [{delta: {tool_calls: [{index: 0, id: "a", function: {arguments: "{}"}}]}}]}),
    ...s.feed({choices: [{delta: {tool_calls: [{index: 1, id: "b", function: {name: "second", arguments: "{}"}}]}}]}),
    ...s.feed({choices: [{delta: {tool_calls: [{index: 0, function: {name: "first"}}]}, finish_reason: "tool_calls"}]}),
  ];
  assert.deepEqual(events.filter(e => e.event === "response.output_item.added").map(e => (e.data["item"] as any).call_id), ["a", "b"]);
  assert.equal(s.finish().at(-1)?.event, "response.completed");
});
