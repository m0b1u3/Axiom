import {readFile,stat} from "node:fs/promises";
import {COMPATIBILITY,decode,type ScannerJob,type ScannerJobV2,type ScannerJobV3} from "@axiom/contracts";
import type {ScanTask} from "./scan-task.js";

export async function loadScannerJob(file: string): Promise<ScannerJob|ScannerJobV2|ScannerJobV3> {
  if ((await stat(file)).size > COMPATIBILITY.limits.documentBytes) throw new Error("Scanner job exceeds the document size limit");
  const raw=await readFile(file);
  const version=(JSON.parse(raw.toString("utf8")) as {schemaVersion?:unknown}).schemaVersion;
  return version===3?decode("ScannerJobV3",raw):version===2?decode("ScannerJobV2",raw):decode("ScannerJob",raw);
}

/** Maps only validated execution input. Credentials stay in process.env. */
export function scanTaskFromJob(job: ScannerJob|ScannerJobV2|ScannerJobV3): ScanTask {
  return {
    taskId:job.execution.taskId,
    ...(job.schemaVersion>=2?{resultSchemaVersion:(job as ScannerJobV2).resultSchemaVersion,resolvedModel:job.model}:{}),
    ...(job.schemaVersion===3?{progressSchemaVersion:job.progressSchemaVersion,eventSchemaVersion:job.eventSchemaVersion}:{}),
    executionAttempt:job.execution.attempt,
    sourcePath:job.source.path,
    outputPath:job.output.path,
    provider:job.model.providerId,
    connectionType:job.model.connectionType,
    model:job.model.model,
    credentialEnv:job.model.credentialEnv,
    wireApi:job.model.wireApi,
    configurationRevision:job.model.configurationRevision,
    ...(job.model.baseUrl!==undefined?{baseUrl:job.model.baseUrl}:{}),
    scanMode:job.policy.mode,
    timeoutMinutes:job.policy.timeBudget.scanSeconds/60,
    timeBudget:job.policy.timeBudget,
    includePaths:[...job.policy.includePaths],
    scanFocus:[...job.policy.focus],
    validationMode:job.policy.validation.mode,
    ...(job.policy.validation.businessContext!==undefined?{businessContext:job.policy.validation.businessContext}:{}),
    ...(job.policy.maxCostUsd!==undefined?{maxCostUsd:job.policy.maxCostUsd}:{}),
    ...(job.policy.workers!==undefined?{workers:job.policy.workers}:{}),
    ...(job.policy.deepMaxDiscoveryRuns!==undefined?{deepMaxDiscoveryRuns:job.policy.deepMaxDiscoveryRuns}:{}),
    ...(job.policy.deepStopAfterNoNew!==undefined?{deepStopAfterNoNew:job.policy.deepStopAfterNoNew}:{}),
  };
}
