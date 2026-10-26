import {validate} from "@axiom/contracts";
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
  if ((task.wireApi !== undefined && task.wireApi !== "responses") || (task.resolvedModel && task.resolvedModel.wireApi !== "responses")) throw new Error("Chat 协议适配已移除；请配置支持 Responses 的模型接口。");
  if(!['standard','deep'].includes(task.scanMode))throw new Error("Unsupported scan mode");
  if (task.workers !== undefined && (task.scanMode!=="deep" || !Number.isSafeInteger(task.workers) || task.workers < 1)) {
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
  if(task.gitTarget){validate("GitDiffTarget",task.gitTarget);if(task.scanMode!=="standard"||task.includePaths?.length||!task.sourceContentHash||!task.sourceSnapshotId)throw new Error("Git diff requires frozen source identity and Standard mode");}
  if(task.componentScan){validate('ComponentScanRequest',task.componentScan,{taskId:String(task.taskId),attempt:task.executionAttempt??1});if(task.componentPlanning||task.sdkOperation||task.gitTarget||task.scanMode!=='standard'||task.includePaths?.length||task.scanFocus?.length||task.scanInput?.knowledge.length||task.parentState||task.maxCostUsd===undefined||task.workers!==undefined||task.componentScan.planVersion.source.contentHash!==task.sourceContentHash||task.componentScan.planVersion.source.snapshotId!==task.sourceSnapshotId)throw new Error('组件审计须使用冻结计划和预算，不能混合其他目标或操作');}
  if(task.componentPlanning){validate('ComponentPlanningOperation',task.componentPlanning,{taskId:String(task.taskId),attempt:task.executionAttempt??1});if(task.sdkOperation||task.gitTarget||task.scanMode!=='standard'||task.includePaths?.length||task.scanInput?.knowledge.length||task.parentState||task.maxCostUsd!==undefined)throw new Error('组件规划不得混合扫描目标、预算或其他 SDK 操作');}
  if(task.sdkParameters){validate('SDKParametersAssignment',task.sdkParameters,{taskId:String(task.taskId),attempt:task.executionAttempt??1});if(task.sdkParameters.parameters.componentInput&&!task.componentScan)throw new Error('Component SDK inputs require frozen component execution');}
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
  // Finding JSON is evidence, never a credential source. Validate its envelope
  // before excluding only that data field from configuration-secret checks.
  const {sdkOperation,componentPlanning,sdkIdentity,...configuration}=task;
 if(sdkIdentity)validate("SDKTaskIdentity",sdkIdentity,{taskId:String(task.taskId),attempt:task.executionAttempt??1});
  if(componentPlanning)validate('ComponentPlanningOperation',componentPlanning,{taskId:String(task.taskId),attempt:task.executionAttempt??1});
  let checked:unknown=configuration;
  if(sdkOperation){
    validate('SDKOperation',sdkOperation,{taskId:String(task.taskId),attempt:task.executionAttempt??1});
    const {finding,...operationMetadata}=sdkOperation;
    checked={...configuration,sdkOperation:operationMetadata};
  }
  const json = JSON.stringify(checked).toLowerCase();
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
