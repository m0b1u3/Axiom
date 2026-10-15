import { writeFile } from "node:fs/promises";
import path from "node:path";

/**
 * 把 SDK 对象的 JSON 快照额外落到 raw 目录。
 * 官方 SDK 已经把 findings.json / coverage.json / scan-manifest.json / report.md
 * 写进 outputDir；这里再留一份 toJSON()，方便 Scanner 升级后重新转换。
 */
export async function saveRawArtifacts(
  rawDir: string,
  sdkResult: { toJSON?: () => unknown },
): Promise<Record<string,unknown>|undefined> {
  if (typeof sdkResult.toJSON !== "function") return;
  const snapshot = path.join(rawDir, "sdk-result.json");
  const value=sdkResult.toJSON();
  if(!value || typeof value!=='object' || Array.isArray(value))throw new Error('SDK toJSON did not return an object');
  const serialized=JSON.stringify(value,null,2);
  await writeFile(snapshot,serialized+'\n',{encoding:'utf8',mode:0o600});
  return JSON.parse(serialized) as Record<string,unknown>;
}
