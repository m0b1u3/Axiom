// Fixtures / Mock. Run only in the network-none native SDK acceptance container.
import assert from 'node:assert/strict';
import {readFile,writeFile,mkdir,mkdtemp,rm,cp} from 'node:fs/promises';
import {existsSync} from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {createHash} from 'node:crypto';
import {createServer} from 'node:http';
import {spawn} from 'node:child_process';
import path from 'node:path';
import {tmpdir} from 'node:os';
import {fileURLToPath,pathToFileURL} from 'node:url';
assert.equal(process.env.AXIOM_ISOLATED_DEEP_RESUME_FIXTURE,'1','Use the isolated native acceptance runner');
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'../..'),scanner=path.join(root,'code-audit-scanner');
const moduleAt=name=>import(pathToFileURL(path.join(scanner,'dist',name+'.js')));
if(process.argv[2]==='--child'){
 const task=JSON.parse(await readFile(process.argv[3],'utf8'));
 const {enterSDKWorkingDirectory}=await moduleAt('runtime/sdk-working-directory');
 const restore=await enterSDKWorkingDirectory(task.deepResume?path.join(path.dirname(task.outputPath),'attempt-1'):task.outputPath);
 const controller=new AbortController();process.once('SIGTERM',()=>controller.abort(new Error('Fixtures / Mock boundary reached')));
 try{const {CodexSecurityEngine}=await moduleAt('engine/codex-security-engine');const result=await new CodexSecurityEngine().scan(task,controller.signal);console.log(JSON.stringify({fixtureOnly:true,status:result.status,error:result.error??null}));}finally{restore()}
}else{
 const scratch=await mkdtemp(path.join(tmpdir(),'axiom-native-deep-resume-'));
 const executablePath=process.env.PATH;
 for(const k of Object.keys(process.env))delete process.env[k];
 Object.assign(process.env,{PATH:executablePath,HOME:path.join(scratch,'parent-home'),CODEX_HOME:path.join(scratch,'parent-home/.codex'),CUSTOM_API_KEY:'fixture-selected-not-real',AXIOM_ISOLATED_DEEP_RESUME_FIXTURE:'1'});
 await mkdir(process.env.CODEX_HOME,{recursive:true,mode:0o700});
 const source=path.join(scratch,'source');await mkdir(source,{mode:0o700});const bytes='# Fixtures / Mock only. No user source or real model.\n';await writeFile(path.join(source,'fixture.py'),bytes);
 const hash=createHash('sha256').update(Buffer.byteLength(bytes)+'\tfixture.py\t'+createHash('sha256').update(bytes).digest('hex')+'\n').digest('hex');
 const output=path.join(scratch,'scans/17/attempt-1'),state=path.join(output,'runtime/sdk');
 let phase='initial';const requests=[],children=[];const sockets=new Set();
 const server=createServer(async(req,res)=>{
  let body='';for await(const part of req)body+=part;
  const route=(req.url??'').split('?')[0],selectedCredential=req.headers.authorization==='Bearer fixture-selected-not-real';let value={};try{value=JSON.parse(body)}catch{}
  if(route.endsWith('/models')){res.writeHead(200,{'content-type':'application/json'});res.end(JSON.stringify({data:[{id:'fixture-native-deep',object:'model'}]}));return}
  if(!selectedCredential||req.method!=='POST'||!route.endsWith('/responses')){res.writeHead(401);res.end('{}');return}
  requests.push({phase,selectedCredential,reasoningEffort:value.reasoning?.effort??null,resumeInstruction:body.includes('Resume the existing Deep Scan through its coordinator')});
  res.writeHead(200,{'content-type':'text/event-stream'});res.write('event: response.created\ndata: {"type":"response.created","response":{"id":"fixture-pending","status":"in_progress","output":[]}}\n\n');
  const timer=setInterval(()=>res.write(': fixture keepalive\n\n'),1000);res.once('close',()=>clearInterval(timer));
 });server.on('connection',s=>{sockets.add(s);s.on('close',()=>sockets.delete(s))});
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const task={taskId:17,executionAttempt:1,resultSchemaVersion:1,sourcePath:source,sourceContentHash:hash,outputPath:output,provider:'custom',connectionType:'openai_compatible',credentialEnv:'CUSTOM_API_KEY',wireApi:'responses',baseUrl:'http://127.0.0.1:'+server.address().port+'/v1',model:'fixture-native-deep',scanMode:'deep',workers:1,timeoutMinutes:1,sdkParameters:{schemaVersion:1,execution:{taskId:'17',attempt:1},parameters:{schemaVersion:1,reasoningEffort:'high'}},deepResumeContext:{schemaVersion:1,execution:{taskId:'17',attempt:1},agentId:'fixture-native-agent',configurationHash:'b'.repeat(64)}};
 task.resolvedModel={providerId:'custom',connectionType:'openai_compatible',model:task.model,baseUrl:task.baseUrl,wireApi:'responses',credentialEnv:'CUSTOM_API_KEY',configurationRevision:'fixture-native-deep'};
 async function waitFor(predicate,message,limit=45000){const end=Date.now()+limit;while(Date.now()<end){if(await predicate())return;await new Promise(r=>setTimeout(r,100))}throw new Error(message)}
 async function child(t,label){const file=path.join(scratch,label+'.json');await writeFile(file,JSON.stringify(t));const p=spawn(process.execPath,[fileURLToPath(import.meta.url),'--child',file],{detached:true,env:{...process.env,CODEX_SECURITY_STATE_DIR:state},stdio:['ignore','pipe','pipe']});let log='';p.stdout.on('data',v=>log+=v);p.stderr.on('data',v=>log+=v);const done=new Promise(resolve=>p.once('exit',(code,signal)=>resolve({code,signal})));p.fixtureLog=()=>log;children.push(p);return {p,done,log:()=>log}}
 function nativeRow(){try{const db=new DatabaseSync(path.join(state,'workbench.sqlite3'),{readOnly:true});try{return db.prepare('SELECT id,status,mode,continuation_thread_id FROM scans').get()}finally{db.close()}}catch{return null}}
 let report={status:'failed',fixtureOnly:true,realModelCalls:0,realSDKLogins:0};
 try{
  const initial=await child(task,'initial');await waitFor(()=>{if(initial.p.exitCode!==null)throw new Error('Native initial child failed: '+initial.log());return requests.some(v=>v.phase==='initial')&&nativeRow()?.continuation_thread_id},'Native Deep first model boundary not reached: '+initial.log());
  const before=nativeRow();assert.equal(before.mode,'deep');assert.equal(before.status,'running');
  process.kill(-initial.p.pid,'SIGKILL');await initial.done;
  const {inspectDeepResume}=await moduleAt('runtime/deep-resume');process.env.AXIOM_DEEP_RESUME_EXPECTED_STATE_ROOT=state;
  const claim={schemaVersion:1,id:'fixture-native-probe',leaseToken:'fixture-only',execution:{taskId:'17',attempt:1},origin:{taskId:'17',attempt:1},agentId:'fixture-native-agent',sourcePath:source,outputPath:output,sourceHash:hash,configurationHash:'b'.repeat(64),model:task.model,identity:null};
  let inspection;await waitFor(async()=>{inspection=await inspectDeepResume(claim);return inspection.eligible},'Killed native Deep was not resumable',10000);assert.equal(inspection.scanId,before.id);assert.equal(inspection.threadId,before.continuation_thread_id);assert.equal(inspection.baseline.estimatedUsd,null);
  phase='resume';const resumedTask={...task,executionAttempt:2,outputPath:path.join(scratch,'scans/17/attempt-2'),sdkParameters:{...task.sdkParameters,execution:{taskId:'17',attempt:2}},deepResume:{schemaVersion:1,execution:{taskId:'17',attempt:2},previous:claim.execution,inspection}};
  const resumed=await child(resumedTask,'resumed');await waitFor(()=>{if(resumed.p.exitCode!==null)throw new Error('Native resume child failed: '+resumed.log());return requests.some(v=>v.phase==='resume')},'Native resume did not reach the model boundary: '+resumed.log());
  assert.ok(requests.some(v=>v.phase==='resume'&&v.resumeInstruction&&v.selectedCredential&&v.reasoningEffort==='high'));
  const after=nativeRow();assert.equal(after.id,before.id);assert.equal(after.continuation_thread_id,before.continuation_thread_id);
  resumed.p.kill('SIGTERM');await Promise.race([resumed.done,new Promise((_,reject)=>setTimeout(()=>reject(new Error('Native resume did not stop at the fixture boundary')),15000))]);
  assert.equal(await readFile(path.join(source,'fixture.py'),'utf8'),bytes);assert.equal(existsSync(path.join(state,'codex-home/auth.json')),false);
  const db=new DatabaseSync(path.join(state,'workbench.sqlite3'),{readOnly:true});try{assert.equal(db.prepare('SELECT COUNT(*) AS n FROM scans').get().n,1)}finally{db.close()}
  assert.ok(existsSync(path.join(resumedTask.outputPath,'runtime/resume-before/workbench.sqlite3')));
  report={status:'passed',fixtureOnly:true,sdk:'0.5.0',nativeCrash:true,nativeOriginalSessionResume:true,scanIdPreserved:true,checkpointBackup:true,sourceHashPreserved:true,unknownPricingPreserved:true,requests,realModelCalls:0,realSDKLogins:0};
 }catch(error){report={...report,error:String(error)};for(const [i,p] of children.entries())report['child'+i+'Exit']={exitCode:p.exitCode,signalCode:p.signalCode};throw error}
 finally{for(const p of children){try{process.kill(-p.pid,'SIGKILL')}catch{}}for(const s of sockets)s.destroy();await new Promise(resolve=>server.close(resolve));console.log(JSON.stringify(report));await rm(scratch,{recursive:true,force:true})}
}
