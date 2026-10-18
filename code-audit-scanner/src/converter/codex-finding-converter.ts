import {InvalidSDKResultError} from "../artifact/result-publisher.js";
import { createHash } from "node:crypto";
import { VERSION as CODEX_SECURITY_SDK_VERSION } from "@openai/codex-security";
import type { ScanTask } from "../contract/scan-task.js";
import type { CodeEvidenceItem, Finding, FindingSeverity } from "../contract/finding.js";
import type { ScanResult } from "../contract/scan-result.js";

/**
 * 把 Codex Security SDK 的 ScanResult 转成业务层 ScanResult。
 *
 * 映射常用字段为具名业务字段，同时把上游 finding 对象原样透传
 * （finding.raw = 完整上游对象），保证"产出的全部内容"不丢。Raw 文件仍由
 * artifact-manager 另存。
 */
export function convertCodexResult(
  task: ScanTask,
  sdkResult: CodexLikeScanResult,
  meta: {
    startedAt: string;
    finishedAt: string;
    rawArtifactDir: string;
    normalizedResultPath: string;
    sdkSnapshot?: Record<string,unknown>;
  },
): ScanResult {
  const input = sdkResult.findings?.findings;
  if (!Array.isArray(input)) throw new InvalidSDKResultError("SDK result has no findings array");
  if(task.resultSchemaVersion && input.some(item=>! ["critical","high","medium","low","informational","info"].includes(item.severity?.level??"")))throw new InvalidSDKResultError("SDK result contains an unsupported finding severity");
  const findings = input.map((item) => toFinding(item));
  const completeness = mapCompleteness(sdkResult.coverage?.completeness);
  const sdkRaw=task.resultSchemaVersion?(meta.sdkSnapshot??sdkResult.toJSON?.()):undefined;

  const result: ScanResult = {
    taskId: task.taskId,
    status: mapStatus(sdkResult.manifest?.scan?.status, findings.length),
    startedAt: task.resultSchemaVersion ? meta.startedAt : sdkResult.manifest?.scan?.startedAt ?? meta.startedAt,
    finishedAt: task.resultSchemaVersion ? meta.finishedAt : sdkResult.manifest?.scan?.completedAt ?? meta.finishedAt,
    engine: {
      name: "codex-security",
      version: CODEX_SECURITY_SDK_VERSION,
      ...(sdkResult.pluginVersion !== undefined
        ? { pluginVersion: sdkResult.pluginVersion }
        : {}),
    },
    model: {
      provider: task.provider,
      model: task.model,
      ...(task.baseUrl !== undefined ? { baseUrl: task.baseUrl } : {}),
    },
    findings,
    ...(sdkRaw?{sdkRaw,sdkRawSource:(sdkRaw.recoveredArtifacts===true?'recovered_artifacts':'sdk_to_json') as 'recovered_artifacts'|'sdk_to_json'}:{}),
    rawArtifactDir: meta.rawArtifactDir,
    normalizedResultPath: meta.normalizedResultPath,
  };
  if (completeness !== undefined) {
    result.coverage = { ...sdkResult.coverage, completeness };
    if (completeness === "partial" && result.status === "completed") result.status = "partial";
  }
  const cost = mapCost(sdkResult.cost);
  if (cost !== undefined) {
    result.cost = cost;
  }
  return result;
}

function toFinding(item: CodexLikeFinding): Finding {
  const sink = pickSink(item.locations ?? []);
  const fingerprint =
    item.fingerprints?.primary ??
    fallbackFingerprint({
      ...(sink?.path !== undefined ? { filePath: sink.path } : {}),
      ...(item.ruleId !== undefined ? { ruleId: item.ruleId } : {}),
      ...(item.taxonomy?.cwe !== undefined ? { cwe: item.taxonomy.cwe } : {}),
      ...(sink?.startLine !== undefined ? { startLine: sink.startLine } : {}),
    });

  const finding: Finding = {
    fingerprint,
    title: item.title,
    severity: mapSeverity(item.severity?.level),
    description: item.summary,
    // 上游完整对象原样透传（未裁剪）
    raw: item,
  };

  if (item.taxonomy?.category) finding.category = item.taxonomy.category;
  if (item.taxonomy?.cwe?.length) finding.cwe = item.taxonomy.cwe;
  if (sink?.path) finding.filePath = sink.path;
  if (sink?.startLine !== undefined) finding.startLine = sink.startLine;
  if (sink?.endLine !== undefined) finding.endLine = sink.endLine;
  if (item.remediation) finding.remediation = item.remediation;
  if (item.confidence?.level) finding.confidence = mapConfidence(item.confidence.level);
  if (item.confidence?.rationale) finding.confidenceRationale = item.confidence.rationale;
  if (item.severity?.score !== undefined) finding.severityScore = item.severity.score;
  if (item.severity?.scoringSystem) finding.scoringSystem = item.severity.scoringSystem;
  if (item.ruleId) finding.ruleId = item.ruleId;
  if (typeof item.rootCause === "string") finding.rootCause = item.rootCause;
  else if (item.rootCause?.summary) finding.rootCause = item.rootCause.summary;
  if (item.findingId) finding.engineFindingId = item.findingId;
  if (item.occurrenceId) finding.engineOccurrenceId = item.occurrenceId;

  // attackPath：对象或字符串都收
  if (typeof item.attackPath === "string") {
    finding.attackPath = item.attackPath;
  } else if (item.attackPath) {
    if (item.attackPath.summary) finding.attackPath = item.attackPath.summary;
    if (item.attackPath.entrypoint) finding.entrypoint = item.attackPath.entrypoint;
    if (item.attackPath.source) finding.source = item.attackPath.source;
    if (item.attackPath.sink) finding.sink = item.attackPath.sink;
    const reach = item.attackPath.reachability;
    if (reach) {
      finding.reachability = typeof reach === "string" ? reach : reach.summary ?? "";
    }
    const dataflow = item.attackPath.dataFlow ?? item.attackPath.dataflow ?? item.attackPath.data_flow ?? item.attackPath.dataflowText;
    if (dataflow) {
      finding.dataflow =
        typeof dataflow === "string" ? dataflow : (dataflow.summary ?? JSON.stringify(dataflow));
    }
    const impact = item.attackPath.impact;
    if (impact) {
      finding.impact = typeof impact === "string" ? impact : (impact.rationale ?? impact.why ?? "有影响");
    }
    const likelihood = item.attackPath.likelihood;
    if (likelihood) {
      finding.likelihood =
        typeof likelihood === "string" ? likelihood : (likelihood.level ?? likelihood.rationale ?? "未知");
    }
    if (Array.isArray(item.attackPath.steps) && item.attackPath.steps.length) {
      finding.attackSteps = item.attackPath.steps;
    }
    if (Array.isArray(item.attackPath.preconditions) && item.attackPath.preconditions.length) {
      finding.preconditions = item.attackPath.preconditions;
    }
  }

  // codeEvidence：全部证据条目（代码 + 解释）
  if (item.codeEvidence?.length) {
    finding.codeEvidence = item.codeEvidence.map((e) => ({
      code: e.code,
      ...(e.id !== undefined ? { id: e.id } : {}),
      ...(e.label !== undefined ? { label: e.label } : {}),
      ...(e.path !== undefined ? { path: e.path } : {}),
      ...(e.startLine !== undefined ? { startLine: e.startLine } : {}),
      ...(e.endLine !== undefined ? { endLine: e.endLine } : {}),
      ...(e.language !== undefined ? { language: e.language } : {}),
      ...(e.role !== undefined ? { role: e.role } : {}),
      ...(e.explanation !== undefined ? { explanation: e.explanation } : {}),
    })) satisfies CodeEvidenceItem[];
  }
  if (item.validation !== undefined) finding.validation = item.validation;
  if (item.remediationTests?.length) finding.remediationTests = item.remediationTests;
  if (item.preventiveControls?.length) finding.preventiveControls = item.preventiveControls;
  if (item.provenance !== undefined) finding.provenance = item.provenance;
  if (item.extensions !== undefined) finding.extensions = item.extensions;

  const evidence = firstEvidence(item);
  if (evidence) finding.evidence = evidence;

  return finding;
}

function pickSink(
  locations: Array<{ path: string; startLine: number; endLine?: number; role?: string }>,
): { path: string; startLine: number; endLine?: number } | undefined {
  const sink = locations.find((loc) => loc.role === "sink") ?? locations[0];
  if (!sink) return undefined;
  return {
    path: sink.path,
    startLine: sink.startLine,
    ...(sink.endLine !== undefined ? { endLine: sink.endLine } : {}),
  };
}

function firstEvidence(item: CodexLikeFinding): string | undefined {
  const snippet = item.codeEvidence?.[0];
  if (snippet?.code) {
    return `${snippet.path}:${snippet.startLine}\n${snippet.code}`;
  }
  if (typeof item.rootCause === "string") return item.rootCause;
  if (item.rootCause && typeof item.rootCause === "object" && item.rootCause.summary) {
    return item.rootCause.summary;
  }
  return undefined;
}

export function fallbackFingerprint(input: {
  filePath?: string;
  ruleId?: string;
  cwe?: string[];
  startLine?: number;
}): string {
  const pathPart = normalizePath(input.filePath ?? "");
  const rulePart = (input.ruleId ?? input.cwe?.[0] ?? "unknown").toLowerCase();
  const linePart = String(input.startLine ?? 0);
  const material = `${pathPart}|${rulePart}|${linePart}`;
  const digest = createHash("sha256").update(material).digest("hex");
  return `axiom/v1:sha256:${digest}`;
}

function normalizePath(filePath: string): string {
  return filePath.replaceAll("\\", "/").replace(/^\.\//, "").toLowerCase();
}

function mapSeverity(level: string | undefined): FindingSeverity {
  switch (level) {
    case "critical":
      return "critical";
    case "high":
      return "high";
    case "medium":
      return "medium";
    case "low":
      return "low";
    case "info":
    case "informational":
      return "info";
    default:
      return "info";
  }
}

function mapConfidence(level: string): number {
  switch (level) {
    case "high":
      return 0.9;
    case "medium":
      return 0.6;
    case "low":
      return 0.3;
    default:
      return 0.5;
  }
}

function mapCompleteness(
  value: string | undefined,
): "complete" | "partial" | "unknown" | undefined {
  if (value === "complete" || value === "partial" || value === "unknown") return value;
  return undefined;
}

function mapCost(
  cost: CodexLikeScanResult["cost"],
): ScanResult["cost"] | undefined {
  if (!cost) return undefined;
  const mapped: NonNullable<ScanResult["cost"]> = {};
  if (typeof cost.estimatedUsd === "number") mapped.estimatedUsd = cost.estimatedUsd;
  if (typeof cost.inputTokens === "number") mapped.inputTokens = cost.inputTokens;
  if (typeof cost.outputTokens === "number") mapped.outputTokens = cost.outputTokens;
  return Object.keys(mapped).length > 0 ? mapped : undefined;
}

function mapStatus(
  upstream: string | undefined,
  findingCount: number,
): ScanResult["status"] {
  if (upstream === "canceled" || upstream === "interrupted") return "canceled";
  if (upstream === "failed") return "failed";
  if (upstream === "completed") return "completed";
  return findingCount > 0 ? "partial" : "failed";
}

/** 最小上游形状，避免把 SDK 类型泄漏进业务层。 */
export interface CodexLikeScanResult {
  toJSON?: ()=>Record<string,unknown>;
  pluginVersion?: string;
  manifest?: {
    scan?: {
      status?: string;
      startedAt?: string;
      completedAt?: string;
    };
  };
  findings?: { findings?: CodexLikeFinding[] };
  coverage?: { completeness?: string; [key: string]: unknown };
  cost?: {
    estimatedUsd?: number;
    inputTokens?: number;
    outputTokens?: number;
  } | null;
}

export interface CodexLikeFinding {
  findingId?: string;
  occurrenceId?: string;
  ruleId?: string;
  fingerprints?: { primary?: string };
  title: string;
  summary: string;
  severity?: { level?: string; score?: number; scoringSystem?: string };
  confidence?: { level?: string; rationale?: string };
  taxonomy?: { category?: string; cwe?: string[] };
  locations?: Array<{ path: string; startLine: number; endLine?: number; role?: string }>;
  remediation?: string;
  rootCause?: { summary?: string } | string | null;
  attackPath?:
    | string
    | {
        summary?: string;
        entrypoint?: string;
        source?: string;
        dataflow?: string | { summary?: string } | null;
        dataFlow?: string | { summary?: string } | null;
        data_flow?: string | { summary?: string } | null;
        dataflowText?: string | null;
        sink?: string | null;
        reachability?: string | { summary?: string } | null;
        impact?: string | { level?: string; rationale?: string; why?: string } | null;
        likelihood?: string | { level?: string; rationale?: string } | null;
        steps?: string[];
        preconditions?: string[];
        [k: string]: unknown;
      }
    | null;
  codeEvidence?: Array<{
    id?: string;
    label?: string;
    path: string;
    startLine: number;
    endLine?: number;
    language?: string;
    role?: string;
    code: string;
    explanation?: string;
  }>;
  validation?: unknown;
  remediationTests?: string[];
  preventiveControls?: string[];
  provenance?: unknown;
  extensions?: unknown;
}
