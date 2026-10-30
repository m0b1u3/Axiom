// Synthetic SDK boundary. No model requests; budget scenarios use loopback only.
import {writeFile,mkdir,readFile,rm,appendFile} from 'node:fs/promises';
import {spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
import path from 'node:path';
import {runComponentFixture} from './component-scan-sdk.mjs';
import {runComponentScans as actualComponentScans} from '../../../code-audit-scanner/node_modules/@openai/codex-security/dist/component-scan.js';
import {DatabaseSync} from 'node:sqlite';
export const VERSION='fixture-input-sdk';
// Used only by explicit, network-isolated deployment/stability acceptance.
export const pricedAcceptanceUsage=(input=1125000)=>({input_tokens:input,cached_input_tokens:0,cache_write_input_tokens:0,output_tokens:0,reasoning_output_tokens:0,total_tokens:input});
// The public estimate function is pure. Resolve from this fixture directory so
// the runtime replacement loader does not replace the real estimator itself.
export {DiffTarget,estimateScanCost,normalizeComponentPlan,bundledPluginRoot,loadContract} from '../../../code-audit-scanner/node_modules/@openai/codex-security/dist/index.js';
export async function runComponentScans(options){if(process.env.AXIOM_COMPONENT_SCAN_FIXTURE!=='1')throw new Error('Component fixtures not enabled');await writeFile(process.env.AXIOM_INPUT_TEST_CAPTURE+'.component-options',JSON.stringify({fixtureOnly:true,workers:options.workers,componentCount:options.components.length}));return actualComponentScans(options)}
export async function planComponents(source,options){
  const file=process.env.AXIOM_INPUT_TEST_CAPTURE;
  if(!file)throw new Error('Component fixture requires capture path');
  await mkdir(path.dirname(file),{recursive:true});
  await writeFile(file,JSON.stringify({fixtureOnly:true,kind:'planComponents',source,config:options.config,credentialPresent:!!process.env.OPENAI_API_KEY,leakedServerCredentials:['AXIOM_DATABASE_URL','AXIOM_AGENT_TOKEN','AXIOM_MASTER_KEY','AXIOM_JWT_SECRET'].some(name=>process.env[name]!==undefined)}));
  const scenario=process.env.AXIOM_COMPONENT_TEST_SCENARIO;
  if(scenario==='hold')await new Promise((resolve,reject)=>{const timer=setTimeout(resolve,30000);options.signal?.addEventListener('abort',()=>{clearTimeout(timer);reject(new Error('Fixture component planning canceled'));},{once:true});});
  if(scenario==='failure')throw new Error('Fixture planning failed; Bearer fixture-private-planning-token');
  if(scenario==='mutate')await writeFile(path.join(source,'mutated-by-fixture.txt'),'Fixture only');
  return {components:[{name:'Fixtures / Mock 建议',paths:['.']}]};
}
export class CodexSecurityError extends Error {}
export class AuthenticationRequiredError extends CodexSecurityError {}
export class IncompleteScanError extends CodexSecurityError {}
export class ScanInterruptedError extends CodexSecurityError {}
export class ScanCostLimitExceededError extends ScanInterruptedError {}
export class CodexSecurity {
  constructor(config){this.config=config;}
  async preflight(source,options){
    await this.captureOperation('preflight',source,options);
    if(process.env.AXIOM_OPERATION_TEST_SCENARIO==='preflight-failure')throw new Error('Fixture preflight failed: Bearer fixture-private-operation-token');
    return {repository:source,target:{type:'snapshot',paths:options.target??[]},mode:options.mode,model:this.config.codexOverrides?.model??'fixture-default-model',modelProvider:this.config.codexOverrides?.model_provider,reasoningEffort:'high',authentication:{method:'api_key',source:'OPENAI_API_KEY',verified:false},outputDir:options.outputDir,...(options.knowledgeBasePaths?{knowledgeBasePaths:options.knowledgeBasePaths}:{})};
  }
  async validate(options){
    await this.captureOperation('validate',options.repositoryPath,options);
    const scenario=process.env.AXIOM_OPERATION_TEST_SCENARIO??'reportable';
    if(scenario==='validation-usage-replay'){
      const config=this.config.codexOverrides,base=config?.model_providers?.[config.model_provider]?.base_url;
      if(!base||new URL(base).hostname!=='127.0.0.1')throw new Error('Usage replay requires a loopback-only fixture');
      const {format}=JSON.parse(await readFile(new URL('../../fixtures/sdk050-standalone-validation-format.json',import.meta.url),'utf8'));
      for(let i=0;i<2;i++){
        const reply=await fetch(base+'/responses',{method:'POST',signal:options.signal,body:JSON.stringify({model:config.model,input:[{role:'user',content:'Fixtures / Mock usage replay only'}],stream:true,text:{format}})});
        await reply.text();if(!reply.ok)throw new Error('Fixtures / Mock validation HTTP '+reply.status);
      }
    }
    if(scenario==='validation-hold')await new Promise((resolve,reject)=>{const timer=setTimeout(resolve,15000);options.signal?.addEventListener('abort',()=>{clearTimeout(timer);reject(new ScanInterruptedError('Fixture validation canceled'));},{once:true});});
    if(scenario==='headline-reportable'||scenario==='headline-not-applicable')return {disposition:scenario==='headline-reportable'?'reportable':'not_applicable',report:scenario==='headline-reportable'?'## 静态发现评估：用户输入直接拼接进 SQL 语句（CWE-89）':'## Static Finding Assessment — 用户输入直接拼接进 SQL 语句 (CWE-89)',threadId:'fixture-validation-thread',outputDir:options.outputDir};
    if(scenario==='validation-failure')throw new AuthenticationRequiredError('Fixture account not available; Bearer fixture-private-operation-token');
    return {disposition:['reportable','suppressed','not_applicable','deferred'].includes(scenario)?scenario:'reportable',report:scenario==='validation-large'?'合成原始报告'.repeat(1600000):'Fixtures / Mock：独立复核 '+scenario+(options.finding?.inputSha256?' `src/query.ts:2-4`'+(scenario==='reportable-bad-source'?' and `src/db.ts:5-8`':''):'')+'\n<script>window.operationInjected=true</script>',threadId:'fixture-validation-thread',outputDir:options.outputDir};
  }
  async captureOperation(kind,source,options){
    const file=process.env.AXIOM_INPUT_TEST_CAPTURE;if(!file)throw new Error('Operation fixture requires capture path');
    await mkdir(path.dirname(file),{recursive:true});await writeFile(file,JSON.stringify({fixtureOnly:true,kind,source,options,config:this.config,credentialPresent:!!process.env.OPENAI_API_KEY,leakedServerCredentials:['AXIOM_DATABASE_URL','AXIOM_AGENT_TOKEN','AXIOM_MASTER_KEY','AXIOM_JWT_SECRET'].some(name=>process.env[name]!==undefined)}));
  }
  async run(source,options){
    if(process.env.AXIOM_COMPONENT_SCAN_FIXTURE==='1')return runComponentFixture(source,options,this.config);
    this.output=path.dirname(path.dirname(options.outputDir));
    const state=process.env.CODEX_SECURITY_STATE_DIR;
    if(!state)throw new Error('Fixture SDK requires isolated state');await mkdir(state,{recursive:true});
    const stateDb=new DatabaseSync(path.join(state,'workbench.sqlite3'));stateDb.exec('CREATE TABLE IF NOT EXISTS fixture_parent_state(id TEXT); CREATE TABLE IF NOT EXISTS scans(id TEXT,scan_dir TEXT,status TEXT,phase TEXT,updated_at TEXT,failure_message TEXT,completed_at TEXT); CREATE TABLE IF NOT EXISTS scan_progress(scan_id TEXT,scope_file_count INTEGER,phase_items_total INTEGER,phase_items_completed INTEGER,phase_progress_unit TEXT,updated_at TEXT);');stateDb.close();
    const file=process.env.AXIOM_INPUT_TEST_CAPTURE;
    if(!file)throw new Error('Input SDK fixture requires a capture path');
    const provider=this.config.codexOverrides?.model_provider;
    const providerConfig=this.config.codexOverrides?.model_providers?.[provider];
    const envName=providerConfig?.auth?.args?.[1]??providerConfig?.env_key??'OPENAI_API_KEY';
    await mkdir(path.dirname(file),{recursive:true});
    await appendFile(file+'.calls',`${process.pid}\n`);
    await writeFile(file,JSON.stringify({fixtureOnly:true,source,config:this.config,options,
      knowledge:await Promise.all((options.knowledgeBasePaths??[]).map(async name=>({path:name,content:await readFile(name,'utf8')}))),
      credentialDigest:createHash('sha256').update(process.env[envName]??'').digest('hex'),
      leakedServerCredentials:['AXIOM_DATABASE_URL','AXIOM_AGENT_TOKEN','AXIOM_MASTER_KEY','AXIOM_JWT_SECRET'].some(name=>process.env[name]!==undefined)},null,2));
    for(let i=0;i<73;i++)options.onSessionEvent?.({threadId:'fixture-native-thread',parentThreadId:null,worker:0,event:{type:i%2?'response_item':'future_session_type',payload:{sequence:i,authorization:'Bearer fixture-private-session-token',text:'Fixtures / Mock: full callback event'}}});
    options.onProgress?.({phase:'discovery',filesTotal:2,filesCompleted:2});
    const scenario=process.env.AXIOM_RESULT_TEST_SCENARIO??'rich';
    if(scenario==='token-budget-limit'||scenario==='token-budget-missing'){
      const directory=path.join(options.outputDir,'artifacts/checkpoints');await mkdir(directory,{recursive:true});
      await writeFile(path.join(directory,'fixture-worker.json'),JSON.stringify({checkpoint:'Fixtures / Mock budget interruption',candidates:[{id:'fixture-budget-candidate',title:'Fixtures / Mock partial candidate',severity:'high',summary:'Synthetic partial result retained at budget stop',evidence:['src/query.ts:1-2']}]}));
      const providers=this.config.codexOverrides?.model_providers,base=providers?.[Object.keys(providers)[0]]?.base_url;
      if(!base||!['127.0.0.1','localhost','[::1]'].includes(new URL(base).hostname))throw new Error('Budget fixture requires loopback transport');
      for(let i=0;i<3;i++){
        const response=await fetch(base+'/responses',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({model:this.config.codexOverrides.model,input:'Fixtures / Mock only',text:{format:{type:'json_schema',name:'fixture_report',strict:true,schema:{type:'object',properties:{report:{type:'string'}},required:['report'],additionalProperties:false}}}}),signal:options.signal});
        const text=await response.text();if(!response.ok)throw new Error(text);
      }
      throw new Error('Budget fixture unexpectedly dispatched all turns');
    }
    if(scenario==='interim-failure'||scenario==='interim-timeout'){
      const directory=path.join(options.outputDir,'artifacts/checkpoints');await mkdir(directory,{recursive:true});
      const count=process.env.AXIOM_INTERIM_REVIEW_SOURCE==='1'?3:scenario==='interim-failure'?25:2;
      await writeFile(path.join(directory,'fixture-worker.json'),JSON.stringify({checkpoint:'Fixtures / Mock intermediate evidence',candidates:Array.from({length:count},(_,i)=>({id:'C'+(process.env.AXIOM_INTERIM_REVIEW_SOURCE==='1'&&i===1?0:i),title:(scenario==='interim-timeout'?'Timeout':'Failed')+' fixture candidate '+String(process.env.AXIOM_INTERIM_REVIEW_SOURCE==='1'&&i===1?1:i+1).padStart(2,'0'),severity:'high',status:'verified',summary:'Fixtures / Mock inference <script>window.interimInjected=true</script>',evidence:['src/query.ts:2-4'],api_key:'fixture-interim-credential-DO-NOT-SHOW'}))}));
      options.onSessionEvent?.({threadId:'fixture-interim-worker',parentThreadId:'fixture-main',worker:1,event:{type:'event_msg',payload:{type:'task_complete',last_agent_message:'Fixtures / Mock worker conclusion; not a final result.'}}});
      options.onProgress?.({phase:'discovery',filesTotal:2,filesCompleted:1});
      for(let i=0;i<600&&!(await readFile(file+'.release').then(()=>true,()=>false));i++)await new Promise(r=>setTimeout(r,100));
      throw new Error(scenario==='interim-timeout'?'wall-clock timeout':'Fixtures / Mock interrupted before final SDK report');
    }
    if(scenario==='large-stdout'){
      await new Promise((resolve,reject)=>process.stdout.write('x'.repeat(9*1024*1024)+'\n',error=>error?reject(error):resolve()));
    }
    if(scenario==='reliability-hold'||scenario==='reliability-controlled'){
      // A controlled hold is released only by cancellation/termination. Slow
      // backup refusal must not let a fixture finish and settle its cost first.
      const controlled=scenario==='reliability-controlled'&&await readFile(file+'.hold','utf8').then(()=>true,()=>false);
      const child=spawn(process.execPath,['-e','setInterval(()=>{},1000)'],{stdio:'ignore'});
      await writeFile(file+'.processes',JSON.stringify({fixtureOnly:true,scannerPid:process.pid,childPid:child.pid}));
      try{
        await new Promise((resolve,reject)=>{
          const timer=controlled?null:setTimeout(resolve,15000);
          options.signal?.addEventListener('abort',()=>{clearTimeout(timer);reject(new ScanInterruptedError('Fixture only: execution interrupted'));},{once:true});
        });
      }finally{child.kill('SIGKILL');}
    }
    const sdk050=process.env.AXIOM_SDK050_FIXTURE==='1',pricedAcceptance=process.env.AXIOM_ACCEPTANCE_PRICED_USAGE_FIXTURE==='1';
    if(sdk050){options.onProgress?.({phase:'discovery'});options.onWorkerStatus?.({kind:'dispatch',phase:'discovery',planned:2,started:1});options.onWorkerEvent?.({kind:'observed',worker:1});options.onUsage?.(pricedAcceptance?pricedAcceptanceUsage():{input_tokens:123,cached_input_tokens:20,cache_write_input_tokens:0,output_tokens:0,reasoning_output_tokens:0,total_tokens:123});}
    const cost=(scenario==='empty'||sdk050)?null:{model:'fixture-reported-model',estimatedUsd:1.125,inputTokens:0,cachedInputTokens:0,cacheWriteInputTokens:3,outputTokens:17};
    if(cost){options.onUsage?.(pricedAcceptance?pricedAcceptanceUsage():{input_tokens:0,cached_input_tokens:0,cache_write_input_tokens:0,output_tokens:17,reasoning_output_tokens:0,total_tokens:17});options.onCost?.(cost);}
    if(scenario==='observations'||scenario==='observation-cancel'){
      options.onAuthentication?.({method:'api_key',source:'OPENAI_API_KEY',verified:false});
      options.onTrustedAccessStatus?.('not_granted');
      options.onOutputDirReady?.(options.outputDir);
      options.onScanStarted?.();
      const nextLimit=options.maxCostUsd===undefined?undefined:await options.onBudgetApproaching?.({maxCostUsd:options.maxCostUsd,cost,signal:options.signal});
      if(nextLimit!==undefined)throw new Error('Fixture budget must not be raised');

      options.onProgress?.({phase:'preflight',filesTotal:2,filesCompleted:0});
      options.onWorkerStatus?.({kind:'preflight',delegation:'available',configuredSlots:2});
      options.onSessionEvent?.({threadId:'fixture-main',parentThreadId:null,worker:0,event:{type:'event_msg',payload:{type:'task_started'}}});
      options.onSessionEvent?.({threadId:'fixture-worker-1',parentThreadId:'fixture-main',worker:1,event:{type:'event_msg',payload:{type:'task_started'}}});
      options.onActivity?.({id:'fixture-tool-1',kind:'tool',status:'running',description:'Fixture only: inspecting source; failed is just text',paths:['src/a,b.ts'],worker:1});
      options.onDeepProgress?.({completed:0,active:1,maximum:8});
      options.onWorkerStatus?.({kind:'dispatch',phase:'file_review',planned:2,started:1});
      options.onReconnect?.(1,3,{reason:'network',retryAfterSeconds:0});
      options.onWarning?.('Fixture only: native observer warning');
      options.onSessionEvent?.({threadId:'fixture-worker-1',parentThreadId:'fixture-main',worker:1,event:{type:'event_msg',payload:{type:'task_complete',error:{code:'rate_limited',message:'Fixture only: worker request limited'}}}});
      options.onSessionEvent?.({threadId:'fixture-main',parentThreadId:null,worker:0,event:{type:'event_msg',payload:{type:'task_complete'}}});
      const dir=path.join(this.output,'runtime/sdk');await mkdir(dir,{recursive:true});
      const db=new DatabaseSync(path.join(dir,'workbench.sqlite3'));
      db.exec('CREATE TABLE IF NOT EXISTS scans(id TEXT,scan_dir TEXT,status TEXT,phase TEXT,updated_at TEXT); CREATE TABLE IF NOT EXISTS scan_progress(scan_id TEXT,scope_file_count INTEGER,phase_items_total INTEGER,phase_items_completed INTEGER,phase_progress_unit TEXT,updated_at TEXT);');
      const stamp=new Date().toISOString();
      db.prepare('INSERT INTO scans(id,scan_dir,status,phase,updated_at) VALUES(?,?,?,?,?)').run('fixture-workbench',options.outputDir,'running','fixture_native_phase',stamp);
      db.prepare('INSERT INTO scan_progress VALUES(?,?,?,?,?,?)').run('fixture-workbench',2,3,0,'review_receipts',stamp);db.close();
      await new Promise((resolve,reject)=>{const timer=setTimeout(resolve,scenario==='observation-cancel'?30000:9000);options.signal?.addEventListener('abort',()=>{clearTimeout(timer);reject(new ScanInterruptedError('Fixture only: Web cancellation'));},{once:true});});
    }
    if(scenario==='failed')throw new AuthenticationRequiredError('Synthetic authentication failure, no model called');
    const finding={findingId:'fixture-finding-1',occurrenceId:'fixture-occurrence-1',ruleId:'fixture-rule',fingerprints:{primary:'fixture-result-fingerprint'},
      title:'合成合同发现（非真实漏洞）',summary:'Fixture only: complete result retention.',severity:{level:'high',score:7.5,scoringSystem:'fixture-score'},
      confidence:{level:'medium',rationale:'合成置信理由；未执行独立验证'},taxonomy:{category:'fixture',cwe:['CWE-000']},locations:[{path:'src/a,b.ts',startLine:1,endLine:1,role:'sink'}],
      rootCause:{summary:'合成根因'},attackPath:{summary:'合成攻击路径',entrypoint:'fixture-entry',source:'fixture-source',dataflow:'fixture-flow',sink:'fixture-sink',reachability:'unknown',impact:{rationale:'fixture-impact'},likelihood:{level:'unknown'},steps:['fixture-step'],preconditions:['fixture-precondition']},
      codeEvidence:[{id:'fixture-evidence-1',label:'合成代码证据',path:'src/a,b.ts',startLine:1,endLine:1,language:'typescript',role:'sink',code:'// Synthetic evidence',explanation:'完整证据解释：用于合同验收'}],
      validation:{fixtureOnly:true,status:'hypothesis',observed:false},remediation:'合成修复建议',remediationTests:['fixture-remediation-test'],preventiveControls:['fixture-preventive-control'],
      provenance:{fixtureOnly:true,source:'synthetic-sdk',future:{nested:[0,false,null,'中文']}},extensions:{future:{nested:[0,false,null,'']}},unknownNativeField:{retained:true}};
    if(scenario==='progress-io'){await rm(path.join(this.output,'state/progress.json'),{force:true});await mkdir(path.join(this.output,'state/progress.json'));}
    if(process.env.AXIOM_OPERATION_TEST_FINDING_FIELDS==='1')finding.fixtureCredentials={api_key:'fixture-evidence-only',authorization:'Fixture finding header',password:'fixture-reproduction-field'};
    if(scenario==='evidence-chain'||scenario==='git-evidence'){
      const fixture=JSON.parse(await readFile(new URL('../../fixtures/v1/'+(sdk050?'evidence050-source.json':'evidence-source.json'),import.meta.url),'utf8'));
      for(const key of Object.keys(finding))delete finding[key];
      Object.assign(finding,fixture.raw,{fingerprints:{primary:'fixture:evidence-chain'},locations:[{path:'fixture.ts',startLine:3,role:'sink'}],remediation:'Fixtures / Mock：需要独立核实参数边界。'});
    }
    if(scenario==='git-evidence'){finding.codeEvidence.push({id:'E-base-only',label:'仅存在于 base 的代码',path:'fixture.ts',startLine:2,endLine:2,code:"const query = 'base-only';",explanation:'Fixtures / Mock：不能观察为 HEAD 代码'},{id:'E-deleted',label:'HEAD 已删除文件',path:'deleted.ts',startLine:1,endLine:1,code:'// deleted in HEAD',explanation:'Fixtures / Mock：删除内容只能用作 base 差异上下文'});}
    const result={scanId:'fixture-input',manifest:{scan:{id:'fixture-input',status:scenario==='canceled'?'canceled':'completed',producer:{version:'fixture-plugin-1'}},futureManifest:{retained:true}},
      findings:{findings:['empty','partial-empty'].includes(scenario)?[]:[finding],futureFindings:{retained:true}},coverage:{completeness:scenario==='partial-empty'?'partial':'complete',futureCoverage:{retained:true}},
      cost,turn:{model:'fixture-reported-model',usage:{input_tokens:0,output_tokens:17,cached_input_tokens:0,reasoning_output_tokens:5,total_tokens:17}},repositoryFindings:{fixtureOnly:true},unknownNativeResult:{retained:[null,0,false,'']}};
    if(pricedAcceptance)result.turn.usage=pricedAcceptanceUsage();
    if(sdk050){result.cost={model:'fixture-reported-model',estimatedUsd:1.125,estimatedUsdRange:{min:1.125,max:null,context:'unknown'},inputTokens:123,outputTokens:0};options.onCost?.(result.cost);}
    if(sdk050)result.coverage={documentType:'codex-security.coverage',schemaVersion:'1.0',surfaces:[],deferred:['Fixtures / Mock 未审查项'],resolvedDeferred:['Fixtures / Mock 已完成审查'],openQuestions:['Fixtures / Mock 未决问题'],fixtureOnly:true};
    if(scenario==='evidence-chain'||scenario==='git-evidence'){
      const second=structuredClone(finding);second.findingId='fixture-evidence-2';second.fingerprints.primary='fixture:evidence-chain-other';second.title='Fixtures / Mock：第二条证据链';result.findings.findings.push(second);
    }
    await mkdir(options.outputDir,{recursive:true});
    await writeFile(path.join(options.outputDir,'report.md'),'Synthetic SDK report. No model called.\n');
    if(sdk050){result.reportPath=path.join(options.outputDir,'report.md');result.sarifPath=path.join(options.outputDir,'results.sarif');await writeFile(result.sarifPath,JSON.stringify({version:'2.1.0',runs:[],fixtureOnly:true}));}
    if(scenario.startsWith('native-')){
      const job=JSON.parse(await readFile(path.join(this.output,'runtime/job.json'),'utf8'));
      const scanId=`fixture-native-${job.execution.taskId}-${job.execution.attempt}`;
      result.manifest.scan.id=scanId;result.manifest.scan.target={targetId:'fixture-native-target',kind:'directory_snapshot'};
      result.reportPath=path.join(options.outputDir,'report.md');result.sarifPath=null;
      result.turn={model:scenario==='native-unknown'?'fixture-unpriced-model':'gpt-5.6',usage:{input_tokens:0,cached_input_tokens:0,cache_write_input_tokens:0,output_tokens:scenario==='native-zero'?0:17,reasoning_output_tokens:0}};
      result.repositoryFindings=scenario==='native-zero'?[]:[
        {findingId:'fixture-repository-closed',occurrenceId:'fixture-repository-occurrence-1',scanId:options.parentScanId??scanId,targetId:'fixture-native-target',title:'合成仓库历史：SDK 已关闭',summary:'SDK 历史关联，不自动修改 Axiom 人工状态。',severity:{level:'high'},status:'closed',confirmedInLatestScan:false,knownSince:'2026-10-15T00:00:00Z',knownScanIds:[options.parentScanId??scanId,scanId,'fixture-unresolved-scan'],matchedFindingIds:['fixture-earlier-finding']},
        {findingId:'fixture-repository-open',occurrenceId:'fixture-repository-occurrence-2',scanId,targetId:'fixture-native-target',title:'合成仓库历史：SDK 最近确认',summary:'Fixture only.',severity:{level:'medium'},status:'open',confirmedInLatestScan:true}
      ];
      if(scenario==='native-unknown')delete result.repositoryFindings;
      if(scenario==='native-bad-history')result.repositoryFindings={fixtureOnly:true};
      if(scenario!=='native-no-exports'){
        await writeFile(result.reportPath,'# 合成 SDK 原生报告\n<script>window.__nativeFixtureExecuted=true</script>\n原文必须保持，不是 Axiom HTML 报告。\n');
        await mkdir(path.join(options.outputDir,'exports'),{recursive:true});result.sarifPath=path.join(options.outputDir,'exports/results.sarif');
        await writeFile(result.sarifPath,scenario==='native-bad-sarif'?'{':JSON.stringify({version:'2.1.0',$schema:'https://json.schemastore.org/sarif-2.1.0.json',runs:[{tool:{driver:{name:'Fixture SDK'}},results:[]}]}));
      }else{await rm(result.reportPath);result.reportPath=null;}
      if(scenario==='native-rich'){
        for(let i=0;i<40;i++)options.onWarning?.('合成 SDK 目标变化 '+i,{kind:'target_changed'});
        options.onObserverError?.('onProgress',Object.assign(new Error('合成观察器失败 Bearer fixture-private-diagnostic-token'),{code:'fixture_observer_error'}));
      }
    }
    if(options.target?.kind==='refs')result.manifest.scan.target={targetId:'fixture-git-diff',kind:'git_diff',baseRevision:options.target.base,headRevision:options.target.head,revision:options.target.head};
    if(scenario==='git-wrong-target'&&result.manifest.scan.target)result.manifest.scan.target.headRevision=result.manifest.scan.target.baseRevision;
    return {...result,pluginVersion:'fixture-plugin-1',toJSON(){return {...result,fixtureOnly:true}}};
  }
  async close(){
    const scenario=process.env.AXIOM_RESULT_TEST_SCENARIO;
    if(!this.output)return;
    if(scenario==='late-exit')throw new Error('Synthetic late SDK close failure');
    const file=path.join(this.output,'normalized/result.json'),markerFile=path.join(this.output,'normalized/result.commit.json');
    if(['native-old','native-future','native-historical-estimate'].includes(scenario)){
      const value=JSON.parse(await readFile(file,'utf8')),marker=JSON.parse(await readFile(markerFile,'utf8'));
      if(scenario==='native-old')delete value.extensions.sdkNative;else if(scenario==='native-future')value.extensions.sdkNative.schemaVersion=99;else{const historical=JSON.parse(await readFile(new URL('../../fixtures/v1/sdk050-estimate-range.json',import.meta.url),'utf8'));historical.execution=value.execution;value.extensions.sdkNative.usageEstimate=historical;}
      const bytes=Buffer.from(JSON.stringify(value));marker.sizeBytes=bytes.length;marker.sha256=createHash('sha256').update(bytes).digest('hex');await writeFile(file,bytes);await writeFile(markerFile,JSON.stringify(marker));return;
    }
    if(scenario==='no-commit'){await rm(markerFile,{force:true});return;}
    if(scenario==='tampered'){await writeFile(file,(await readFile(file)).toString()+' ');return;}
    if(['wrong-task','wrong-attempt','bad-json','oversized'].includes(scenario)){
      const result=JSON.parse(await readFile(file,'utf8')),marker=JSON.parse(await readFile(markerFile,'utf8'));
      if(scenario==='wrong-task')result.execution.taskId='999999';
      if(scenario==='wrong-attempt')result.execution.attempt++;
      const bytes=Buffer.from(scenario==='bad-json'?'{':scenario==='oversized'?' '.repeat(33554433):JSON.stringify(result));
      marker.execution=result.execution;marker.sizeBytes=bytes.length;marker.sha256=createHash('sha256').update(bytes).digest('hex');
      await writeFile(file,bytes);await writeFile(markerFile,JSON.stringify(marker));
    }
  }
}
