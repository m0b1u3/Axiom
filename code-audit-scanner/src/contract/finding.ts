/**
 * 业务层 Finding。与 Codex Security 的 findings.json 解耦，但保留其全部字段。
 *
 * `raw` 为上游 finding 对象原样透传（JSON 任意结构）。
 * 界面展示"Codex Security 产出的全部内容"时，优先从 raw 读取；
 * 以下具名字段是常用字段的可靠映射，供筛选/排序/摘要使用。
 */
export type FindingSeverity = "critical" | "high" | "medium" | "low" | "info";

export interface CodeEvidenceItem {
  id?: string;
  label?: string;
  path?: string;
  startLine?: number;
  endLine?: number;
  language?: string;
  role?: string;
  code: string;
  explanation?: string;
}

export interface Finding {
  /** 稳定指纹，与 scan_id / taskId 无关 */
  fingerprint: string;

  title: string;

  severity: FindingSeverity;

  /** 上游 CVSS 评分与体系 */
  severityScore?: number;
  scoringSystem?: string;

  confidence?: number;
  /** 上游 confidence.rationale 原文 */
  confidenceRationale?: string;

  category?: string;
  cwe?: string[];

  filePath?: string;
  startLine?: number;
  endLine?: number;

  description: string;

  evidence?: string;

  remediation?: string;

  /** 上游 rootCause 原文 */
  rootCause?: string;

  /* ---- attackPath 结构化映射 ---- */
  attackPath?: string; // 上游 summary 摘要
  entrypoint?: string;
  source?: string;
  dataflow?: string;
  sink?: string;
  reachability?: string;
  impact?: string;
  likelihood?: string;
  attackSteps?: string[];
  preconditions?: string[];

  /* ---- 证据与验证 ---- */
  codeEvidence?: CodeEvidenceItem[];
  validation?: unknown;

  remediationTests?: string[];
  preventiveControls?: string[];

  provenance?: unknown;
  extensions?: unknown;

  /** 上游原始 finding 对象（未裁剪） */
  raw?: unknown;

  /** 上游原始 ID，仅用于追溯，不作为业务主键 */
  engineFindingId?: string;
  engineOccurrenceId?: string;
  ruleId?: string;
}
