export async function resolve(specifier,context,nextResolve) {
  if(specifier==='@axiom/codex-security-compat-v1')specifier='@openai/codex-security';
  if(specifier==='@openai/codex-security'&&context.parentURL?.includes('/code-audit-scanner/')&&process.env.AXIOM_DEEP_RESUME_FIXTURE==='1'&&process.env.AXIOM_INPUT_TEST_CAPTURE)return nextResolve(new URL('./fixtures/deep-resume-sdk.mjs',import.meta.url).href,context);
  if(specifier==='@openai/codex-security'&&context.parentURL?.includes('/code-audit-scanner/')&&process.env.AXIOM_IDENTITY_TEST_FIXTURE==='1'&&process.env.AXIOM_INPUT_TEST_CAPTURE)return nextResolve(new URL('./fixtures/identity-sdk.mjs',import.meta.url).href,context);
  if(process.env.AXIOM_COMPONENT_SCAN_FIXTURE==='1'&&context.parentURL?.endsWith('/codex-security/dist/component-scan.js')&&specifier==='./scan-comparison.js')return nextResolve(new URL('./fixtures/component-scan-sdk.mjs',import.meta.url).href,context);
  if(specifier==='@openai/codex-security' && context.parentURL?.includes('/code-audit-scanner/') && process.env.AXIOM_INPUT_TEST_CAPTURE) {
    return nextResolve(new URL('./fixtures/input-sdk.mjs',import.meta.url).href,context);
  }
  return nextResolve(specifier,context);
}
