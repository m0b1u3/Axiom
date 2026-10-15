import {ContractProgressSink} from '../runtime/contract-progress.js';
import {publishResult} from "../artifact/result-publisher.js";
import { AuditCheckpoints } from "../runtime/audit-checkpoints.js";
import { prepareAuditWorkspace } from "../runtime/audit-bootstrap.js";
import { startReliableUpstream } from "../proxy/reliable-upstream.js";
import { observeSdkProgress } from "../runtime/sdk-progress-observer.js";
import {exploitabilityOptions} from '../contract/exploitability.js';
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
import { startChatShim, type ChatShim } from "../proxy/chat-shim.js";
import { withIsolatedScanEnvironment } from "../runtime/scan-environment.js";
import {
  finishPhaseFromStatus,
  ScanProgressSink,
} from "../runtime/scan-progress.js";
import { formatActivityLine, summarizeSessionEvent } from "../runtime/session-summary.js";
import { buildScanPrompt } from "../contract/scan-prompt.js";
import { deepScanOptions } from "../contract/deep-policy.js";
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
    validateScanTask(task);
    const deepOptions = deepScanOptions(task);
    const validationOptions = exploitabilityOptions(task);
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

    const overrides = buildCodexOverrides(task);
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
    // Chat 上游（如 Kimi/GLM paas）：codex 0.149 只讲 Responses（二进制实证
    // 0 处 chat/completions），起环回代理做转换（cc-switch 路由接管同构）。
    // 必须先于 new CodexSecurity：插件构造函数 structuredClone(config)，
    // 之后对 overrides 的任何改写都不会进入 config.toml（scan 17/18 的教训）。
    const shim = await maybeStartChatShim(task, overrides, (message) => { observe(sink.warning(message)); });
    const security = new CodexSecurity(
      overrides !== undefined ? { codexOverrides: overrides } : {},
    );

    let lastObservedCost: Record<string,unknown>|undefined;
    try {
      return await withIsolatedScanEnvironment(task, async () => {
        const stopProgressObserver = observeSdkProgress(task.outputPath,p=>sink instanceof ContractProgressSink?sink.observation(p):sink.progress({phase:p.phase,...(p.filesTotal!==null?{filesTotal:p.filesTotal}:{})}),message=>observe(sink.warning(message)));
        const checkpoints = new AuditCheckpoints(task.outputPath, message => { observe(sink.warning(message)); });
        const workspace = await prepareAuditWorkspace(task);
        try {
          // 取消是杀进程，workbench 来不及 fail-scan；同一 outputDir 重试会被
          // 「Cannot archive the output of a running scan」挡住。
          const released = releaseStaleWorkbenchScan(rawDir);
          if (released > 0) {
            process.stderr.write(`scan-activity released ${released} stale workbench scan(s)\n`);
          }
          const sdkResult = await security.run(task.sourcePath, {
            outputDir: rawDir,
            // 同一 taskId 重试时 raw/ 里还留着上次取消的产物；SDK 默认拒绝覆盖。
            // archiveExisting 把旧结果挪到归档目录再开新扫（对应 CLI --archive-existing）。
            archiveExisting: true,
            scanPrompt: [buildScanPrompt(task.scanFocus),workspace.prompt,validationOptions.scanPrompt].filter(Boolean).join("\n\n"),
            ...(validationOptions.validationPrompt ? {validationPrompt:validationOptions.validationPrompt}:{}),
            mode: task.scanMode,
            ...deepOptions,
            // 路径子集扫描：白名单透传 SDK ScanTarget（readonly string[] 形态）。
            ...(task.includePaths !== undefined && task.includePaths.length > 0
              ? { target: task.includePaths.map(normalizeIncludePath) }
              : {}),
            ...(task.mock
              ? { mock: true }
              : nativeOpenAI(task)
                ? { auth: "api-key" as const }
                : {}),
            ...(task.maxCostUsd !== undefined ? { maxCostUsd: task.maxCostUsd } : {}),
            ...(task.scanMode === "deep" && task.workers !== undefined ? { workers: task.workers } : {}),
            ...(signal !== undefined ? { signal } : {}),
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
            onObserverError: (name) => { observe(sink.warning(`进度观察器 ${name} 出错，部分状态可能暂不可用`)); },
            onWorkerStatus: (status) => {
              observe(sink.workerStatus(status));
              process.stderr.write(`scan-worker ${JSON.stringify(status)}\n`);
            },
            onCost: (cost) => {
              lastObservedCost={...cost,observedAt:new Date().toISOString(),source:"engine_callback"};
              observe(sink instanceof ContractProgressSink ? sink.cost({...cost}) : sink.cost(cost.estimatedUsd));
              process.stderr.write(
                `scan-cost usd=${cost.estimatedUsd} in=${cost.inputTokens} out=${cost.outputTokens}\n`,
              );
            },
            onWarning: (warning) => {
              observe(sink.warning(warning));
              process.stderr.write(`scan-warning ${warning}\n`);
            },
            onSessionEvent: (event) => {
              workspace.ensureTemp();
              checkpoints.session(event);
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
          });

          const sdkSnapshot=await saveRawArtifacts(rawDir, sdkResult);
          const converted = convertCodexResult(task, sdkResult, {
            startedAt,
            finishedAt: new Date().toISOString(),
            rawArtifactDir: SCANNER_LAYOUT.rawDir,
            normalizedResultPath: SCANNER_LAYOUT.normalizedResult,
            ...(sdkSnapshot?{sdkSnapshot}:{}),
          });
          if(lastObservedCost)converted.lastObservedCost=lastObservedCost;
          await attachValidationReview(task,converted,rawDir);
          checkpoints.attach(converted);
          await writeNormalized(task, converted);
          await finishProgress(converted);
          return converted;
        } catch (error) {
          const result = await salvageOrFail(task, error, startedAt, rawDir);
          if(lastObservedCost){result.lastObservedCost=lastObservedCost;if(!task.resultSchemaVersion && !result.cost)result.cost={estimatedUsd:lastObservedCost.estimatedUsd as number,inputTokens:lastObservedCost.inputTokens as number,outputTokens:lastObservedCost.outputTokens as number};}
          // Interrupted/recovered artifacts and callbacks are partial observations.
          // The final heartbeat retains their cost; the portable result only carries
          // a final SDK-returned estimate. Keep the historical reader unchanged.
          if(task.resultSchemaVersion)delete result.cost;
          checkpoints.attach(result);
          await writeNormalized(task, result);
          await finishProgress(result);
          return result;
        } finally {
          stopProgressObserver();
          checkpoints.close();
          workspace.restore();
          await shim?.close();
        }
      });
    } finally {
      await security.close();
    }
  }
}

/**
 * 有效上游协议为 chat 时启动 Responses→Chat 环回代理，并把 Codex provider
 * 指到代理（wire_api 对 Codex 而言始终是 responses；真实协议只有代理知道）。
 */
async function maybeStartChatShim(task: ScanTask, overrides: JsonObject | undefined, notice: (message:string)=>void): Promise<ChatShim | null> {
  if (overrides === undefined || overrides["model_providers"]===undefined) return null;
  const spec = resolveTaskProvider(task);
  const wire = task.wireApi ?? spec.wireApi;

  const upstream = task.baseUrl ?? spec.defaultBaseUrl;
  if (upstream === undefined || upstream === "") {
    throw new Error(`chat-shim: ScanTask.baseUrl is required for provider ${task.provider}`);
  }
  const transport = await startReliableUpstream(upstream, path.resolve(task.outputPath, '../../model-request-gate.sqlite'), notice);
  const converted = wire === "chat" ? await startChatShim(transport.baseUrl) : null;
  const shim: ChatShim = {baseUrl:converted?.baseUrl??transport.baseUrl,upstreamBaseUrl:upstream,
    close:async()=>{await converted?.close();await transport.close();}};
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
  target["request_max_retries"] = 0; // One retry owner: reliable-upstream.
  target["stream_max_retries"] = 0; // Never replay a partly consumed turn automatically.
  target["base_url"] = shim.baseUrl;
  target["wire_api"] = "responses";
  process.stderr.write(`scan-activity chat-shim ${shim.baseUrl} -> ${shim.upstreamBaseUrl}\n`);
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
  if (task.mock) return;
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
