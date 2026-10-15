import { readFile } from "node:fs/promises";
import path from "node:path";
import type { Finding } from "../contract/finding.js";

export interface FixtureSpec {
  id: string;
  cwe: string[];
  files: string[];
}

export interface FixtureHit {
  id: string;
  expectedCwe: string[];
  hit: boolean;
  matchedFingerprints: string[];
}

export interface FixtureScore {
  total: number;
  hits: number;
  results: FixtureHit[];
}

export async function loadFixtureManifest(manifestPath: string): Promise<FixtureSpec[]> {
  const raw = JSON.parse(await readFile(manifestPath, "utf8")) as { fixtures: FixtureSpec[] };
  return raw.fixtures;
}

export function scoreFixtures(findings: Finding[], fixtures: FixtureSpec[]): FixtureScore {
  const results = fixtures.map((fixture) => {
    const matched = findings.filter((finding) => matchesFixture(finding, fixture));
    return {
      id: fixture.id,
      expectedCwe: fixture.cwe,
      hit: matched.length > 0,
      matchedFingerprints: matched.map((finding) => finding.fingerprint),
    };
  });
  return {
    total: fixtures.length,
    hits: results.filter((item) => item.hit).length,
    results,
  };
}

function matchesFixture(finding: Finding, fixture: FixtureSpec): boolean {
  const cwe = new Set((finding.cwe ?? []).map((item) => item.toUpperCase()));
  if (fixture.cwe.some((item) => cwe.has(item.toUpperCase()))) {
    return true;
  }
  const filePath = normalize(finding.filePath ?? "");
  return fixture.files.some((expected) => filePath.includes(normalize(expected)));
}

function normalize(value: string): string {
  return value.replaceAll("\\", "/").toLowerCase();
}

export function defaultFixtureManifestPath(from = process.cwd()): string {
  return path.resolve(from, "..", "test-fixtures", "manifest.json");
}
