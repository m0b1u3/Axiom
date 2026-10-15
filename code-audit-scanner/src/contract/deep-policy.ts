import type { ScanTask } from "./scan-task.js";
/** Native discovery deadline precedes the hard deadline, leaving bounded time to aggregate. */
export const DEEP_FINALIZE_MINUTES = 10;
export function hardTimeoutMinutes(task: Pick<ScanTask, "scanMode" | "timeoutMinutes" | "timeBudget">): number {
  if(task.timeBudget)return (task.timeBudget.scanSeconds+task.timeBudget.finalizationGraceSeconds)/60;
  return task.timeoutMinutes + (task.scanMode === "deep" ? DEEP_FINALIZE_MINUTES : 0);
}
export function deepScanOptions(task: ScanTask): {maxTimeHours?: number; maxDiscoveryRuns?: number; stopAfterNoNew?: number} {
  if (task.scanMode !== "deep") {
    if (task.deepMaxDiscoveryRuns !== undefined || task.deepStopAfterNoNew !== undefined) throw new Error("Deep scan limits require deep mode");
    return {};
  }
  for (const n of [task.deepMaxDiscoveryRuns, task.deepStopAfterNoNew]) {
    if (n !== undefined && (!Number.isInteger(n) || n < 1 || n > 1000)) throw new Error("Deep scan counts must be integers between 1 and 1000");
  }
  return {maxTimeHours: task.timeoutMinutes / 60,
    ...(task.deepMaxDiscoveryRuns !== undefined ? {maxDiscoveryRuns: task.deepMaxDiscoveryRuns} : {}),
    ...(task.deepStopAfterNoNew !== undefined ? {stopAfterNoNew: task.deepStopAfterNoNew} : {})};
}
