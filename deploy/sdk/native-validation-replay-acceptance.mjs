// Fixtures / Mock only. Replays retained title-only answer shapes through the
// real SDK 0.5.0 native process, never a live model or stored identity.
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {mkdir,mkdtemp,readFile,writeFile,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
import {createServer} from 'node:http';
const hostLoopback=process.platform==='darwin'&&process.env.AXIOM_HOST_VALIDATION_REPLAY_FIXTURE==='1';
assert.ok(process.env.AXIOM_ISOLATED_VALIDATION_REPLAY_FIXTURE==='1'||hostLoopback,'Use the isolated Docker runner or explicit macOS loopback fixture');
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'../..'),scanner=path.join(root,'code-audit-scanner');
const {runSDKOperation}=await import(pathToFileURL(path.join(scanner,'dist/engine/sdk-operation.js')));
const {enterSDKWorkingDirectory}=await import(pathToFileURL(path.join(scanner,'dist/runtime/sdk-working-directory.js')));
const {VERSION}=await import(pathToFileURL(path.join(scanner,'node_modules/@openai/codex-security/dist/index.js')));assert.equal(VERSION,'0.5.0');
const scratch=await mkdtemp(path.join(tmpdir(),'axiom-native-validation-replay-')),executablePath=process.env.PATH;
for(const key of Object.keys(process.env))delete process.env[key];
Object.assign(process.env,{PATH:executablePath,HOME:path.join(scratch,'home'),CUSTOM_API_KEY:'fixture-selected-not-real'});await mkdir(process.env.HOME,{recursive:true,mode:0o700});
const nativeFixtures=[
 {name:'read-source-before-assessment',disposition:'reportable',report:'## Fixtures / Mock assessment\n\n`src/query.ts:2` concatenates input. This statement follows one isolated file-read tool call; database behavior and runtime reproduction remain unverified.',status:'completed',toolRead:true},
 {name:'retained-review-11',disposition:'reportable',report:"## SQL Injection in `lookup()` (`a036597803b7bd848456a8ddfe314614ae901dbc6baad40631c55f7b77a44ae0` — 用户输入直接拼接进 SQL 语句)",status:'partial'},
 {name:'retained-review-9',disposition:'reportable',report:'## 静态发现评估：用户输入直接拼接进 SQL 语句（CWE-89）',status:'partial'},
 {name:'retained-review-10',disposition:'not_applicable',report:'## Static Finding Assessment — 用户输入直接拼接进 SQL 语句 (CWE-89)',status:'partial'},
 {name:'complete-assessment',disposition:'reportable',report:'## Fixtures / Mock assessment\n\n`src/query.ts:2-4` joins input into SQL. This is a synthetic source-only example; database behavior and deployment are unknown. No runtime reproduction.',status:'completed'},
];
const fixtures=['native_schema','json_object'].flatMap(mode=>nativeFixtures.map(f=>({...f,mode}))).concat([
 ...['missing','extra','wrong_disposition','invalid_json','empty'].map(name=>({name,mode:'json_object',status:'failed',invalid:true})),
 {name:'collapsed-body',mode:'json_object',disposition:'reportable',report:'## Assessment Disposition: reportable Rationale: content is glued to the heading',status:'partial'},
]);
const source=path.join(scratch,'source');await mkdir(path.join(source,'src'),{recursive:true,mode:0o700});await writeFile(path.join(source,'src/query.ts'),'// Fixtures / Mock only\nexport const query = (name) => "SELECT " + name;\n');
const proofs=[];
try{
 for(let index=0;index<fixtures.length;index++){
  const fixture=fixtures[index],taskId=index+1,output=path.join(scratch,'scans',String(taskId),'attempt-1');await mkdir(output,{recursive:true,mode:0o700});
  process.env.AXIOM_VALIDATION_OUTPUT_MODE=fixture.mode;
  const invalid={missing:JSON.stringify({disposition:'reportable'}),extra:JSON.stringify({disposition:'reportable',report:'Fixture body',extra:true}),wrong_disposition:JSON.stringify({disposition:'verified',report:'Fixture body'}),invalid_json:'{',empty:JSON.stringify({disposition:'reportable',report:'   '})};
  const text=fixture.invalid?invalid[fixture.name]:JSON.stringify({disposition:fixture.disposition,report:fixture.report}),controller=new AbortController();let calls=0;const requests=[];
  const upstream=createServer(async(req,res)=>{
   let raw='';for await(const part of req)raw+=part;
   const body=JSON.parse(raw);calls++;assert.equal(req.method,'POST');assert.ok(req.url.endsWith('/responses'));assert.equal(req.headers.authorization,'Bearer fixture-selected-not-real');
   if(fixture.mode==='native_schema'){
   assert.equal(body.text.format.type,'json_schema');assert.deepEqual(body.text.format.schema.properties.disposition.enum,['reportable','suppressed','not_applicable','deferred']);assert.equal(body.text.format.schema.properties.report.minLength,1);assert.equal(body.text.format.schema.properties.report.description,undefined);
   assert.equal(createHash('sha256').update(JSON.stringify(body.text.format)).digest('hex'),'146d5f8d876052c923acf7d97fdcfeb2cc5aee884e23736a42310a4ebe1b5d23','Native format must match the retained review-11 receipt');
   }else{assert.deepEqual(body.text.format,{type:'json_object'});assert.match(body.instructions,/Return exactly one JSON object/);assert.match(body.instructions,/additionalProperties/);}
   assert.ok(JSON.stringify(body.input).includes('full Markdown assessment'));
   requests.push({path:req.url,structuredOutput:true,fullAssessmentRequested:true,format:body.text.format,formatSha256:createHash('sha256').update(JSON.stringify(body.text.format)).digest('hex'),controls:{maxOutputTokens:body.max_output_tokens??null,verbosity:body.text?.verbosity??null,stopSpecified:Object.hasOwn(body,'stop')},sha256:createHash('sha256').update(raw).digest('hex')});
   if(calls>(fixture.toolRead?2:1)){res.writeHead(400);res.end('{"error":{"code":"fixture_no_replay"}}');return;}
   res.writeHead(200,{'content-type':'text/event-stream','connection':'close'});
   if(fixture.toolRead&&calls===1){
    // Select an actual declared native read-capable tool; the command reads
    // only the tiny synthetic file in the container's temporary source root.
    const tool=body.tools.find(t=>t.type==='function'&&t.name==='exec_command');assert.ok(tool,'Native exec_command must be exposed with structured output');
    const item={id:'fixture-read-item',type:'function_call',call_id:'fixture-read-call',name:tool.name,arguments:JSON.stringify({cmd:'cat '+path.join(source,'src/query.ts'),max_output_tokens:200}),status:'completed'};
    const send=v=>res.write('event: '+v.type+'\ndata: '+JSON.stringify(v)+'\n\n');
    send({type:'response.created',response:{id:'fixture-read-response',status:'in_progress',output:[]}});
    send({type:'response.output_item.added',output_index:0,item:{...item,status:'in_progress',arguments:''}});
    send({type:'response.function_call_arguments.delta',item_id:item.id,output_index:0,delta:item.arguments});
    send({type:'response.function_call_arguments.done',item_id:item.id,output_index:0,arguments:item.arguments});
    send({type:'response.output_item.done',output_index:0,item});
    send({type:'response.completed',response:{id:'fixture-read-response',status:'completed',output:[item],usage:{input_tokens:12,output_tokens:8,total_tokens:20}}});res.end();return;
   }
   if(fixture.toolRead)assert.ok(body.input.some(v=>v.type==='function_call_output'&&JSON.stringify(v.output).includes('export const query')),'Actual source file content must return through the native tool round trip');
   const item={id:'fixture-message',type:'message',role:'assistant',status:'completed',content:[{type:'output_text',text,annotations:[]}]};
   const send=v=>res.write('event: '+v.type+'\ndata: '+JSON.stringify(v)+'\n\n');
   send({type:'response.created',response:{id:'fixture-response',status:'in_progress',output:[]}});
   send({type:'response.output_item.added',output_index:0,item:{...item,status:'in_progress',content:[]}});
   send({type:'response.content_part.added',item_id:item.id,output_index:0,content_index:0,part:{type:'output_text',text:'',annotations:[]}});
   send({type:'response.output_text.delta',item_id:item.id,output_index:0,content_index:0,delta:text});
   send({type:'response.output_text.done',item_id:item.id,output_index:0,content_index:0,text});
   send({type:'response.content_part.done',item_id:item.id,output_index:0,content_index:0,part:item.content[0]});
   send({type:'response.output_item.done',output_index:0,item});
   send({type:'response.completed',response:{id:'fixture-response',status:'completed',output:[item],usage:{input_tokens:12,output_tokens:8,total_tokens:20}}});res.end();
  });
  await new Promise(resolve=>upstream.listen(0,'127.0.0.1',resolve));
  const task={taskId,executionAttempt:1,resultSchemaVersion:1,progressSchemaVersion:2,eventSchemaVersion:2,sourcePath:source,outputPath:output,provider:'custom',connectionType:'openai_compatible',credentialEnv:'CUSTOM_API_KEY',wireApi:'responses',baseUrl:'http://127.0.0.1:'+upstream.address().port+'/v1',model:'fixture-native-validation',scanMode:'standard',timeoutMinutes:1,sdkOperation:{schemaVersion:1,execution:{taskId:String(taskId),attempt:1},kind:'validate',origin:{taskId:'17',attempt:1},findingId:'1',finding:{title:'Fixtures / Mock SQL example',evidence:['src/query.ts:1-2'],reproduced:false},acknowledgeUnknownCost:true}};
  task.resolvedModel={providerId:'custom',connectionType:'openai_compatible',model:task.model,wireApi:'responses',baseUrl:task.baseUrl,credentialEnv:'CUSTOM_API_KEY',configurationRevision:'fixture-validation-replay'};
  process.env.CODEX_SECURITY_STATE_DIR=path.join(output,'runtime/sdk');process.env.CODEX_HOME=path.join(output,'runtime/sdk/codex-home');
  const restore=await enterSDKWorkingDirectory(output),timer=setTimeout(()=>controller.abort(new Error('Fixture native replay timed out')),45000);
  try{
   const result=await runSDKOperation(task,controller.signal);assert.equal(result.status,fixture.status,JSON.stringify(result.error));assert.equal(calls,fixture.toolRead?2:1,'Only an explicit tool round trip may add a fixture turn; never retry incomplete reports');const savedMode=JSON.parse(await readFile(path.join(output,'logs/validation-output-mode.json'),'utf8'));assert.equal(savedMode.mode,fixture.mode);
   if(fixture.invalid){assert.equal(result.sdkOperation.validation,null);assert.ok(result.error);assert.equal(result.findings.length,0);assert.equal(result.sdkOperation.cost.state,'unknown');proofs.push({name:fixture.name,mode:fixture.mode,status:result.status,requestCount:calls,strictValidation:true});continue;}
   assert.equal(result.sdkOperation.validation.report,fixture.report);assert.equal(result.sdkOperation.validation.disposition,fixture.disposition);assert.equal(result.sdkOperation.cost.state,'unknown');assert.equal(result.sdkOperation.cost.estimatedUsd,null);assert.equal(result.findings.length,0);assert.equal(result.engine.version,'0.5.0');
   if(fixture.status==='partial'){assert.equal(result.error.code,'incomplete');assert.equal(result.sdkOperation.error.code,'incomplete');assert.match(result.error.message,/复核报告输出不完整/);}else assert.equal(result.sdkOperation.error,null);
   const native=JSON.parse(await readFile(path.join(output,'raw/codex-security/sdk-operation-native.json'),'utf8'));assert.equal(native.report,fixture.report);assert.equal(native.disposition,fixture.disposition);
   const normalized=JSON.parse(await readFile(path.join(output,'normalized/result.json'),'utf8'));assert.equal(normalized.status,fixture.status);
   const journal=await readFile(path.join(output,'logs/validation-transport.jsonl'),'utf8');assert.doesNotMatch(journal,/fixture-selected-not-real|Bearer|SELECT|Static Finding|用户输入/);const receipt=journal.trim().split('\n').map(v=>JSON.parse(v));assert.equal(receipt.at(-1).summary.state,'complete');assert.equal(receipt.at(-1).summary.received,calls);const finalReceipt=receipt.at(-2);assert.equal(finalReceipt.response.terminalText.sha256,createHash('sha256').update(text).digest('hex'));assert.equal(finalReceipt.response.deltaText.sha256,finalReceipt.response.terminalText.sha256);if(fixture.mode==='native_schema'){assert.equal(receipt[0].request.validationContract.reportRequirement,'nonempty_string_only');assert.equal(receipt[0].request.validationContract.reportDescriptionPresent,false);}else assert.equal(receipt[0].request.format,'json_object');
   proofs.push({name:fixture.name,mode:fixture.mode,status:result.status,disposition:native.disposition,reportSha256:createHash('sha256').update(native.report).digest('hex'),unchanged:true,requestCount:calls,toolRoundTrip:fixture.toolRead===true,requests,transportReceipt:true,costState:'unknown'});
  }finally{clearTimeout(timer);upstream.closeAllConnections();await new Promise(resolve=>upstream.close(resolve));restore();}
 }
 console.log(JSON.stringify({status:'passed',fixtureOnly:true,sdk:VERSION,proofs,realModelCalls:0,realSDKLogins:0,protocol:'responses',isolation:hostLoopback?'temporary-home-fake-credentials-loopback-only':'docker-network-none',evidenceBoundary:'Retained SDK answer strings in reconstructed local HTTP envelopes; original provider HTTP payloads were not retained.'}));
}finally{await rm(scratch,{recursive:true,force:true});}
