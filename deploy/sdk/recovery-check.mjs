import {readFile} from 'node:fs/promises';
import {createHash} from 'node:crypto';
import {pathToFileURL} from 'node:url';

// An immutable source audit of the installed version, not a capability claim
// inferred from current online documentation or a newer Codex Security build.
export async function verifyRecoveryBaseline(){
 const root=new URL('../../code-audit-scanner/node_modules/@openai/codex-security/',import.meta.url);
 const baseline=JSON.parse(await readFile(new URL('./recovery-baseline.json',import.meta.url),'utf8'));
 const installed=JSON.parse(await readFile(new URL('package.json',root),'utf8'));
 if(installed.version!==baseline.sdkVersion||baseline.checkpointResume!==false)throw new Error('Scan recovery SDK version is not certified');
 for(const [name,want] of Object.entries(baseline.files)){
  const got=createHash('sha256').update(await readFile(new URL(name,root))).digest('hex');
  if(got!==want)throw new Error('Scan recovery source evidence changed: '+name);
 }
 const sdk=await import(new URL('dist/index.js',root).href);
 if(sdk.VERSION!=='0.1.25'||typeof sdk.CodexSecurity.prototype.resume!=='undefined')throw new Error('Scan recovery surface changed');
 const api=await readFile(new URL('dist/api.js',root),'utf8');
 if(api.includes('__resumeDeepScan')||!api.includes('const thread = codex.startThread(')||!api.includes('"register-cli-scan"'))throw new Error('Scan creation baseline changed');
 return {sdkVersion:installed.version,checkpointResume:false,sourceFiles:Object.keys(baseline.files).length,conclusions:baseline.conclusions,modelCalls:0};
}
if(process.argv[1]&&pathToFileURL(process.argv[1]).href===import.meta.url){
 try{process.stdout.write(JSON.stringify(await verifyRecoveryBaseline(),null,2)+'\n');}catch(e){process.stderr.write(String(e)+'\n');process.exitCode=1;}
}
