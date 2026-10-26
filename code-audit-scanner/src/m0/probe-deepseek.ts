/**
 * Cheap DeepSeek connectivity probe. Reads DEEPSEEK_API_KEY from the
 * environment. Never print the key. Not a compatibility PASS.
 *
 *   DEEPSEEK_API_KEY=... npx tsx src/m0/probe-deepseek.ts
 */
const KEY = process.env["DEEPSEEK_API_KEY"];
const BASE = "https://api.deepseek.com";
const MODEL = "deepseek-v4-flash-vision-exp";

async function main(): Promise<void> {
  if (!KEY) {
    process.stderr.write("Missing DEEPSEEK_API_KEY\n");
    process.exitCode = 3;
    return;
  }
  process.stderr.write(`key_len=${KEY.length} model=${MODEL}\n`);

  await probe("GET", "/models");
  await probe("GET", "/v1/models");
  await probe("POST", "/responses", {
    model: MODEL,
    input: "ping",
    max_output_tokens: 16,
  });
}

async function probe(method: string, path: string, body?: unknown): Promise<void> {
  const response = await fetch(`${BASE}${path}`, {
    method,
    headers: {
      Authorization: `Bearer ${KEY}`,
      "Content-Type": "application/json",
    },
    ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
  });
  const text = await response.text();
  const clipped = text.slice(0, 800).replace(KEY ?? "", "[REDACTED]");
  process.stdout.write(`${method} ${path} ${response.status}\n${clipped}\n---\n`);
}

main().catch((error: unknown) => {
  const message = error instanceof Error ? error.message : String(error);
  process.stderr.write(`${message}\n`);
  process.exitCode = 10;
});
