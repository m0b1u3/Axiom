// Native stage-0 diagnostics. Original SDK, synthetic source and loopback model
// fixture only. No production task configuration or real model credentials.
import assert from 'node:assert/strict';
import {createHash,randomUUID} from 'node:crypto';
import {execFile,spawn} from 'node:child_process';
import {createServer} from 'node:http';
import {createServer as tcpServer} from 'node:net';
import {mkdir,readFile,writeFile} from 'node:fs/promises';
import {existsSync,openSync,closeSync} from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {pathToFileURL,fileURLToPath} from 'node:url';
import {promisify} from 'node:util';
import {createRequire} from 'node:module';

const execute=promisify(execFile),here=path.dirname(fileURLToPath(import.meta.url));
const args=Object.fromEntries(process.argv.slice(2).reduce((rows,value,i,list)=>i%2?rows:[...rows,[value.slice(2),list[i+1]]],[]));
const root=path.resolve(here,'../..'),out=path.resolve(args.out),work=path.resolve(args.work);
assert.ok(['linux','win32'].includes(process.platform),'Stage 0 requires a native Linux or Windows host');
assert.equal(existsSync(work),false,'Use a fresh fixture directory; existing data must not be overwritten');
await mkdir(out,{recursive:true});await mkdir(path.dirname(work),{recursive:true,mode:0o700});await mkdir(work,{mode:0o700});
const lock=JSON.parse(await readFile(path.join(here,'dependencies.lock.json'),'utf8'));
const runtime=JSON.parse(await readFile(args.runtime,'utf8'));
const report={schemaVersion:1,stage:'native-stage0',platform:process.platform,arch:process.arch,
  osRelease:os.release(),node:process.version,startedAt:new Date().toISOString(),
  run:{commit:process.env.GITHUB_SHA??null,runId:process.env.GITHUB_RUN_ID??null,runnerImage:process.env.ImageVersion??null},
  sourceSnapshot:lock.sourceSnapshot,dependencyLockSha256:createHash('sha256').update(await readFile(path.join(here,'dependencies.lock.json'))).digest('hex'),
  fixtureOnly:true,realModelCalls:0,realSDKLogins:0,checks:[],runtimePreparation:runtime.components};
report.status='running';
const checkpoint=()=>writeFile(path.join(out,'current-report.json'),JSON.stringify(report,null,2)+'\n');
async function check(id,fn){
  report.currentStep=id;await checkpoint();
  try{const detail=await fn();const row={id,status:'passed',...detail};report.checks.push(row);console.log(JSON.stringify(row));return detail;}
  catch(error){const row={id,status:'failed',error:String(error.message),...(error.stderr?{stderr:error.stderr}:{}),...(error.stdout?{stdout:error.stdout}:{})};report.checks.push(row);console.log(JSON.stringify(row));return null;}
  finally{report.currentStep=null;await checkpoint();}
}
const original=process.env;
const environment={};
for(const [name,value] of Object.entries(original))if(/^(SystemRoot|WINDIR|ComSpec|PATHEXT|OS|NUMBER_OF_PROCESSORS|PROCESSOR_ARCHITECTURE)$/i.test(name))environment[name]=value;
environment.PATH=runtime.path.join(path.delimiter);
const userHome=runtime.userHome??path.join(work,'home');
Object.assign(environment,{HOME:userHome,USERPROFILE:userHome,
  CODEX_HOME:path.join(userHome,'.codex'),CODEX_SECURITY_STATE_DIR:runtime.sdkStateRoot??path.join(work,'sdk-state'),
  APPDATA:path.join(userHome,'AppData','Roaming'),LOCALAPPDATA:path.join(userHome,'AppData','Local'),
  TEMP:path.join(work,'tmp'),TMP:path.join(work,'tmp'),TMPDIR:path.join(work,'tmp'),
  PYTHON:runtime.python,CODEX_MCP_NODE_PATH:process.execPath,
  CUSTOM_API_KEY:'fixture-native-stage0-no-real-key'});
if(runtime.codexPath)environment.CODEX_CLI_PATH=runtime.codexPath;
for(const key of ['HOME','CODEX_HOME','CODEX_SECURITY_STATE_DIR','APPDATA','LOCALAPPDATA','TEMP'])await mkdir(environment[key],{recursive:true,mode:0o700});
for(const key of Object.keys(process.env))delete process.env[key];Object.assign(process.env,environment);
process.chdir(work);
const source=path.join(work,'源码 with spaces'),foreign=path.join(work,'foreign');
await mkdir(source,{recursive:true});await mkdir(foreign,{recursive:true});
const example=path.join(source,'example.py'),originalSource='print("Fixtures / Mock native stage 0")\n';
await writeFile(example,originalSource);
await check('target-and-account',async()=>{
  assert.ok(['linux','win32'].includes(process.platform));assert.equal(process.arch,'x64');assert.equal(process.versions.node,lock.runtime.node);
  if(process.platform==='linux'){
    assert.notEqual(process.getuid(),0,'Native SDK probe must run as a non-root account');
    assert.equal(existsSync('/.dockerenv'),false,'A container is not native-host acceptance');
    const policy={};for(const file of ['/proc/sys/kernel/unprivileged_userns_clone','/proc/sys/user/max_user_namespaces','/proc/sys/kernel/apparmor_restrict_unprivileged_userns'])policy[file]=existsSync(file)?(await readFile(file,'utf8')).trim():null;
    return {uid:process.getuid(),container:false,policy};
  }
  const result=await execute('powershell.exe',['-NoProfile','-Command','([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)'],{env:environment});
  assert.equal(result.stdout.trim().toLowerCase(),'false','Windows SDK evidence must come from a non-administrator account');
  return {administrator:false};
});
await check('locked-source-inputs',async()=>{
  for(const [relative,want] of Object.entries(lock.sourceInputs))assert.equal(createHash('sha256').update(await readFile(path.join(root,relative))).digest('hex'),want,relative+' differs from selected input snapshot');
  return {files:Object.keys(lock.sourceInputs).length};
});
await check('python-and-native-sqlite',async()=>{
  const result=await execute(runtime.python,['-c','import json,sys,sqlite3,tempfile; print(json.dumps({"version":sys.version.split()[0],"sqlite":sqlite3.sqlite_version,"encoding":sys.getfilesystemencoding()}))'],{env:environment});
  const value=JSON.parse(result.stdout);assert.equal(value.version,lock.runtime.python);
  const {DatabaseSync}=await import('node:sqlite');const db=new DatabaseSync(':memory:');try{value.nodeSQLite=db.prepare('select sqlite_version() as version').get().version;}finally{db.close();}
  return value;
});
await check('git-runtime',async()=>{
  const result=await execute(runtime.git,['--version'],{env:environment});
  assert.ok(result.stdout.includes(process.platform==='win32'?'2.56.0.windows.4':'2.56.0'));
  return {version:result.stdout.trim()};
});
let sdk,runtimeApi;
await check('sdk-load-platform-and-integrity',async()=>{
  const sdkRoot=path.join(root,'code-audit-scanner/node_modules/@openai/codex-security');
  const candidate=await import(pathToFileURL(path.join(sdkRoot,'dist/index.js')));
  const candidateRuntime=await import(pathToFileURL(path.join(sdkRoot,'dist/runtime.js')));
  const config=await import(pathToFileURL(path.join(sdkRoot,'dist/config.js')));
  assert.equal(candidate.VERSION,lock.runtime.sdk);assert.equal(config.DEFAULT_CODEX_CONFIG.features.multi_agent_v2.enabled,true);
  assert.equal(config.DEFAULT_CODEX_CONFIG.windows.sandbox,'elevated');
  const manifest=JSON.parse(await readFile(path.join(root,'deploy/sdk/manifest.json'),'utf8'));
  const platform=process.platform+'-'+process.arch,entry=manifest.packages.find(p=>p.platform===platform);assert.ok(entry,'No full platform binary inventory');
  const moduleRoot=runtime.codexModuleRoot??path.join(root,'code-audit-scanner/node_modules',entry.path);
  for(const [relative,want] of Object.entries(entry.files))assert.equal(createHash('sha256').update(await readFile(path.join(moduleRoot,relative))).digest('hex'),want,relative);
  const command=candidateRuntime.resolveCodexCommand(environment).command;
  assert.ok(command.replaceAll('\\','/').includes('/'+entry.path+'/'),'SDK selected an unpinned platform binary');
  const version=await execute(command,['--version'],{env:environment});assert.ok(version.stdout.includes(lock.runtime.codex));
  if(process.platform==='win32')createRequire(import.meta.url)(path.join(sdkRoot,'_bundled_plugin/mcp/native/win32-x64/windows.node'));
  sdk=candidate;runtimeApi=candidateRuntime;
  return {sdk:sdk.VERSION,codex:version.stdout.trim(),binaryFiles:Object.keys(entry.files).length,windowsSandbox:config.DEFAULT_CODEX_CONFIG.windows.sandbox};
});
if(runtimeApi){
  await check('plugin-python',async()=>{
    const python=await runtimeApi.resolvePluginPython({configuredPath:runtime.python,environment,protectedRoot:source});
    const script=path.join(root,'code-audit-scanner/node_modules/@openai/codex-security/_bundled_plugin/scripts/workbench_db.py');
    const result=await execute(python,[script,'--help'],{env:environment});assert.match(result.stdout,/usage:/i);
    return {python};
  });
  if(process.platform==='linux')await check('native-sandbox-preflight',async()=>{await runtimeApi.probeCodexSandbox(runtimeApi.resolveCodexCommand(environment),environment);return {upstreamProbe:true};});
}

function emit(res,items,id){
  res.writeHead(200,{'content-type':'text/event-stream','connection':'close'});
  const send=v=>res.write('event: '+v.type+'\ndata: '+JSON.stringify(v)+'\n\n');
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
const helper=path.join(source,'probe.py');
await writeFile(helper,`import errno,json,os,pathlib,sys,tempfile\nsource=pathlib.Path(sys.argv[1]); foreign=pathlib.Path(sys.argv[2])\nresult={'sourceRead':source.read_text(),'denied':[]}\nfor p in (source,foreign):\n try:\n  p.write_text('FIXTURE_WRITE_MUST_BE_DENIED'); result['denied'].append(False)\n except OSError as error:\n  if error.errno not in (errno.EACCES,errno.EPERM,errno.EROFS):raise\n  result['denied'].append(True)\nfd,name=tempfile.mkstemp(prefix='axiom-native-fixture-');os.close(fd);os.unlink(name)\nresult['temporaryFile']=name\nprint('AXIOM_NATIVE_PROOF='+json.dumps(result,ensure_ascii=True))\n`);
const auth=path.join(work,'fixture-auth.mjs');await writeFile(auth,"process.stdout.write(process.env.CUSTOM_API_KEY+'\\n');\n");
const quote=value=>process.platform==='win32'?"'"+value.replaceAll("'","''")+"'":"'"+value.replaceAll("'","'\"'\"'")+"'";
const shells=process.platform==='win32'?[['powershell',path.join(environment.SystemRoot??environment.SYSTEMROOT??'C:\\Windows','System32/WindowsPowerShell/v1.0/powershell.exe')]]:[['sh','/bin/sh'],['bash','/bin/bash']];
for(const [label,shell] of shells)await check('sdk-native-shell-'+label,async()=>{
  assert.ok(sdk&&runtimeApi,'SDK failed to load');
  const controller=new AbortController(),raw=path.join(work,'executions',label,'raw/codex-security');
  await mkdir(raw,{recursive:true,mode:0o700});
  let requests=0,catalogRequests=0,toolOutput=null,fixtureError=null,terminalError=null;
  const command=(process.platform==='win32'?'& ':'')+[runtime.python,helper,example,path.join(foreign,'unapproved.txt')].map(quote).join(' ');
  const server=createServer(async(req,res)=>{
    try{
      assert.equal(req.headers.authorization,'Bearer fixture-native-stage0-no-real-key');
      const route=new URL(req.url,'http://127.0.0.1').pathname;
      if(req.method==='GET'&&(route.endsWith('/models')||route.endsWith('/models/fixture-native-stage0'))){
        catalogRequests++;
        const model={id:'fixture-native-stage0',object:'model'};
        res.writeHead(200,{'content-type':'application/json'});res.end(JSON.stringify(route.endsWith('/models')?{object:'list',data:[model]}:model));return;
      }
      assert.equal(req.method,'POST');assert.ok(route.endsWith('/responses'));
      let data='';for await(const part of req)data+=part;const body=JSON.parse(data);requests++;
      if(requests===1){
        // Registration has completed before the first model request. Creating
        // this earlier violates the original SDK's empty-output invariant.
        await mkdir(path.join(raw,'artifacts/tmp'),{recursive:true,mode:0o700});
        const tool=body.tools.find(t=>t.type==='function'&&t.name==='exec_command');assert.ok(tool,'Original SDK did not expose native exec_command');
        emit(res,[{id:'fixture-call',type:'function_call',call_id:'fixture-call',name:tool.name,arguments:JSON.stringify({cmd:command,shell,login:false,yield_time_ms:10000}),status:'completed'}],'fixture-first');
      }else{
        assert.equal(requests,2,'Unexpected request after the finite diagnostic exchange');
        const item=body.input.find(x=>x.type==='function_call_output'&&x.call_id==='fixture-call');assert.ok(item,'Missing native tool result');
        toolOutput=typeof item.output==='string'?item.output:JSON.stringify(item.output);
        emit(res,[{id:'fixture-message',type:'message',role:'assistant',status:'completed',content:[{type:'output_text',text:'Fixtures / Mock diagnostic complete.',annotations:[]}]}],'fixture-second');
        setImmediate(()=>controller.abort(new Error('Fixture exchange complete; this is not an audit result')));
      }
    }catch(error){fixtureError=String(error);res.writeHead(500);res.end();controller.abort(error);}
  });
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const config={model:'fixture-native-stage0',model_provider:'axiom_fixture',model_reasoning_effort:'high',model_reasoning_summary:'none',
    features:{shell_snapshot:false,shell_snapshot_v2:false},cli_auth_credentials_store:'file',
    model_providers:{axiom_fixture:{name:'Local synthetic fixture',base_url:'http://127.0.0.1:'+server.address().port+'/v1',wire_api:'responses',requires_openai_auth:false,auth:{command:process.execPath,args:[auth],cwd:work}}}};
  const security=new sdk.CodexSecurity({pythonPath:runtime.python,codexOverrides:config});
  // This deadline contains a finite diagnostic fixture only. It is outside
  // Axiom's task input and never changes scan/review settings or SDK defaults.
  const deadline=setTimeout(()=>controller.abort(new Error('Native diagnostic fixture did not finish within 120 seconds')),120000);
  try{await security.run(source,{mode:'standard',outputDir:raw,archiveExisting:true,scanPrompt:'Fixtures / Mock only. Run the supplied native diagnostic tool. No real security assessment.',signal:controller.signal});}
  catch(error){terminalError=String(error.message);}
  finally{
    clearTimeout(deadline);
    await writeFile(path.join(out,'sdk-'+label+'-before-close.json'),JSON.stringify({requests,catalogRequests,fixtureError,terminalError,toolOutput},null,2)+'\n');
    await security.close();server.closeAllConnections();await new Promise(resolve=>server.close(resolve));
  }
  await writeFile(path.join(out,'sdk-'+label+'.json'),JSON.stringify({requests,catalogRequests,fixtureError,terminalError,toolOutput},null,2)+'\n');
  assert.equal(fixtureError,null,fixtureError);assert.ok(toolOutput,'No native shell evidence: '+terminalError);
  const line=toolOutput.split('\n').find(s=>s.startsWith('AXIOM_NATIVE_PROOF='));assert.ok(line,toolOutput);
  const proof=JSON.parse(line.slice('AXIOM_NATIVE_PROOF='.length));assert.equal(proof.sourceRead,originalSource);assert.deepEqual(proof.denied,[true,true]);
  assert.equal(await readFile(example,'utf8'),originalSource);assert.equal(existsSync(path.join(foreign,'unapproved.txt')),false);
  assert.ok(path.resolve(proof.temporaryFile).startsWith(path.resolve(raw)+path.sep),'Native temporary file escaped this execution');
  return {shell,fixtureResponsesRequests:requests,catalogRequests,sourceRead:true,sourceWriteDenied:true,foreignWriteDenied:true,temporaryFileScopedToExecution:true,completedAudit:false};
});

await check('postgres-runtime',async()=>{
  const bin=runtime.postgresBin,exe=name=>path.join(bin,name+(process.platform==='win32'?'.exe':''));
  async function control(arguments_){
    const log=path.join(out,'postgres-control.log'),fd=openSync(log,'a');
    try{
      const code=await new Promise((resolve,reject)=>{
        const child=spawn(exe('pg_ctl'),arguments_,{env:environment,stdio:['ignore',fd,fd],windowsHide:true});
        child.once('error',reject);child.once('exit',resolve);
      });
      assert.equal(code,0,'PostgreSQL control failed: '+await readFile(log,'utf8'));
    }finally{closeSync(fd);}
  }
  const version=await execute(exe('postgres'),['--version'],{env:environment});assert.ok(version.stdout.includes(lock.runtime.postgresql));
  const database=path.join(work,'postgres'),passwordFile=path.join(work,'pg-password');
  const password=randomUUID();await writeFile(passwordFile,password,{mode:0o600});
  await execute(exe('initdb'),['-D',database,'--encoding=UTF8','--locale=C','--username=axiom_probe','--auth=scram-sha-256','--pwfile='+passwordFile],{env:environment});
  const socket=tcpServer();await new Promise(resolve=>socket.listen(0,'127.0.0.1',resolve));const port=socket.address().port;await new Promise(resolve=>socket.close(resolve));
  let started=false;
  try{
    await control(['-D',database,'-l',path.join(work,'pg.log'),'-o','-h 127.0.0.1 -p '+port,'start']);started=true;
    const result=await execute(exe('psql'),['-h','127.0.0.1','-p',String(port),'-U','axiom_probe','-d','postgres','-At','-c',"select version(),current_setting('server_encoding')"],{env:{...environment,PGPASSWORD:password}});
    assert.ok(result.stdout.includes('UTF8'));return {version:version.stdout.trim(),localEphemeralDatabase:true,authentication:'scram-sha-256'};
  }finally{if(started)await control(['-D',database,'-m','fast','stop']);}
});
await check('pdf-browser-runtime',async()=>{
  const version=await execute(runtime.browser,['--version'],{env:environment});assert.ok(version.stdout.includes(lock.runtime.chromium));
  const html=path.join(work,'report.html'),pdf=path.join(work,'report.pdf');
  await writeFile(html,'<!doctype html><meta charset="utf-8"><style>@font-face{font-family:Probe;src:url("'+pathToFileURL(runtime.font).href+'")}body{font-family:Probe}</style><h1>Axiom 原生报告</h1><p>Fixtures / Mock only</p>');
  // Match the existing PDF implementation. This flag concerns Chromium only;
  // the SDK sandbox is tested separately and is never disabled by the probe.
  await execute(runtime.browser,['--headless=new','--disable-gpu','--no-sandbox','--no-pdf-header-footer','--user-data-dir='+path.join(work,'browser-profile'),'--print-to-pdf='+pdf,pathToFileURL(html).href],{env:environment});
  const bytes=await readFile(pdf);assert.ok(bytes.subarray(0,5).toString()==='%PDF-');
  return {version:version.stdout.trim(),pdfBytes:bytes.length,sha256:createHash('sha256').update(bytes).digest('hex'),existingPdfNoSandboxFlag:true,fontSha256:createHash('sha256').update(await readFile(runtime.font)).digest('hex')};
});
report.status=report.checks.every(c=>c.status==='passed')?'passed':'partial';report.completedAt=new Date().toISOString();
await checkpoint();
await writeFile(path.join(out,'report.json'),JSON.stringify(report,null,2)+'\n');
console.log(JSON.stringify({status:report.status,passed:report.checks.filter(c=>c.status==='passed').length,failed:report.checks.filter(c=>c.status==='failed').map(c=>c.id),realModelCalls:0}));
if(report.status!=='passed')process.exitCode=1;
