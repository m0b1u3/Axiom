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
  "执行可靠性要求：",
  "- 子任务必须以非空最终结论结束：明确范围、证据路径、候选发现或未发现，以及尚未完成的工作。工具输出不等于最终审计结论。",
  "- wait_agent 返回 completed:null/空结果时，不计为审计完成；最多请求该子任务补交一次。如果仍为空，由主任务接手该范围并记录未完成覆盖，不无限重启 worker。",
  "- 每完成一个审计范围，立即把审查记录、候选发现与证据保存到 CODEX_SECURITY_SCAN_DIR/artifacts/checkpoints/；没有候选时也写明检查范围和限制。保留 SDK 正式产物 schema，不把未验证候选写成确认漏洞。",
  "- 只报告有审查记录对应的已完成数量，读取文件或搜索命中不算完成审计。",
  "- 预检完成后立即进入源码审计；复用宿主提供的 helper 结果和明确参数，不反复读取用法。",
  "工作范围：",
  "- 审计对象是 CODEX_SECURITY_REPOSITORY 下的目标源码。读完技能文档后立刻审仓库。",
  "- 按技能要求调用插件 helper（config_preflight.py、resolve_security_md.py 等）；不要逐行切片阅读任何文件。",
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
