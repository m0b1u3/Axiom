export async function resolve(specifier,context,nextResolve) {
  if(specifier==='@openai/codex-security' && context.parentURL?.includes('/code-audit-scanner/') && process.env.AXIOM_INPUT_TEST_CAPTURE) {
    return nextResolve(new URL('./fixtures/input-sdk.mjs',import.meta.url).href,context);
  }
  return nextResolve(specifier,context);
}
