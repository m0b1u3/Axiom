/**
 * Convert an existing Codex Security raw directory into normalized/result.json
 * without starting a scan.
 *
 *   npx tsx src/m0/salvage-raw.ts --raw <rawDir> --output <outputDir> --task-id 1 --provider deepseek --model deepseek-v4-flash-vision-exp
 */
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { parseArgs } from "../cli/args.js";
import { loadRawCodexResult } from "../artifact/load-raw-result.js";
import { convertCodexResult } from "../converter/codex-finding-converter.js";
import type { ScanMode, ScanProvider, ScanTask } from "../contract/scan-task.js";
import { resolveProvider } from "../provider/model-provider.js";
import { SCANNER_LAYOUT } from "../contract/workspace.js";
import { loadFixtureManifest, scoreFixtures, defaultFixtureManifestPath } from "../eval/fixture-score.js";

const args = parseArgs(process.argv.slice(2));
const rawDir = path.resolve(args.raw ?? "");
const outputPath = path.resolve(args.output ?? path.dirname(rawDir));
if (!args.raw) {
  process.stderr.write("Usage: salvage-raw --raw <rawDir> --output <outputDir>\n");
  process.exit(2);
}

const provider = (args.provider as ScanProvider | undefined) ?? "deepseek";
const spec = resolveProvider(provider);
const task: ScanTask = {
  taskId: Number(args["task-id"] ?? 1),
  sourcePath: args.source ?? ".",
  outputPath,
  provider,
  model: args.model ?? spec.defaultModel,
  scanMode: (args.mode as ScanMode | undefined) ?? "standard",
  timeoutMinutes: 1,
};

const recovered = await loadRawCodexResult(rawDir);
if (!recovered) {
  process.stderr.write(`No salvageable findings in ${rawDir}\n`);
  process.exit(2);
}

const converted = convertCodexResult(task, recovered, {
  startedAt: new Date().toISOString(),
  finishedAt: new Date().toISOString(),
  rawArtifactDir: SCANNER_LAYOUT.rawDir,
  normalizedResultPath: SCANNER_LAYOUT.normalizedResult,
});
converted.status = converted.findings.length > 0 ? "partial" : "failed";

const outFile = path.join(outputPath, converted.normalizedResultPath);
await mkdir(path.dirname(outFile), { recursive: true, mode: 0o700 });
await writeFile(outFile, `${JSON.stringify(converted, null, 2)}\n`, "utf8");
process.stdout.write(`${JSON.stringify(converted, null, 2)}\n`);

try {
  const fixtures = await loadFixtureManifest(defaultFixtureManifestPath());
  const score = scoreFixtures(converted.findings, fixtures);
  process.stderr.write(
    `fixture-score ${score.hits}/${score.total} ${score.results
      .map((item) => `${item.id}:${item.hit ? "HIT" : "MISS"}`)
      .join(" ")}\n`,
  );
} catch {
  // optional
}
