// Fixtures / Mock only: launched by native_sdk_acceptance.py in a network-none
// container with no business volumes, stored identities or model credentials.
import assert from 'node:assert/strict';
import {mkdir,mkdtemp,readFile,writeFile,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
import {createServer} from 'node:http';
assert.equal(process.env.AXIOM_ISOLATED_COLD_FIXTURE,'1','Use the isolated Docker acceptance runner');
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'../..');
const scanner=path.join(root,'code-audit-scanner');
const {CodexSecurity,VERSION}=await import(pathToFileURL(path.join(scanner,'node_modules/@openai/codex-security/dist/index.js')));
const {buildCodexOverrides}=await import(pathToFileURL(path.join(scanner,'dist/provider/model-provider.js')));
const {CodexSecurityEngine}=await import(pathToFileURL(path.join(scanner,'dist/engine/codex-security-engine.js')));
const {enterSDKWorkingDirectory}=await import(pathToFileURL(path.join(scanner,'dist/runtime/sdk-working-directory.js')));
assert.equal(VERSION,'0.5.0');
const scratch=await mkdtemp(path.join(tmpdir(),'axiom-native-cold-'));
// Never select a host or image ambient identity, even if this script is misused.
const executablePath=process.env.PATH;
for(const key of Object.keys(process.env))delete process.env[key];
Object.assign(process.env,{PATH:executablePath,HOME:path.join(scratch,'home'),CODEX_HOME:path.join(scratch,'home/.codex'),CODEX_SECURITY_STATE_DIR:path.join(scratch,'sdk-state'),CUSTOM_API_KEY:'fixture-selected-not-real'});
for(const key of ['HOME','CODEX_HOME','CODEX_SECURITY_STATE_DIR'])await mkdir(process.env[key],{recursive:true,mode:0o700});
const source=path.join(scratch,'source');await mkdir(source,{mode:0o700});
const bytes='# Fixtures / Mock: no user code, models or accounts.\n';await writeFile(path.join(source,'example.py'),bytes);
const output=path.join(scratch,'scans/1/attempt-1');await mkdir(path.join(output,'raw'),{recursive:true,mode:0o700});
await writeFile(path.join(output,'raw','previous-fixture.txt'),'SDK must archive this before native startup.\n');
const task={taskId:1,executionAttempt:1,sourcePath:source,outputPath:output,provider:'custom',connectionType:'openai_compatible',credentialEnv:'CUSTOM_API_KEY',wireApi:'responses',baseUrl:'http://127.0.0.1',model:'fixture-native-cold',scanMode:'standard',timeoutMinutes:1,sdkParameters:{schemaVersion:1,execution:{taskId:'1',attempt:1},parameters:{schemaVersion:1,reasoningEffort:'high'}}};
const requests=[];const controller=new AbortController();let authenticated=false,inferenceCalls=0;
const server=createServer(async(req,res)=>{
 let body='';for await(const part of req)body+=part;
 let value={};try{value=JSON.parse(body)}catch{}
 const route=(req.url??'').split('?')[0];const matches=req.headers.authorization==='Bearer fixture-selected-not-real';
 requests.push({method:req.method,path:route,selectedCredential:matches,reasoningEffort:value.reasoning?.effort??null});
 if(!matches){res.writeHead(401,{'Content-Type':'application/json'});res.end('{"error":"Fixtures / Mock credential mismatch"}');return;}
 if(route.endsWith('/models')){res.writeHead(200,{'Content-Type':'application/json'});res.end(JSON.stringify({data:[{id:task.model,object:'model'}]}));return;}
 if(req.method!=='POST'||!route.endsWith('/responses')){res.writeHead(404);res.end();return;}
 authenticated=true;inferenceCalls++;
 if(inferenceCalls===1){
  res.writeHead(200,{'Content-Type':'text/event-stream','Connection':'close'});
  res.end('event: response.failed\ndata: {"type":"response.failed","response":{"id":"fixture-transient","status":"failed","error":{"code":"server_error","message":"Fixtures / Mock transient upstream failure"}}}\n\n');return;
 }
 // Stop at the model boundary: validate the native process, provider command,
 // frozen reasoning parameter and local transport without any model inference.
 res.writeHead(200,{'Content-Type':'text/event-stream','Connection':'close'});
 res.end('event: response.failed\ndata: {"type":"response.failed","response":{"id":"fixture-stop","status":"failed","error":{"code":"fixture_stop","message":"Fixtures / Mock reached native inference boundary"}}}\n\n');
 setImmediate(()=>controller.abort(new Error('Fixtures / Mock reached native inference boundary')));
});
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));task.baseUrl='http://127.0.0.1:'+server.address().port+'/v1';
const restore=await enterSDKWorkingDirectory(output);const timer=setTimeout(()=>controller.abort(new Error('Fixtures / Mock cold start timed out')),45000);
try{
 const security=new CodexSecurity({codexOverrides:buildCodexOverrides(task)});
 try{const preflight=await security.preflight(source,{outputDir:path.join(scratch,'preflight')});assert.equal(preflight.authentication.method,'command');assert.equal(preflight.authentication.verified,false);}finally{await security.close();}
 const result=await new CodexSecurityEngine().scan(task,controller.signal);
 assert.equal(inferenceCalls,2,"Engine must preserve native stream retry policy without proxy replay");
 assert.equal(authenticated,true,'Native SDK never reached its isolated model gateway: '+JSON.stringify(result.error));
 assert.ok(requests.some(r=>r.method==='POST'&&r.path.endsWith('/responses')&&r.selectedCredential&&r.reasoningEffort==='high'),'Selected credential / high reasoning did not reach the native request');
 assert.ok(['canceled','failed','partial'].includes(result.status),'Fixture boundary must not masquerade as a complete audit');
 assert.equal(await readFile(path.join(source,'example.py'),'utf8'),bytes);
 assert.equal(process.env.OPENAI_API_KEY,undefined);assert.equal(process.env.CODEX_API_KEY,undefined);
 console.log(JSON.stringify({status:'passed',fixtureOnly:true,sdk:VERSION,nativeColdStart:true,nativeStreamRetry:true,selectedCredential:true,reasoningEffort:'high',readOnlyInstallation:true,stableTemporaryDirectory:true,requests,realModelCalls:0,realSDKLogins:0}));
}finally{clearTimeout(timer);server.closeAllConnections();await new Promise(resolve=>server.close(resolve));restore();await rm(scratch,{recursive:true,force:true});}
