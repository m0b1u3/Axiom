import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { DatabaseSync } from "node:sqlite";
import { test } from "node:test";
import {
  releaseStaleWorkbenchScan,
  workbenchDatabasePath,
} from "../src/runtime/workbench-release.js";

test("workbenchDatabasePath prefers CODEX_SECURITY_STATE_DIR", () => {
  assert.equal(
    workbenchDatabasePath({ CODEX_SECURITY_STATE_DIR: "C:\\state" }),
    path.join("C:\\state", "workbench.sqlite3"),
  );
});

test("releaseStaleWorkbenchScan only fails the matching running row", async () => {
  const dir = await mkdtemp(path.join(tmpdir(), "axiom-workbench-"));
  const dbPath = path.join(dir, "workbench.sqlite3");
  const db = new DatabaseSync(dbPath);
  db.exec(`
    CREATE TABLE scans (
      id TEXT PRIMARY KEY,
      scan_dir TEXT,
      status TEXT,
      failure_message TEXT,
      completed_at TEXT,
      updated_at TEXT
    );
  `);
  db.prepare("INSERT INTO scans (id, scan_dir, status) VALUES (?, ?, ?)").run(
    "keep-running",
    path.join("F:", "other", "codex-security"),
    "running",
  );
  const target = path.join("F:", "scans", "19", "raw", "codex-security");
  db.prepare("INSERT INTO scans (id, scan_dir, status) VALUES (?, ?, ?)").run(
    "stale",
    target,
    "running",
  );
  db.prepare("INSERT INTO scans (id, scan_dir, status) VALUES (?, ?, ?)").run(
    "done",
    target,
    "complete",
  );
  db.close();

  const n = releaseStaleWorkbenchScan(target, { CODEX_SECURITY_STATE_DIR: dir });
  assert.equal(n, 1);

  const check = new DatabaseSync(dbPath);
  const rows = (
    check.prepare("SELECT id, status FROM scans ORDER BY id").all() as Array<{
      id: string;
      status: string;
    }>
  ).map((row) => ({ id: row.id, status: row.status }));
  check.close();
  assert.deepEqual(rows, [
    { id: "done", status: "complete" },
    { id: "keep-running", status: "running" },
    { id: "stale", status: "failed" },
  ]);

  await rm(dir, { recursive: true, force: true });
});
