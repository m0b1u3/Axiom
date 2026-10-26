import { hardTimeoutMinutes } from "./contract/deep-policy.js";
import { mkdir } from "node:fs/promises";
import path from "node:path";
import { enterSDKWorkingDirectory } from "./runtime/sdk-working-directory.js";
import { superviseAgent } from "./runtime/agent-lifecycle.js";
import { flagEnabled, parseArgs } from "./cli/args.js";
import { describeScanner } from "./runtime/describe-scanner.js";
import type { ScanMode, ScanProvider, ScanTask } from "./contract/scan-task.js";
import { resolveProvider } from "./provider/model-provider.js";
import {
  defaultFixtureManifestPath,
  loadFixtureManifest,
  scoreFixtures,
} from "./eval/fixture-score.js";

/**
 * 最小 CLI：读环境变量拼 ScanTask，跑 CodexSecurityEngine。
 *
 *   OPENAI_API_KEY=... npm run scan -- --source ../test-fixtures --output ./m0-out --task-id 1
 *   npm run m0:mock -- --source ../test-fixtures --output ./m0-out/mock
 *
 * API Key 只允许来自环境变量，禁止 argv。
 */
async function main(): Promise<void> {
  const args = parseArgs(process.argv.slice(2));
  if (flagEnabled(args["describe"])) {
    process.stdout.write(`${JSON.stringify(await describeScanner(), null, 2)}\n`);
    return;
  }
  if(args['export-saved-interim']!==undefined){if(Object.keys(args).length!==1)throw new Error('Evidence export cannot be mixed with scans');await (await import('./runtime/interim-findings.js')).exportSavedInterim(args['export-saved-interim']);return;}
  if(args['deep-resume-probe']!==undefined){if(Object.keys(args).length!==1)throw new Error('Resume inspection cannot be mixed with scans');await (await import('./runtime/deep-resume.js')).runDeepProbe(args['deep-resume-probe']);return;}
  if(args['identity-operation']!==undefined){if(Object.keys(args).length!==1)throw new Error('Identity operation cannot be mixed with scans');await (await import('./runtime/sdk-identity.js')).runIdentityOperation(args['identity-operation']);return;}
 let task: ScanTask;
  if(args.job!==undefined) {
    const extra=Object.keys(args).filter(key=>!['job','mock','describe','identity','sdk-parameters','deep-resume-context','deep-resume'].includes(key));
    if(extra.length)throw new Error("Scanner job cannot be mixed with legacy scan flags");
    const {loadScannerJob,scanTaskFromJob}=await import("./contract/scanner-job.js");
    task=scanTaskFromJob(await loadScannerJob(args.job));
    if(flagEnabled(args.mock))task.mock=true;
  }else{
  const sourcePath = path.resolve(args.source ?? path.join("..", "test-fixtures"));
  const outputPath = path.resolve(args.output ?? path.join("m0-out", String(Date.now())));
  await mkdir(outputPath, { recursive: true, mode: 0o700 });

  const provider = (args.provider as ScanProvider | undefined) ?? "openai";
  const spec = resolveProvider(provider);
  task = {
    taskId: Number(args["task-id"] ?? 1),
    sourcePath,
    outputPath,
    provider,
    model: args.model ?? spec.defaultModel,
    scanMode: (args.mode as ScanMode | undefined) ?? "standard",
    timeoutMinutes: Number(args.timeout ?? 60),
    ...(args.workers !== undefined ? { workers: Number(args.workers) } : {}),
  };

  if (args["deep-max-runs"] !== undefined) task.deepMaxDiscoveryRuns = Number(args["deep-max-runs"]);
  if (args["deep-stop-no-new"] !== undefined) task.deepStopAfterNoNew = Number(args["deep-stop-no-new"]);
  if (process.env["AXIOM_VALIDATION_MODE"]) task.validationMode = process.env["AXIOM_VALIDATION_MODE"] as "default" | "exploitability";
  if (process.env["AXIOM_BUSINESS_CONTEXT"]) task.businessContext = process.env["AXIOM_BUSINESS_CONTEXT"];
  const baseUrl = args["base-url"] ?? spec.defaultBaseUrl;
  if (baseUrl) {
    task.baseUrl = baseUrl;
  }
  // Agent 透传的 provider 接入描述（注册表驱动，优先于内置 PROVIDERS）
  if (args["credential-env"]) {
    task.credentialEnv = args["credential-env"];
  }
  if (args["wire-api"] !== undefined) {
    if (args["wire-api"] !== "responses") throw new Error("Only Responses model connections are supported; Chat adaptation was removed.");
    task.wireApi = "responses";
  }
  if (args["display-name"]) {
    task.displayName = args["display-name"];
  }
  if (args.include) {
    task.includePaths = args.include
      .split(",")
      .map((s) => s.trim())
      .filter((s) => s.length > 0);
  }
  if (args.focus) {
    task.scanFocus = args.focus
      .split(",")
      .map((s) => s.trim())
      .filter((s) => s.length > 0);
  }
  if (args["max-cost"]) {
    task.maxCostUsd = Number(args["max-cost"]);
  }
  if (flagEnabled(args.mock)) {
    task.mock = true;
  }

  }
  if(args['sdk-parameters']!==undefined){if(args.job===undefined)throw new Error('SDK parameters require a versioned job');await (await import('./runtime/sdk-parameters.js')).loadSDKParameters(args['sdk-parameters'],task);}
 await (await import('./runtime/deep-resume.js')).loadDeepSidecars(args['deep-resume-context'],args['deep-resume'],task);
 if(args['identity']!==undefined){if(args.job===undefined)throw new Error('SDK identity requires a versioned job');await (await import('./runtime/sdk-identity.js')).loadTaskIdentity(args['identity'],task);}
 // Resolve and validate input before loading the SDK or creating its state.
  const {validateScanTask}=await import("./contract/validate-task.js");
  validateScanTask(task);
  const entryDirectory = process.cwd();
  const restoreWorkingDirectory = await enterSDKWorkingDirectory(task.deepResume?(await import('./runtime/deep-resume.js')).originOutput(task.outputPath,task.deepResume.inspection.origin.attempt):task.outputPath);
  const { CodexSecurityEngine, SCAN_EXIT_CODES } = await import("./engine/codex-security-engine.js");
  const engine = new CodexSecurityEngine();
  const controller = new AbortController();
  const timeoutMs = hardTimeoutMinutes(task) * 60_000;
  const timer = setTimeout(() => controller.abort(new Error("wall-clock timeout")), timeoutMs);

  const onSignal = (): void => controller.abort(new Error("canceled"));
  process.on("SIGINT", onSignal);
  process.on("SIGTERM", onSignal);

  try {
    const result = task.componentScan ? await (await import('./engine/component-scan.js')).runComponentAudit(task,controller.signal) : task.componentPlanning ? await (await import('./engine/component-planning.js')).runComponentPlanning(task,controller.signal) : task.sdkOperation ? await (await import('./engine/sdk-operation.js')).runSDKOperation(task,controller.signal) : await engine.scan(task, controller.signal);
    if(task.sdkIdentity&&!controller.signal.aborted)await (await import('./runtime/sdk-identity.js')).persistTaskIdentity(task);
    process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
    if (!task.mock && process.env["AXIOM_FIXTURE_SCORE"] === "1") {
      try {
        const fixtures = await loadFixtureManifest(defaultFixtureManifestPath(entryDirectory));
        const score = scoreFixtures(result.findings, fixtures);
        process.stderr.write(
          `fixture-score ${score.hits}/${score.total} ${score.results
            .map((item) => `${item.id}:${item.hit ? "HIT" : "MISS"}`)
            .join(" ")}\n`,
        );
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        process.stderr.write(`fixture-score skipped: ${message}\n`);
      }
    }
    process.exitCode = SCAN_EXIT_CODES[result.status];
  } finally {
    clearTimeout(timer);
    process.off("SIGINT", onSignal);
    process.off("SIGTERM", onSignal);
    restoreWorkingDirectory();
  }
}

const stopSupervision = superviseAgent();
// SDK cleanup can fail while a native process still holds Node handles. Keep
// supervision until Node actually exits; its unref timer never delays a clean exit.
process.once("exit", stopSupervision);
main().catch((error: unknown) => {
  const message = error instanceof Error ? error.message : String(error);
  process.stderr.write(`${message}\n`);
  process.exitCode = 10;
});
