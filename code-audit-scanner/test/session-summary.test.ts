import assert from "node:assert/strict";
import { test } from "node:test";
import {
  cleanCommand,
  formatActivityLine,
  summarizeSessionEvent,
} from "../src/runtime/session-summary.js";

function wrap(event: Record<string, unknown>) {
  return { threadId: "t1", parentThreadId: null, event };
}

test("drops protocol noise", () => {
  assert.equal(summarizeSessionEvent(wrap({ type: "session_meta", payload: { id: "x" } })), null);
  assert.equal(summarizeSessionEvent(wrap({ type: "world_state", payload: { full: true } })), null);
  assert.equal(summarizeSessionEvent(wrap({ type: "turn_context", payload: { model: "m" } })), null);
  assert.equal(
    summarizeSessionEvent(wrap({ type: "event_msg", payload: { type: "token_count", info: null } })),
    null,
  );
  assert.equal(
    summarizeSessionEvent(
      wrap({ type: "event_msg", payload: { type: "item_completed", item: { type: "CommandExecution" } } }),
    ),
    null,
  );
  assert.equal(
    summarizeSessionEvent(
      wrap({
        type: "response_item",
        payload: { type: "function_call", name: "exec_command", arguments: "{}" },
      }),
    ),
    null,
  );
});

test("keeps reasoning and task start, skips prompts and assistant (activity covers those)", () => {
  assert.equal(
    summarizeSessionEvent(
      wrap({
        type: "response_item",
        payload: {
          type: "message",
          role: "developer",
          content: [{ type: "input_text", text: "<skills_instructions>" }],
        },
      }),
    ),
    null,
  );
  assert.equal(
    summarizeSessionEvent(
      wrap({
        type: "response_item",
        payload: {
          type: "message",
          role: "assistant",
          content: [{ type: "output_text", text: "I’ll load the scan workflow." }],
        },
      }),
    ),
    null,
  );
  assert.equal(
    summarizeSessionEvent(
      wrap({
        type: "response_item",
        payload: { type: "reasoning", summary: "checking exec fork" },
      }),
    ),
    "[reasoning] checking exec fork",
  );
  assert.equal(
    summarizeSessionEvent(
      wrap({ type: "event_msg", payload: { type: "task_started", turn_id: "abc" } }),
    ),
    "[system] 主任务执行轮次开始（不代表重新扫描）",
  );
});

test("never dumps raw JSON", () => {
  const line = summarizeSessionEvent(
    wrap({
      type: "event_msg",
      payload: { type: "unknown_future", nested: { a: 1 } },
    }),
  );
  assert.equal(line, null);
});

test("strips powershell wrapper from commands", () => {
  const raw =
    '"C:\\\\Windows\\\\System32\\\\WindowsPowerShell\\\\v1.0\\\\powershell.exe" -NoProfile -Command \'Get-Content -Raw $p\'';
  assert.match(cleanCommand(raw), /Get-Content/);
  assert.equal(cleanCommand(raw).includes("powershell.exe"), false);
  assert.equal(
    formatActivityLine({ status: "running", kind: "command", description: "Get-Item Env:FOO" }),
    "running command Get-Item Env:FOO",
  );
});

test("worker task_complete errors survive session filtering without dumping the payload", () => {
  const line = summarizeSessionEvent(wrap({type:"event_msg",payload:{type:"task_complete",error:{message:"Missing environment variable: `DEEPSEEK_API_KEY`.",codex_error_info:"other"},last_agent_message:null}}));
  assert.match(line!,/\[error\].*DEEPSEEK_API_KEY/u);
  assert.ok(!line!.includes("last_agent_message"));
  assert.equal(summarizeSessionEvent(wrap({type:"event_msg",payload:{type:"task_complete",error:null}})),null);
});
