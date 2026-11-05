// Run as the ordinary disposable account, so private directories have its owner.
import {mkdir,writeFile} from 'node:fs/promises';
import path from 'node:path';
const root=path.resolve(process.argv[2]);
for(const name of ['profile/.codex','sdk-state/codex-home'])await mkdir(path.join(root,name),{recursive:true,mode:0o700});
await writeFile(path.join(root,'sdk-state/codex-home/config.toml'),'[windows]\nsandbox = "elevated"\n',{mode:0o600});
