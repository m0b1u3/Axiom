export async function resolve(specifier, context, nextResolve) {
  if (specifier === '@openai/codex-security') {
    if (process.env.AXIOM_STABILITY_FIXTURE !== '1' || !process.env.AXIOM_INPUT_TEST_CAPTURE) throw new Error('Explicit isolated stability fixture required');
    return nextResolve(new URL('./stability-sdk.mjs', import.meta.url).href, context);
  }
  if (specifier.startsWith('@openai/codex-security/')) throw new Error('Native SDK subpath is forbidden in stability acceptance');
  return nextResolve(specifier, context);
}
