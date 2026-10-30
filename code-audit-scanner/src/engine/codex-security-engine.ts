import {providerMcpOverrides} from '../runtime/provider-mcp.js';
import {tokenBudget,type TokenBudget} from '../runtime/token-budget.js';
import {bindSDKRuntime,sdkForTask,sdkRuntimeId} from '../runtime/sdk-runtime.js';
import {deepSeekAgentMessageCompatibility} from '../proxy/agent-messages.js';
import {sdkExecutionUsage} from '../runtime/execution-token-usage.js';
import {InterimFindings} from '../runtime/interim-findings.js';
import {prepareDeepResume,saveDeepManifest,acquireDeepGuard,runPinnedDeep,deltaUsage,originOutput} from '../runtime/deep-resume.js';
import {restoreSdkParentState,exportSdkParentState} from '../runtime/sdk-parent-state.js';
import {SDKSessionJournal} from '../runtime/sdk-session-journal.js';
import {SDKDiagnosticTracker} from '../runtime/sdk-diagnostics.js';
import {sdkScanOptions} from '../contract/sdk-scan-options.js';
import {sdkLifecycleCallbacks} from '../runtime/sdk-lifecycle.js';
import {ContractProgressSink} from '../runtime/contract-progress.js';
import {publishResult} from "../artifact/result-publisher.js";
import { prepareAuditWorkspace } from "../runtime/audit-bootstrap.js";
import { startReliableUpstream } from "../proxy/reliable-upstream.js";
import { observeSdkProgress } from "../runtime/sdk-progress-observer.js";
import {attachValidationReview} from '../artifact/validation-review.js';
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { CodexSecurity, VERSION as CODEX_SECURITY_SDK_VERSION } from "@openai/codex-security";
import type { JsonObject } from "@openai/codex-security";
import type { AuditEngine } from "./audit-engine.js";
import type { ScanTask } from "../contract/scan-task.js";
import type { ScanResult } from "../contract/scan-result.js";
import { SCAN_EXIT_CODES } from "../contract/scan-result.js";
import { SCANNER_LAYOUT } from "../contract/workspace.js";
import { validateScanTask, normalizeIncludePath } from "../contract/validate-task.js";
import { convertCodexResult } from "../converter/codex-finding-converter.js";
import { saveRawArtifacts } from "../artifact/artifact-manager.js";
import { loadRawCodexResult } from "../artifact/load-raw-result.js";
import { redactSecrets } from "../security/secret-redactor.js";
import { mapEngineError } from "./error-mapping.js";
import {
  buildCodexOverrides,
  requiredCredentialEnv,
  buildModelCatalog,
  resolveTaskProvider,
  nativeOpenAI,
  AXIOM_CATALOG_FILE,
} from "../provider/model-provider.js";
import type {UpstreamProxy} from "../proxy/upstream-http.js";
import { withIsolatedScanEnvironment } from "../runtime/scan-environment.js";
import {
  finishPhaseFromStatus,
  ScanProgressSink,
} from "../runtime/scan-progress.js";
import { formatActivityLine, summarizeSessionEvent } from "../runtime/session-summary.js";
import { releaseStaleWorkbenchScan } from "../runtime/workbench-release.js";

/**
 * Codex Security 适配器。
 *
 * Secret 约定（由调用方注入，本模块不读磁盘密钥文件）：
 * - openai:    OPENAI_API_KEY
 * - qwen:      DASHSCOPE_API_KEY（M0 未通过不得标 production）
 * - deepseek:  DEEPSEEK_API_KEY（M0 未通过不得标 production）
 */
export class CodexSecurityEngine implements AuditEngine {
  readonly name = "codex-security";

  async scan(task: ScanTask, signal?: AbortSignal): Promise<ScanResult> {
    await bindSDKRuntime(task);
    const continuation=await prepareDeepResume(task);
    const release=continuation?.unlock??(task.deepResumeContext&&task.scanMode==='deep'?await acquireDeepGuard(task.outputPath):()=>{});
    try{await saveDeepManifest(task);return await this.performScan(task,signal,continuation)}finally{release()}
  }
  private async performScan(task:ScanTask,signal:AbortSignal|undefined,continuation:Awaited<ReturnType<typeof prepareDeepResume>>):Promise<ScanResult>{
    validateScanTask(task);
    await restoreSdkParentState(task);
    assertCredentialPresent(task);

    const startedAt = new Date().toISOString();
    const rawDir = path.join(task.outputPath, SCANNER_LAYOUT.rawDir);
    const normalizedDir = path.join(task.outputPath, path.dirname(SCANNER_LAYOUT.normalizedResult));
    await mkdir(rawDir, { recursive: true, mode: 0o700 });
    await mkdir(normalizedDir, { recursive: true, mode: 0o700 });

    const sink = task.progressSchemaVersion===2 ? new ContractProgressSink(task.outputPath,{taskId:String(task.taskId),attempt:task.executionAttempt!}) : new ScanProgressSink(task.outputPath,task.taskId);
    const observe = (operation:Promise<void>) => { void operation.catch(() => { process.stderr.write('scan-warning progress persistence temporarily unavailable\n');if(sink instanceof ContractProgressSink)void sink.notice('扫描进度暂时写入失败，继续保留最近有效快照').catch(()=>undefined); }); };
    const finishProgress = async (result:ScanResult) => {
      try {if(sink instanceof ContractProgressSink)await sink.finish(result.status,result.error);else await sink.finish(finishPhaseFromStatus(result.status));}
      catch {process.stderr.write('scan-warning final progress persistence unavailable\n');if(sink instanceof ContractProgressSink)await sink.notice('扫描结果已生成；最终进度写入失败，执行结论以归档结果为准').catch(()=>undefined);}
    };

    await sink.init();

    let budget:TokenBudget|undefined;
    try{budget=tokenBudget(task)}catch(error){
      const result=await salvageOrFail(task,error,startedAt,rawDir);
      await writeNormalized(task,result);await finishProgress(result);return result;
    }

    const overrides = await providerMcpOverrides(task,buildCodexOverrides(task));
    // 第三方 provider：把模型目录写入隔离 CODEX_HOME，避免 models manager
    // 依赖远端 /models（DeepSeek 兼容问题的根因）。
    // 模型目录：绝对路径（该键在 Windows/0.149~0.152 只认绝对路径；相对基准在
    // 本 spawn 形态下 os error 2，内联 JSON 为路径语义 os error 123）。
    // Catalogs are private to this execution attempt.
    // （Windows %USERPROFILE%\.codex；Linux 容器 $HOME/.codex），跨机各自生成，无硬编码。
    if (overrides !== undefined) {
      const catalogPath = await ensureModelCatalog(task);
      if (catalogPath !== null) {
        (overrides as Record<string, unknown>)["model_catalog_json"] = catalogPath.replaceAll("\\", "/");
      }
    }
    // Route compatible providers through the Responses transport guard.
    const scanSignal=budget?AbortSignal.any([...(signal?[signal]:[]),budget.signal]):signal;
    const shim = await startResponsesTransport(task, overrides, (message) => { observe(sink.warning(message)); },undefined,'native_schema',false,budget);
    let security:CodexSecurity;
    try{security = new (await sdkForTask(task)).CodexSecurity(
      overrides !== undefined ? { codexOverrides: overrides } : {},
    );}catch(error){await shim?.close();throw error;}

    let parentExportScanId:string|undefined;
 let lastObservedUsage:Record<string,unknown>|undefined;
 const tokenExecution={taskId:String(task.taskId),attempt:task.executionAttempt??1};
    const diagnostics=task.resultSchemaVersion&&task.executionAttempt?new SDKDiagnosticTracker({taskId:String(task.taskId),attempt:task.executionAttempt},task.outputPath):null;
    const sessions=task.resultSchemaVersion&&task.executionAttempt?new SDKSessionJournal({taskId:String(task.taskId),attempt:task.executionAttempt},task.outputPath):null;
    try {
      return await withIsolatedScanEnvironment(task, async () => {
        // Reuse the original workspace paths without changing the new
        // execution identity or attempting to overwrite the origin's receipt.
        const workspace = await prepareAuditWorkspace(task,task.deepResume?originOutput(task.outputPath,task.deepResume.inspection.origin.attempt):task.outputPath);
        const stopProgressObserver = observeSdkProgress(task.deepResume?originOutput(task.outputPath,task.deepResume.inspection.origin.attempt):task.outputPath,p=>sink instanceof ContractProgressSink?sink.observation(p):sink.progress({phase:p.phase,...(p.filesTotal!==null?{filesTotal:p.filesTotal}:{})}),message=>observe(sink.warning(message)),process.env.CODEX_SECURITY_STATE_DIR);
        let interim:InterimFindings|null=null;
        if(task.scanMode==='standard'&&task.sourceContentHash&&task.executionAttempt)try{interim=new InterimFindings(task.outputPath,{taskId:String(task.taskId),attempt:task.executionAttempt},task.sourceContentHash,value=>{if(sink instanceof ContractProgressSink)observe(sink.interim(value));},message=>observe(sink.warning(message))).start();}catch{observe(sink.warning('中间证据目录不可写或身份不匹配；正式扫描继续，原始产物保留。'));}
        try {
          // 取消是杀进程，workbench 来不及 fail-scan；同一 outputDir 重试会被
          // 「Cannot archive the output of a running scan」挡住。
          const released = continuation?0:releaseStaleWorkbenchScan(rawDir);
          if (released > 0) {
            process.stderr.write(`scan-activity released ${released} stale workbench scan(s)\n`);
          }
          const sdkResult = await runPinnedDeep(security,task.sourcePath, {
            ...await sdkScanOptions(task,scanSignal,workspace.prompt),
            outputDir: rawDir,
            archiveExisting: true,
            ...sdkLifecycleCallbacks((state,message,warning)=>{
              observe(sink instanceof ContractProgressSink ? sink.lifecycle(state,message,warning) : warning ? sink.warning(message) : sink.activity(message));
            }),
            onProgress: (progress) => {
              observe(sink.progress(progress));
              process.stderr.write(
                `scan-progress phase=${progress.phase} files=${progress.filesCompleted}/${progress.filesTotal}\n`,
              );
            },
            onActivity: (activity) => {
              if (activity.kind === "reasoning") return; // session event is the single reasoning source
              const line = formatActivityLine(activity);
              observe(sink instanceof ContractProgressSink ? sink.activity({id:activity.id,kind:activity.kind,status:activity.status,description:activity.description,paths:activity.paths,...(activity.worker!==undefined?{worker:{index:activity.worker}}:{})}) : sink.activity(line));
              process.stderr.write(`scan-activity ${line}\n`);
            },
            onDeepProgress: (progress) => { observe(sink.deepProgress(progress)); },
            onReconnect: (attempt, maximum, details) => { observe(sink.reconnect(attempt, maximum, details)); },
            onObserverError: (name,error) => {
              if(diagnostics){const entry=diagnostics.observer(name,error);observe(sink instanceof ContractProgressSink?sink.sdkDiagnostic(diagnostics.snapshot(),entry):sink.warning(entry.message));}
              else observe(sink.warning(`进度观察器 ${name} 出错，部分状态可能暂不可用`));
            },
            onWorkerStatus: (status) => {
              observe(sink.workerStatus(status));
              process.stderr.write(`scan-worker ${JSON.stringify(status)}\n`);
            },
            onWorkerEvent: (event) => {
              if(sink instanceof ContractProgressSink)observe(sink.workerObserved(event));
              else observe(sink.activity(`SDK 已记录 Worker ${event.worker}；运行状态尚未提供`));
            },
            onUsage: (usage) => {
 lastObservedUsage=task.deepResume?deltaUsage({...usage},task.deepResume.inspection.baseline):{...usage};
              if(sink instanceof ContractProgressSink)observe(sink.usage(lastObservedUsage));
              if(budget&&!shim)budget.observe(sdkExecutionUsage(tokenExecution,lastObservedUsage,true));
            },
            onWarning: (warning,details) => {
              if(diagnostics){const entry=diagnostics.warning(warning,details);observe(sink instanceof ContractProgressSink?sink.sdkDiagnostic(diagnostics.snapshot(),entry):sink.warning(entry.message));process.stderr.write(`scan-warning ${entry.message}\n`);}
              else observe(sink.warning(warning));
            },
            onSessionEvent: (event) => {
              sessions?.record(event);
              workspace.ensureTemp();
              interim?.session(event);
              if(sink instanceof ContractProgressSink){observe(sink.session(event));return;}
              const detail = summarizeSessionEvent(event);
              if (detail === null || (!showReasoning() && !detail.startsWith("[error]"))) return;
              process.stderr.write(`scan-session ${event.threadId} ${detail}\n`);
              // 推理同时更新「当前执行」；助手正文已由 onActivity 覆盖，不再双写。
              if (detail.startsWith("[reasoning]")) {
                observe(sink.activity(detail));
                return;
              }
              observe(sink.session(event.threadId, event.worker, detail));
            },
          },continuation?.options);
          await continuation?.copyArtifacts();

          const sdkSnapshot=await saveRawArtifacts(rawDir, sdkResult);
          if(budget&&!shim)budget.observe(sdkExecutionUsage(tokenExecution,lastObservedUsage,true));
          const converted = convertCodexResult(task, sdkResult, {
            startedAt,
            finishedAt: new Date().toISOString(),
            rawArtifactDir: SCANNER_LAYOUT.rawDir,
            normalizedResultPath: SCANNER_LAYOUT.normalizedResult,
            ...(sdkSnapshot?{sdkSnapshot}:{}),
          });
          // A complete SDK result may finish on the turn that reached the
          // limit. Keep it after validation; interrupted/unknown runs stay partial.
          if(budget?.error){
            if(budget.error.code!=='budget_exceeded'||converted.status!=='completed'||converted.error)throw budget.error;
            observe(sink.warning(budget.error.message));
          }
          if(["completed","partial"].includes(converted.status))parentExportScanId=sdkResult.manifest.scan.id;
          // SDK prices are not a platform estimate. Keep native evidence in the
          // raw archive; configured prices and executionUsage own accounting.
          delete converted.cost;
 converted.executionUsage=shim?.tokenUsage?.(tokenExecution,converted.status==='completed'&&!converted.error)??sdkExecutionUsage(tokenExecution,lastObservedUsage,converted.status==='completed'&&!converted.error);
          if(diagnostics)converted.sdkDiagnostics=diagnostics.snapshot();
          if(sessions)converted.sdkSession=sessions.close();
          await attachValidationReview(task,converted,rawDir);
          interim?.scan();
          if(sink instanceof ContractProgressSink)await sink.drain();
          await diagnostics?.drain();
          await writeNormalized(task, converted);
          await finishProgress(converted);
          return converted;
        } catch (error) {
          try{await continuation?.copyArtifacts()}catch{observe(sink.warning("续扫原生产物复制未完成，原目录和预执行备份仍保留"))}
          const mapped=mapEngineError(error);
          const budgetInterruption=budget?.error&&(error===budget.error||mapped.status==='canceled'||/abort|cancel|budget_/.test(error instanceof Error?error.message:String(error)));
          const cause=signal?.aborted?signal.reason:mapped.code==='result_corrupt'?error:budgetInterruption?budget!.error:error;
          const result = await salvageOrFail(task, cause, startedAt, rawDir);
 result.executionUsage=shim?.tokenUsage?.(tokenExecution,false)??sdkExecutionUsage(tokenExecution,lastObservedUsage,false);
          if(diagnostics)result.sdkDiagnostics=diagnostics.snapshot();
          if(sessions)result.sdkSession=sessions.close();
          // Interrupted/recovered artifacts and callbacks are partial observations.
          // The final heartbeat retains their cost; the portable result only carries
          // a final SDK-returned estimate. Keep the historical reader unchanged.
          if(task.resultSchemaVersion)delete result.cost;
          interim?.scan();
          if(sink instanceof ContractProgressSink)await sink.drain();
          await diagnostics?.drain();
          await writeNormalized(task, result);
          await finishProgress(result);
          return result;
        } finally {
          interim?.close();
          stopProgressObserver();
          workspace.restore();
        }
      });
    } finally {
      sessions?.close();
      await shim?.close();
      await security.close();
      if(parentExportScanId)await exportSdkParentState(task,parentExportScanId).catch(()=>{process.stderr.write("scan-warning parent SDK state snapshot unavailable; scan result retained\n");});
    }
  }
}

/**
 * Keep the SDK on Responses; this guard does not translate protocols.
 */
async function startResponsesTransport(task: ScanTask, overrides: JsonObject | undefined, notice: (message:string)=>void, observeTurn?:((value:import('../proxy/turn-diagnostic.js').TurnDiagnostic)=>void), outputMode:import('../proxy/validation-output-mode.js').ValidationOutputMode='native_schema',frozenReview=false,budget:TokenBudget|undefined=tokenBudget(task)): Promise<UpstreamProxy | null> {
  if (overrides === undefined || overrides["model_providers"]===undefined) return null;
  const spec = resolveTaskProvider(task);
  const wire = task.wireApi ?? spec.wireApi;
  if (wire !== "responses") throw new Error("Chat 协议适配已移除；请配置支持 Responses 的模型接口。");

  const upstream = task.baseUrl ?? spec.defaultBaseUrl;
  if (upstream === undefined || upstream === "") {
    throw new Error(`Responses transport: ScanTask.baseUrl is required for provider ${task.provider}`);
  }
  const deepSeek=deepSeekAgentMessageCompatibility(task.provider,upstream,sdkRuntimeId(task));
  const transport = await startReliableUpstream(upstream, path.resolve(task.outputPath, '../../model-request-gate.sqlite'), notice,observeTurn,outputMode,frozenReview,budget,{agentMessages:deepSeek,nestedAnyOf:deepSeek});
  const shim: UpstreamProxy = transport;
  // codex(reqwest) 会吃 HTTPS_PROXY；环回地址必须进 NO_PROXY，否则代理把
  // 127.0.0.1 请求转发走，shim 永远收不到
  for (const key of ["NO_PROXY", "no_proxy"]) {
    const cur = process.env[key] ?? "";
    if (!cur.includes("127.0.0.1")) {
      process.env[key] = cur ? `${cur},127.0.0.1,localhost` : "127.0.0.1,localhost";
    }
  }
  const providers = overrides["model_providers"] as Record<string, Record<string, unknown>>;
  const target = providers[task.provider];
  if (target === undefined) {
    await shim.close();
    return null; // 不会发生：buildCodexOverrides 已创建该 provider 条目
  }
  // Keep the pinned native SDK retry policy. The transport proxy coordinates
  // provider cooldowns but never runs a second, multiplying retry loop.
  target["base_url"] = shim.baseUrl;
  target["wire_api"] = "responses";
  process.stderr.write(`scan-activity responses-transport ${shim.baseUrl} -> ${shim.upstreamBaseUrl}\n`);
  return shim;
}

async function salvageOrFail(
  task: ScanTask,
  error: unknown,
  startedAt: string,
  rawDir: string,
): Promise<ScanResult> {
  const mapped = mapEngineError(error);
  const recovered = await loadRawCodexResult(rawDir);
  if (recovered) {
    const converted = convertCodexResult(task, recovered, {
      startedAt,
      finishedAt: new Date().toISOString(),
      rawArtifactDir: SCANNER_LAYOUT.rawDir,
      normalizedResultPath: SCANNER_LAYOUT.normalizedResult,
    });
    converted.status = converted.findings.length > 0 ? "partial" : mapped.status;
    converted.error = {
      code: mapped.code,
      message: redactSecrets(mapped.message),
    };
    await attachValidationReview(task,converted,rawDir);
    return converted;
  }

  const failed: ScanResult = {
    taskId: task.taskId,
    status: mapped.status,
    startedAt,
    finishedAt: new Date().toISOString(),
    engine: {
      name: "codex-security",
      version: CODEX_SECURITY_SDK_VERSION,
    },
    model: {
      provider: task.provider,
      model: task.model,
      ...(task.baseUrl !== undefined ? { baseUrl: task.baseUrl } : {}),
    },
    findings: [],
    rawArtifactDir: "raw/codex-security",
    normalizedResultPath: "normalized/result.json",
    error: {
      code: mapped.code,
      message: redactSecrets(mapped.message),
    },
  };
  await attachValidationReview(task,failed,rawDir);
  return failed;
}

async function ensureModelCatalog(task: ScanTask): Promise<string | null> {
  if (task.mock || nativeOpenAI(task)) return null;
  const json = `${JSON.stringify(buildModelCatalog(task), null, 2)}\n`;
  // Each attempt owns its catalog; concurrent providers cannot overwrite one another.
  const catalogDir = path.join(task.outputPath, "runtime");
  await mkdir(catalogDir, { recursive: true, mode: 0o700 });
  const canonical = path.join(catalogDir, AXIOM_CATALOG_FILE);
  await writeFile(canonical, json, { encoding: "utf8", mode: 0o600 });
  return canonical;
}

function showReasoning(): boolean {
  return process.env["AXIOM_SHOW_REASONING"] !== "0";
}

function assertCredentialPresent(task: ScanTask): void {
  if (task.mock || task.sdkIdentity?.identity.kind==='chatgpt') return;
  const envName = task.credentialEnv ?? requiredCredentialEnv(task.provider);
  if (!process.env[envName]) {
    throw new Error(`Missing ${envName} for provider ${task.provider}`);
  }
}

async function writeNormalized(task: ScanTask, result: ScanResult): Promise<void> {
  if(task.resultSchemaVersion){await publishResult(task,result);return;}
  const file = path.join(task.outputPath, result.normalizedResultPath);
  await mkdir(path.dirname(file), { recursive: true, mode: 0o700 });
  await writeFile(file, `${JSON.stringify(result, null, 2)}\n`, "utf8");
}

export { SCAN_EXIT_CODES };

export {startResponsesTransport,ensureModelCatalog};
