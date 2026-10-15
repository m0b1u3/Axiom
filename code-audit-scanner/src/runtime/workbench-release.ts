import { existsSync } from "node:fs";
import { homedir } from "node:os";
import path from "node:path";
import { DatabaseSync } from "node:sqlite";

/**
 * Codex Security workbench 把每次扫描登记成 status=running。
 * Agent 取消是杀进程，SDK 来不及 fail-scan，行会一直占着同一 scan_dir。
 * 下一轮 archiveExisting 看到 running 就拒绝归档。
 *
 * 只释放「本输出目录」上还标 running 的行，不动别的扫描。
 */
export function workbenchDatabasePath(
  env: NodeJS.ProcessEnv = process.env,
): string {
  const stateDir = env["CODEX_SECURITY_STATE_DIR"];
  if (stateDir) return path.join(stateDir, "workbench.sqlite3");
  const home = env["CODEX_HOME"] ?? path.join(homedir(), ".codex");
  return path.join(home, "state", "plugins", "codex-security", "workbench.sqlite3");
}

export function releaseStaleWorkbenchScan(
  scanDir: string,
  env: NodeJS.ProcessEnv = process.env,
): number {
  const dbPath = workbenchDatabasePath(env);
  if (!existsSync(dbPath)) return 0;

  const variants = uniqueDirs(scanDir);
  const now = new Date().toISOString();
  const message = "Interrupted by Axiom; releasing scan directory for retry.";

  const db = new DatabaseSync(dbPath);
  try {
    const stmt = db.prepare(
      `UPDATE scans
       SET status = 'failed',
           failure_message = ?,
           completed_at = ?,
           updated_at = ?
       WHERE status = 'running'
         AND scan_dir = ?`,
    );
    let changes = 0;
    for (const dir of variants) {
      const result = stmt.run(message, now, now, dir);
      changes += Number(result.changes);
    }
    return changes;
  } finally {
    db.close();
  }
}

function uniqueDirs(scanDir: string): string[] {
  const posix = scanDir.replaceAll("\\", "/");
  const win = posix.replaceAll("/", "\\");
  return [...new Set([scanDir, posix, win])];
}
