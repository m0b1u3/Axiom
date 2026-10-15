import {test} from 'node:test';
import assert from 'node:assert/strict';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
import {mkdtemp,cp,appendFile} from 'node:fs/promises';
import os from 'node:os';
test('pinned custom workflow loads and still rejects source drift without a model call',async()=>{
 const root=path.resolve('node_modules/@openai/codex-security');
 const sdk=await import(pathToFileURL(path.join(root,'dist/custom-validation-prompt.js')).href);
 const plugin=path.join(root,'_bundled_plugin');const prompt=await sdk.customDiscoveryPrompt(plugin,'security-scan');assert.match(prompt,/separate custom validation pass/);assert.match(prompt,/custom_pending/);
 const config=await sdk.customValidationConfig({},plugin);assert.ok(config.mcp_servers);
 const changed=await mkdtemp(path.join(os.tmpdir(),'axiom-sdk-contract-'));await cp(path.join(plugin,'references'),path.join(changed,'references'),{recursive:true});await cp(path.join(plugin,'skills/security-scan'),path.join(changed,'skills/security-scan'),{recursive:true});await appendFile(path.join(changed,'references/core-scan.md'),'\nchanged\n');await assert.rejects(sdk.customDiscoveryPrompt(changed,'security-scan'),/incompatible/);
});
