/**
 * 交给 Codex Security 的 scanPrompt。
 *
 * 默认永远带上中文产出约束；场景重点是可叠加的几段，
 * 空列表 = 通用审计（不偏科）。
 */
export const FOCUS_KEYS = [
  "authz",
  "injection",
  "ssrf",
  "secrets",
  "tenant",
  "upload",
] as const;

export type ScanFocus = (typeof FOCUS_KEYS)[number];

export interface FocusPreset {
  key: ScanFocus;
  label: string;
  prompt: string;
}

export const FOCUS_PRESETS: readonly FocusPreset[] = [
  {
    key: "authz",
    label: "认证鉴权 / 越权",
    prompt:
      "发现重点：认证、会话、鉴权与越权。优先检查缺失鉴权、水平/垂直越权、IDOR、角色绕过、管理员接口未鉴权、JWT/session 伪造与固定。",
  },
  {
    key: "injection",
    label: "注入",
    prompt:
      "发现重点：注入类。优先检查 SQL/NoSQL 拼接、命令注入、模板注入、LDAP 与表达式注入。",
  },
  {
    key: "ssrf",
    label: "SSRF / 内网",
    prompt:
      "发现重点：SSRF 与内网访问。优先检查用户可控 URL 请求、webhook 回调、云元数据、把文件读取包装成 HTTP 的路径。",
  },
  {
    key: "secrets",
    label: "密钥泄露",
    prompt:
      "发现重点：密钥与凭证泄露。优先检查硬编码 key/token、密钥进仓、日志打印凭证。",
  },
  {
    key: "tenant",
    label: "多租户隔离",
    prompt:
      "发现重点：多租户隔离。优先检查 tenant_id 未强制、跨租户查询、共享缓存串数据。",
  },
  {
    key: "upload",
    label: "上传 / 路径穿越",
    prompt:
      "发现重点：文件上传与路径穿越。优先检查类型校验绕过、任意路径写入、解压路径逃逸。",
  },
];

const FOCUS_BY_KEY = new Map(FOCUS_PRESETS.map((p) => [p.key, p]));

const LANGUAGE_PROMPT = [
  "语言与产出格式（强制，覆盖技能默认英文）：",
  "- 面向审计员的全部文字使用简体中文：finding 的 title、summary、severity.rationale、confidence.rationale、attackPath、rootCause、remediation、codeEvidence.explanation、助手说明。",
  "- 代码、标识符、文件路径、命令、CWE、ruleId、函数名保持原文，不要翻译。",
  "- 漏洞标题必须是中文，例如「通过 GET id 的未授权 SQL 注入」，不要写 Unauthenticated SQL injection。",
].join("\n");

const WORKFLOW_PROMPT = [
  "审计流程、子任务编排、检查点与完成判定遵循当前 SDK 原生工作流。",
  "工作范围：",
  "- 审计对象是 CODEX_SECURITY_REPOSITORY 下的目标源码。读完技能文档后立刻审仓库。",
  "- 指导文件解析使用当前插件的 launch_codex_security_mcp --helper resolve-security-md，优先复用宿主已提供的解析结果；其他 helper 以当前插件文档为准。源码审查须保留上下文；构造证据时按指定行范围精确提取。",
  "- codeEvidence.code 必须是冻结源码指定范围的完整原始行，保留每行缩进、空格与整行内容；不得去缩进、截句、省略或改写。文字分析只写 explanation。使用宿主提供的 source-evidence 辅助程序提取与检查后再提交，不能把结构合法当作源码匹配。",
].join("\n");

export function isScanFocus(value: string): value is ScanFocus {
  return FOCUS_BY_KEY.has(value as ScanFocus);
}

export function normalizeFocuses(input: string[] | undefined): ScanFocus[] {
  if (input === undefined) return [];
  const seen = new Set<ScanFocus>();
  const out: ScanFocus[] = [];
  for (const raw of input) {
    const key = raw.trim();
    if (!key) continue;
    if (!isScanFocus(key)) {
      throw new Error(`unknown scan focus: ${raw}`);
    }
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(key);
  }
  return out;
}

/** 拼给 SDK ScanOptions.scanPrompt。始终含中文约束。 */
export function buildScanPrompt(focuses?: string[]): string {
  const keys = normalizeFocuses(focuses);
  const parts = [LANGUAGE_PROMPT, WORKFLOW_PROMPT];
  if (keys.length > 0) {
    parts.push("场景重点（在完整审计之外额外加力，不取代通用发现）：");
    for (const key of keys) {
      const preset = FOCUS_BY_KEY.get(key);
      if (preset) parts.push(`- ${preset.prompt}`);
    }
  }
  return parts.join("\n");
}
