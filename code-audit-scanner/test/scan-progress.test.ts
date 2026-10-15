import assert from "node:assert/strict";
import { mkdtemp, readFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { ScanProgressSink } from "../src/runtime/scan-progress.js";

test("init truncates leftover events.jsonl from a previous run", async () => {
  const dir = await mkdtemp(path.join(os.tmpdir(), "axiom-progress-"));
  const first = new ScanProgressSink(dir, 1);
  await first.init();
  await first.activity("old run leftover");
  const second = new ScanProgressSink(dir, 1);
  await second.init();
  const events = (await readFile(path.join(dir, "logs", "events.jsonl"), "utf8"))
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line) as { message: string });
  assert.equal(events.length, 1);
  assert.equal(events[0]?.message, "scan started");
});

test("writes progress.json and append-only events.jsonl", async () => {
  const dir = await mkdtemp(path.join(os.tmpdir(), "axiom-progress-"));
  const sink = new ScanProgressSink(dir, 42);
  await sink.init();
  await sink.progress({ phase: "discovery", filesCompleted: 1, filesTotal: 5 });
  await sink.activity("running tool rg");
  await sink.cost(0);
  await sink.finish("completed");

  const progress = JSON.parse(
    await readFile(path.join(dir, "state", "progress.json"), "utf8"),
  ) as {
    taskId: number;
    phase: string;
    filesCompleted: number;
    filesTotal: number;
    activity: string;
  };
  assert.equal(progress.taskId, 42);
  assert.equal(progress.phase, "completed");
  assert.equal(progress.filesCompleted, 1);
  assert.equal(progress.filesTotal, 5);
  assert.match(progress.activity, /rg/);

  const events = (await readFile(path.join(dir, "logs", "events.jsonl"), "utf8"))
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line) as { kind: string; message: string });
  assert.ok(events.length >= 4);
  assert.equal(events[0]?.kind, "system");
  assert.ok(events.some((event) => event.kind === "progress"));
  assert.ok(events.some((event) => event.kind === "activity"));
  assert.ok(events.some((event) => event.message.includes("scan completed")));
});

test("session events carry thread/worker and kind session", async () => {
  const dir = await mkdtemp(path.join(os.tmpdir(), "axiom-progress-"));
  const sink = new ScanProgressSink(dir, 7);
  await sink.init();
  await sink.session("thr-1", 3, "[reasoning] checking exec fork");
  const events = (await readFile(path.join(dir, "logs", "events.jsonl"), "utf8"))
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line) as { kind: string; message: string });
  const session = events.find((e) => e.kind === "session");
  assert.ok(session, "session event missing");
  assert.ok(session.message.includes("thread=thr-1"));
  assert.ok(session.message.includes("w3"));
  assert.ok(session.message.includes("checking exec fork"));
});

test("redacts secrets in activity lines", async () => {
  const dir = await mkdtemp(path.join(os.tmpdir(), "axiom-progress-"));
  const sink = new ScanProgressSink(dir, 1);
  await sink.init();
  await sink.activity("Authorization: Bearer abcdefghijklmnop.secret");
  const progress = JSON.parse(
    await readFile(path.join(dir, "state", "progress.json"), "utf8"),
  ) as { activity: string };
  assert.equal(progress.activity.includes("abcdefghijklmnop"), false);
  assert.match(progress.activity, /REDACTED/);
});

test("worker failures are error-level events with thread identity", async () => {
  const dir=await mkdtemp(path.join(os.tmpdir(),"axiom-worker-error-"));
  const sink=new ScanProgressSink(dir,1);await sink.init();
  await sink.session("thread-1",4,"[error] 工作线程失败：Missing environment variable: DEEPSEEK_API_KEY");
  const events=(await readFile(path.join(dir,"logs/events.jsonl"),"utf8")).trim().split("\n").map(line=>JSON.parse(line));
  assert.equal(events.at(-1).level,"error");assert.match(events.at(-1).message,/thread=thread-1 w4/);
});

test("deep counters are distinct from files and reconnect metadata survives completion",async()=>{
 const dir=await mkdtemp(path.join(os.tmpdir(),"axiom-deep-progress-"));const sink=new ScanProgressSink(dir,12);await sink.init();
 await sink.progress({phase:"preflight",filesCompleted:0,filesTotal:1});
 await sink.deepProgress({completed:3,active:4,maximum:100});
 await sink.deepProgress({completed:3,active:4,maximum:100});
 await sink.workerStatus({kind:"dispatch",phase:"validation",planned:2,started:1});
 await sink.reconnect(1,3,{reason:"rate_limit",retryAfterSeconds:5});
 await sink.finish("partial");
 const value=JSON.parse(await readFile(sink.progressPath,"utf8"));
 assert.equal(value.phase,"partial");assert.equal(value.filesCompleted,0);assert.equal(value.deep.completed,3);assert.equal(value.deep.maximum,100);assert.equal(value.reconnect.reason,"rate_limit");
 const logs=(await readFile(sink.eventsPath,"utf8")).trim().split("\n").map(x=>JSON.parse(x));assert.equal(logs.filter(x=>x.message.startsWith("深度扫描")).length,1);
});
