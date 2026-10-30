// Fixtures / Mock only: native SDK + native shell, loopback responses; no model calls.
import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {spawnSync} from 'node:child_process';
import {mkdtemp,mkdir,writeFile,readFile,rm} from 'node:fs/promises';
import {mkdirSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {fileURLToPath} from 'node:url';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
assert.equal(process.env.AXIOM_ISOLATED_TEMP_FIXTURE,'1','Use the disposable network-none Linux runner');
assert.equal(process.platform,'linux');assert.equal(process.arch,'x64');assert.notEqual(process.getuid(),0);
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'../..');
const scanner=path.join(root,'code-audit-scanner');
const {CodexSecurity,VERSION}=await import(pathToFileURL(scanner+'/node_modules/@openai/codex-security/dist/index.js'));
const {buildCodexOverrides}=await import(pathToFileURL(scanner+'/dist/provider/model-provider.js'));
const {ensureModelCatalog}=await import(pathToFileURL(scanner+'/dist/engine/codex-security-engine.js'));
const {enterSDKWorkingDirectory}=await import(pathToFileURL(scanner+'/dist/runtime/sdk-working-directory.js'));
const {startReliableUpstream}=await import(pathToFileURL(scanner+'/dist/proxy/reliable-upstream.js'));
assert.equal(VERSION,'0.5.0');
const {resolveCodexCommand}=await import(pathToFileURL(scanner+'/node_modules/@openai/codex-security/dist/runtime.js'));
const {DEFAULT_CODEX_CONFIG}=await import(pathToFileURL(scanner+'/node_modules/@openai/codex-security/dist/config.js'));
assert.equal(DEFAULT_CODEX_CONFIG.features.multi_agent,undefined);assert.equal(DEFAULT_CODEX_CONFIG.features.multi_agent_v2.enabled,true);
const scratch=await mkdtemp(path.join(tmpdir(),'axiom-native-temp-'));
const executablePath=process.env.PATH;
for(const name of Object.keys(process.env))delete process.env[name];
Object.assign(process.env,{PATH:executablePath,HOME:path.join(scratch,'home'),CODEX_HOME:path.join(scratch,'home/.codex'),CODEX_SECURITY_STATE_DIR:path.join(scratch,'sdk-state'),CUSTOM_API_KEY:'fixture-temp-no-real-key',TMPDIR:path.join(scratch,'ambient-tmp')});
for(const name of ['HOME','CODEX_HOME','CODEX_SECURITY_STATE_DIR','TMPDIR'])await mkdir(process.env[name],{recursive:true,mode:0o700});
const source=path.join(scratch,'source');await mkdir(source);const original='print("Fixtures / Mock only")\n';await writeFile(path.join(source,'example.py'),original);
const restore=await enterSDKWorkingDirectory(scratch);
const version=spawnSync(resolveCodexCommand(process.env).command,['--version'],{encoding:'utf8',timeout:10000});assert.equal(version.status,0);assert.match(version.stdout,/0\.163\.0/);
const cases=new Map(),failures=[];
function emit(res,items,id){
 res.writeHead(200,{'content-type':'text/event-stream','connection':'close'});
 const send=v=>res.write('data: '+JSON.stringify(v)+'\n\n');
 send({type:'response.created',response:{id,status:'in_progress',output:[]}});
 for(const [i,item] of items.entries()){
  send({type:'response.output_item.added',output_index:i,item:{...item,status:'in_progress',...(item.type==='function_call'?{arguments:''}:{content:[]})}});
  if(item.type==='function_call'){
   send({type:'response.function_call_arguments.delta',output_index:i,item_id:item.id,delta:item.arguments});
   send({type:'response.function_call_arguments.done',output_index:i,item_id:item.id,name:item.name,arguments:item.arguments});
  }else send({type:'response.output_text.delta',output_index:i,item_id:item.id,content_index:0,delta:item.content[0].text});
  send({type:'response.output_item.done',output_index:i,item});
 }
 send({type:'response.completed',response:{id,status:'completed',output:items,usage:{input_tokens:1,output_tokens:1,total_tokens:2}}});res.end();
}
const up=createServer(async(req,res)=>{
 try{
  let bytes='';for await(const part of req)bytes+=part;
  assert.equal(req.method,'POST');assert.ok(req.url.endsWith('/responses'));
  const body=JSON.parse(bytes),label=typeof body.model==='string'?body.model.replace(/^fixture-native-temp-/, ''):undefined;
  const item=cases.get(label);assert.ok(item,'Unknown fixture case');assert.equal(req.headers.authorization,'Bearer fixture-temp-no-real-key');
  item.requests++;assert.ok(item.requests<=2,'Unexpected extra fixture request');
  if(item.requests===1){
   const tool=body.tools.find(t=>t.type==='function'&&t.name==='exec_command');assert.ok(tool,'Native exec_command absent: '+JSON.stringify(body.tools.map(t=>t.name)));
   emit(res,[{id:'fixture-call-'+label,type:'function_call',call_id:'fixture-call-'+label,name:tool.name,arguments:JSON.stringify({cmd:item.command,shell:item.shell,login:false,yield_time_ms:10000,max_output_tokens:2000}),status:'completed'}],'fixture-'+label+'-1');
  }else{
   const output=body.input.find(v=>v.type==='function_call_output'&&v.call_id==='fixture-call-'+label);assert.ok(output,'Native shell output absent');
   item.output=typeof output.output==='string'?output.output:JSON.stringify(output.output);
   emit(res,[{id:'fixture-msg-'+label,type:'message',role:'assistant',status:'completed',content:[{type:'output_text',text:'Fixture shell probe completed.',annotations:[]}]}],'fixture-'+label+'-2');
   setImmediate(()=>item.controller.abort(new Error('Fixture: stop after native shell evidence')));
  }
 }catch(e){failures.push(String(e));res.writeHead(500,{'content-type':'application/json'});res.end(JSON.stringify({error:{message:String(e)}}));for(const item of cases.values())item.controller.abort(e);}
});
await new Promise(resolve=>up.listen(0,'127.0.0.1',resolve));
const proxy=await startReliableUpstream('http://127.0.0.1:'+up.address().port,path.join(scratch,'gateway.sqlite'));
async function run(label,shell,foreign){
 const output=path.join(scratch,'with spaces',label),raw=path.join(output,'raw/codex-security'),temp=path.join(raw,'artifacts/tmp');
 await mkdir(raw,{recursive:true,mode:0o700});await writeFile(path.join(raw,'old-fixture.txt'),'archive this');
 const task={taskId:label,executionAttempt:1,sourcePath:source,outputPath:output,provider:'custom',connectionType:'openai_compatible',credentialEnv:'CUSTOM_API_KEY',wireApi:'responses',baseUrl:proxy.baseUrl,model:'fixture-native-temp-'+label,scanMode:'standard',timeoutMinutes:1};
 const config=buildCodexOverrides(task);config.model_catalog_json=await ensureModelCatalog(task);
 const controller=new AbortController();
 const payload='Fixtures / Mock only '.repeat(4096);
 const script=`set -eu\nprintf 'AXIOM_SHELL=%s\\n' \"\${BASH_VERSION-\${ZSH_VERSION-sh}}\"\nprintf 'AXIOM_TMPDIR=%s\\nAXIOM_TMPPREFIX=%s\\n' "\${TMPDIR-}" "\${TMPPREFIX-}"\nprintf 'direct write works\\n' > "$CODEX_SECURITY_SCAN_DIR/artifacts/tmp/direct.txt"\ncat > "$CODEX_SECURITY_SCAN_DIR/artifacts/tmp/proof.txt" <<'AXIOM_EOF'\n${payload}\nAXIOM_EOF\npython3 -c 'import tempfile,os; fd,name=tempfile.mkstemp(prefix="axiom-native-"); os.write(fd,b"fixture"); os.close(fd); print("AXIOM_TEMPFILE="+name); os.unlink(name)'\npython3 -c 'import json,os,pathlib,errno; paths=[pathlib.Path(os.environ["CODEX_SECURITY_REPOSITORY"])/"example.py",pathlib.Path(${JSON.stringify(foreign)})/"foreign-write.txt"]; out=[]\nfor p in paths:\n try:\n  p.write_text("UNAUTHORIZED_FIXTURE_WRITE"); out.append({"path":str(p),"denied":False})\n except OSError as e:\n  if e.errno not in [errno.EACCES,errno.EPERM,errno.EROFS]:raise\n  out.append({"path":str(p),"denied":True})\nprint(json.dumps(out))'\nprintf 'AXIOM_HEREDOC_OK\\n'`;
 const proof={label,shell,temp,controller,requests:0,command:script,output:null};cases.set(label,proof);
 const sdk=new CodexSecurity({codexOverrides:config}),timer=setTimeout(()=>controller.abort(new Error('Fixture timed out')),60000);
 try{await sdk.run(source,{mode:'standard',outputDir:raw,archiveExisting:true,scanPrompt:'AXIOM_TEMP_CASE:'+label+'; Fixtures / Mock shell diagnostic only.',onOutputDirReady:()=>mkdirSync(temp,{recursive:true,mode:0o700}),signal:controller.signal});}
 catch(e){proof.terminalError=String(e.message).slice(0,2000);}
 finally{clearTimeout(timer);await sdk.close();}
 assert.ok(proof.output,JSON.stringify({label,failures,error:proof.terminalError}));
 console.log(JSON.stringify({label,shell,requests:proof.requests,output:proof.output,terminalError:proof.terminalError}));
 assert.equal(await readFile(path.join(source,'example.py'),'utf8'),original);
 return proof;
}
try{
 const foreign=path.join(scratch,'foreign');await mkdir(foreign);
 const before={TMPDIR:process.env.TMPDIR,TMP:process.env.TMP,TEMP:process.env.TEMP,TMPPREFIX:process.env.TMPPREFIX};
 const rows=[];
 for(const [name,shell] of [['sh','/bin/sh'],['bash','/bin/bash']]){
  rows.push(await run(name+'-single',shell,foreign));
  rows.push(...await Promise.all(['a','b'].map(part=>run(name+'-'+part,shell,foreign))));
 }
 for(const item of rows){
  assert.equal(item.requests,2);assert.match(item.output,/Process exited with code 0/);
  assert.match(item.output,/AXIOM_HEREDOC_OK/);assert.match(item.output,/AXIOM_TEMPFILE=/);
  assert.equal((item.output.match(/"denied": true/g)||[]).length,2);
  const temporary=item.output.split('\n').find(line=>line.startsWith('AXIOM_TEMPFILE='))?.slice('AXIOM_TEMPFILE='.length);
  assert.ok(temporary&&path.resolve(temporary).startsWith(path.dirname(path.dirname(item.temp))+path.sep),'Native temporary fallback escaped its own scan');
  assert.equal(await readFile(path.join(item.temp,'proof.txt'),'utf8'),'Fixtures / Mock only '.repeat(4096)+'\n');
 }
 assert.deepEqual({TMPDIR:process.env.TMPDIR,TMP:process.env.TMP,TEMP:process.env.TEMP,TMPPREFIX:process.env.TMPPREFIX},before);assert.deepEqual(failures,[]);
 console.log(JSON.stringify({status:'passed',fixtureOnly:true,SDK:VERSION,Codex:version.stdout.trim(),runtime:'official-v2',platform:process.platform,arch:process.arch,uid:process.getuid(),
  shellEnvironmentOverrides:false,shells:['/bin/sh','/bin/bash'],nativeSDKRuns:rows.length,
  fixtureResponsesRequests:rows.reduce((sum,row)=>sum+row.requests,0),heredocBytes:Buffer.byteLength('Fixtures / Mock only '.repeat(4096)+'\n'),
  concurrentShells:2,temporaryFilesScopedToOwnScan:true,sourceWriteDenied:true,foreignWriteDenied:true,hostEnvironmentUnchanged:true,realModelCalls:0}));
}finally{restore();await proxy.close();up.closeAllConnections();await new Promise(resolve=>up.close(resolve));await rm(scratch,{recursive:true,force:true});}
