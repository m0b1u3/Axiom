import { readFile } from "node:fs/promises";
import path from "node:path";
import type { CodexLikeFinding, CodexLikeScanResult } from "../converter/codex-finding-converter.js";

/**
 * SDK 抛错时，官方 runtime 仍可能已经写出 findings.json。
 * 业务层必须回收，不能因为进程非 0 就把 finding 丢掉。
 */
export async function loadRawCodexResult(rawDir: string): Promise<CodexLikeScanResult | undefined> {
  const findingsDoc = await readJson(path.join(rawDir, "findings.json"));
  const rawFindings = findingsDoc?.["findings"];
  if (!Array.isArray(rawFindings)) {
    return undefined;
  }

  const result: CodexLikeScanResult = {
    findings: { findings: rawFindings as CodexLikeFinding[] },
  };

  const manifest = await readJson(path.join(rawDir, "scan-manifest.json"));
  const scan = manifest?.["scan"];
  if (scan && typeof scan === "object" && !Array.isArray(scan)) {
    const record = scan as Record<string, unknown>;
    result.manifest = {
      scan: {
        ...(typeof record["status"] === "string" ? { status: record["status"] } : {}),
        ...(typeof record["startedAt"] === "string" ? { startedAt: record["startedAt"] } : {}),
        ...(typeof record["completedAt"] === "string" ? { completedAt: record["completedAt"] } : {}),
      },
    };
  }

  const coverage = await readJson(path.join(rawDir, "coverage.json"));
  if (typeof coverage?.["completeness"] === "string") {
    result.coverage = { ...coverage, completeness: coverage["completeness"] };
  }

  result.toJSON=()=>({manifest:manifest??null,findings:findingsDoc,coverage:coverage??null,recoveredArtifacts:true});
  return result;
}

async function readJson(file: string): Promise<Record<string, unknown> | undefined> {
  try {
    const parsed: unknown = JSON.parse(await readFile(file, "utf8"));
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>;
    }
    return undefined;
  } catch {
    return undefined;
  }
}
