import type { ScanTask } from "./scan-task.js";
import { normalizeFocuses } from "./scan-prompt.js";
import path from "node:path";

const SECRET_FIELD_NAMES = ["apikey", "api_key", "authorization", "secret", "token", "password"];

export function validateScanTask(task: ScanTask): void {
  if (typeof task.taskId==="string" ? !/^[1-9][0-9]{0,18}$/.test(task.taskId) : !Number.isSafeInteger(task.taskId) || task.taskId <= 0) {
    throw new Error("ScanTask.taskId must be a positive integer");
  }
  if (!task.sourcePath) {
    throw new Error("ScanTask.sourcePath is required");
  }
  if (!task.outputPath) {
    throw new Error("ScanTask.outputPath is required");
  }
  const paths=/^[A-Za-z]:|^\\\\/.test(task.sourcePath)?path.win32:path.posix;
  const relative=paths.relative(paths.resolve(task.sourcePath),paths.resolve(task.outputPath));
  if (pathsEqual(task.sourcePath, task.outputPath) || relative==="" || (relative!==".."&&!relative.startsWith(".."+paths.sep)&&!paths.isAbsolute(relative))) {
    throw new Error("ScanTask.outputPath must be outside sourcePath");
  }
  if (!Number.isFinite(task.timeoutMinutes) || task.timeoutMinutes <= 0 || task.timeoutMinutes > 600) {
    throw new Error("ScanTask.timeoutMinutes must be positive");
  }
  if(!['standard','deep'].includes(task.scanMode))throw new Error("Unsupported scan mode");
  if (task.workers !== undefined && (task.scanMode!=="deep" || !Number.isInteger(task.workers) || task.workers < 1 || task.workers > 64)) {
    throw new Error("ScanTask.workers must be >= 1 for deep scans");
  }
  if(task.maxCostUsd!==undefined&&(!Number.isFinite(task.maxCostUsd)||task.maxCostUsd<=0||task.maxCostUsd>Number.MAX_SAFE_INTEGER))throw new Error("ScanTask.maxCostUsd must be a finite positive number");
  if(task.timeBudget!==undefined&&(!Number.isInteger(task.timeBudget.scanSeconds)||task.timeBudget.scanSeconds<1||task.timeBudget.scanSeconds>36000||!Number.isInteger(task.timeBudget.finalizationGraceSeconds)||task.timeBudget.finalizationGraceSeconds<0||task.timeBudget.finalizationGraceSeconds>600||(task.scanMode==='standard'&&task.timeBudget.finalizationGraceSeconds!==0)))throw new Error("Invalid execution time budget");
  if (task.mock && task.scanMode === "deep") {
    throw new Error("ScanTask.mock cannot be combined with deep scan");
  }
  if (task.validationMode !== undefined && !["default","exploitability"].includes(task.validationMode)) throw new Error("Unsupported validation mode");
  if (task.validationMode === "exploitability" && task.scanMode !== "standard") throw new Error("Independent exploitability validation requires standard mode");
  if (task.businessContext !== undefined && (typeof task.businessContext !== "string" || [...task.businessContext].length > 4000 || task.businessContext.includes("\0") || (task.businessContext.trim() !== "" && task.validationMode !== "exploitability"))) throw new Error("Invalid business context");
  validateIncludePaths(task.includePaths);
  if (task.scanFocus !== undefined) normalizeFocuses(task.scanFocus);
  assertNoSecretOnTask(task);
}

/** 路径子集白名单：相对路径、禁止绝对路径/`..`/空元素/盘符/反斜杠。 */
export function validateIncludePaths(paths: string[] | undefined): void {
  if (paths === undefined) return;
  if (!Array.isArray(paths)) {
    throw new Error("ScanTask.includePaths must be an array of relative paths");
  }
  const seen = new Set<string>();
  for (const raw of paths) {
    if (typeof raw !== "string" || raw.length === 0) {
      throw new Error("ScanTask.includePaths elements must be nonempty strings");
    }
    const p = raw.replaceAll("\\", "/");
    if (/^[A-Za-z]:/.test(p) || p.startsWith("/")) {
      throw new Error(`ScanTask.includePaths must be relative paths, got: ${raw}`);
    }
    for (const seg of p.split("/")) {
      if (seg === "" || seg === ".") continue;
      if (seg === "..") {
        throw new Error(`ScanTask.includePaths must not contain '..', got: ${raw}`);
      }
    }
    const norm = normalizeIncludePath(p);
    if (seen.has(norm)) {
      throw new Error(`ScanTask.includePaths has duplicate entry: ${raw}`);
    }
    seen.add(norm);
  }
}

/** 规范化 includePath：反斜杠转正斜杠、去尾斜杠。 */
export function normalizeIncludePath(p: string): string {
  return p.replaceAll("\\", "/").replace(/\/+$/, "");
}

export function assertNoSecretOnTask(task: ScanTask): void {
  const json = JSON.stringify(task).toLowerCase();
  for (const needle of SECRET_FIELD_NAMES) {
    if (json.includes(`"${needle}"`)) {
      throw new Error(`ScanTask must not carry secret field matching ${needle}`);
    }
  }
}

function pathsEqual(left: string, right: string): boolean {
  return left.replaceAll("\\", "/").replace(/\/+$/, "") ===
    right.replaceAll("\\", "/").replace(/\/+$/, "");
}
