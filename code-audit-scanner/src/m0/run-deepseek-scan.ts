/**
 * M0 DeepSeek standard scan launcher.
 * Reads DEEPSEEK_API_KEY from the environment. Does not accept the key on argv.
 */
import { mkdir } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { isolatedScanEnvironment } from "../runtime/scan-environment.js";
import type { ScanTask } from "../contract/scan-task.js";

const KEY = process.env["DEEPSEEK_API_KEY"];
if (!KEY) {
  process.stderr.write("Missing DEEPSEEK_API_KEY in the environment.\n");
  process.exit(3);
}

const root = path.dirname(fileURLToPath(import.meta.url));
const scannerRoot = path.resolve(root, "../..");
const repoRoot = path.resolve(scannerRoot, "..");
const localAppData = process.env["LOCALAPPDATA"] ?? path.join(os.homedir(), "AppData", "Local");
const stateDir = path.join(localAppData, "code-audit", "m0-deepseek-state");
const outputDir = path.join(localAppData, "code-audit", "m0-deepseek-standard");

await mkdir(stateDir, { recursive: true, mode: 0o700 });
await mkdir(outputDir, { recursive: true, mode: 0o700 });

const isolationTask: ScanTask = {
  taskId: 1,
  sourcePath: path.join(repoRoot, "test-fixtures"),
  outputPath: outputDir,
  provider: "deepseek",
  model: "deepseek-v4-flash-vision-exp",
  scanMode: "standard",
  timeoutMinutes: 90,
};

const child = spawn(
  process.execPath,
  [
    "--import",
    "tsx",
    path.join(scannerRoot, "src", "main.ts"),
    "--provider",
    "deepseek",
    "--source",
    path.join(repoRoot, "test-fixtures"),
    "--output",
    outputDir,
    "--timeout",
    "90",
  ],
  {
    cwd: scannerRoot,
    stdio: "inherit",
    env: {
      ...isolatedScanEnvironment(isolationTask),
      DEEPSEEK_API_KEY: KEY,
      CODEX_SECURITY_STATE_DIR: stateDir,
    },
  },
);

child.on("exit", (code, signal) => {
  if (signal) process.exit(1);
  process.exit(code ?? 1);
});
