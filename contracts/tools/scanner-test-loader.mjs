// Test only: native TS execution resolves the scanner's compiled .js import names.
// Production continues to use its existing tsx/compiled entry points.
import {existsSync} from 'node:fs';
import {fileURLToPath} from 'node:url';
const prefix=new URL('../../code-audit-scanner/src/',import.meta.url).href;
export async function resolve(specifier,context,nextResolve) {
  // The standalone contract job installs only contracts dependencies. Resolve
  // the real generated contract build without relying on Scanner node_modules.
  if(specifier==='@axiom/contracts' && context.parentURL?.startsWith(prefix)) {
    return await nextResolve(new URL('../ts/dist/index.js',import.meta.url).href,context);
  }
  if(context.parentURL===new URL('../../code-audit-scanner/src/main.ts',import.meta.url).href && specifier==='./engine/codex-security-engine.js' && process.env.AXIOM_CONTRACT_ENGINE_STUB) {
    return await nextResolve(new URL(process.env.AXIOM_CONTRACT_ENGINE_STUB).href,context);
  }
  if(context.parentURL===new URL('../../code-audit-scanner/src/main.ts',import.meta.url).href && specifier==='./runtime/sdk-runtime.js' && process.env.AXIOM_CONTRACT_ENGINE_STUB) {
    return await nextResolve(new URL('../tests/fixtures/sdk-runtime.mjs',import.meta.url).href,context);
  }
  try{return await nextResolve(specifier,context)}
  catch(error) {
    if(error.code==='ERR_MODULE_NOT_FOUND' && specifier.endsWith('.js') && context.parentURL?.startsWith(prefix)) {
      const target=new URL(specifier.slice(0,-3)+'.ts',context.parentURL);
      if(target.protocol==='file:' && existsSync(fileURLToPath(target)))return await nextResolve(target.href,context);
    }
    throw error;
  }
}
