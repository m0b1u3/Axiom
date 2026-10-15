export interface CliArgs {
  source?: string;
  output?: string;
  provider?: string;
  model?: string;
  mode?: string;
  timeout?: string;
  "task-id"?: string;
  "base-url"?: string;
  "max-cost"?: string;
  /** 逗号分隔的相对路径白名单（路径子集扫描）。 */
  include?: string;
  /** 逗号分隔的发现重点：authz,injection,ssrf,secrets,tenant,upload。 */
  focus?: string;
  mock?: string;
  [key: string]: string | undefined;
}

export function parseArgs(argv: string[]): CliArgs {
  const out: CliArgs = {};
  for (let i = 0; i < argv.length; i += 1) {
    const token = argv[i];
    if (!token?.startsWith("--")) continue;
    const key = token.slice(2);
    const next = argv[i + 1];
    if (next && !next.startsWith("--")) {
      out[key] = next;
      i += 1;
    } else {
      out[key] = "true";
    }
  }
  return out;
}

export function flagEnabled(value: string | undefined): boolean {
  return value === "true" || value === "1" || value === "yes";
}
