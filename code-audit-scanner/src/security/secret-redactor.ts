const SECRET_PATTERNS: Array<{ name: string; regex: RegExp }> = [
  { name: "openai", regex: /\bsk-[A-Za-z0-9_\-]{10,}\b/g },
  { name: "bearer", regex: /\bBearer\s+[A-Za-z0-9\-._~+/]+=*/gi },
  { name: "generic_key", regex: /\b(api[_-]?key|secret|token|password)\s*[:=]\s*['"][^'"]+['"]/gi },
  { name: "dashscope", regex: /\bsk-[a-z0-9]{20,}\b/gi },
];

/**
 * 日志与错误信息脱敏。不要依赖它当唯一防线：
 * API Key 根本就不该进入 ScanTask / result.json / 报告。
 */
export function redactSecrets(input: string): string {
  let output = input;
  for (const pattern of SECRET_PATTERNS) {
    output = output.replace(pattern.regex, `[REDACTED:${pattern.name}]`);
  }
  return output;
}
