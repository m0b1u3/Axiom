// Synthetic SDK boundary for input conformance. No model or network requests.
import {writeFile,mkdir,readFile,rm,appendFile} from 'node:fs/promises';
import {spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
import path from 'node:path';
import {DatabaseSync} from 'node:sqlite';
export const VERSION='fixture-input-sdk';
export class CodexSecurityError extends Error {}
export class AuthenticationRequiredError extends CodexSecurityError {}
export class IncompleteScanError extends CodexSecurityError {}
export class ScanInterruptedError extends CodexSecurityError {}
export class ScanCostLimitExceededError extends ScanInterruptedError {}
export class CodexSecurity {
  constructor(config){this.config=config;}
  async run(source,options){
    this.output=path.dirname(path.dirname(options.outputDir));
    const file=process.env.AXIOM_INPUT_TEST_CAPTURE;
    if(!file)throw new Error('Input SDK fixture requires a capture path');
    const provider=this.config.codexOverrides?.model_provider;
    const envName=this.config.codexOverrides?.model_providers?.[provider]?.env_key??'OPENAI_API_KEY';
    await mkdir(path.dirname(file),{recursive:true});
    await appendFile(file+'.calls',`${process.pid}\n`);
    await writeFile(file,JSON.stringify({fixtureOnly:true,source,config:this.config,options,
      credentialDigest:createHash('sha256').update(process.env[envName]??'').digest('hex'),
      leakedServerCredentials:['AXIOM_DATABASE_URL','AXIOM_AGENT_TOKEN','AXIOM_MASTER_KEY','AXIOM_JWT_SECRET'].some(name=>process.env[name]!==undefined)},null,2));
    options.onProgress?.({phase:'discovery',filesTotal:2,filesCompleted:2});
    const scenario=process.env.AXIOM_RESULT_TEST_SCENARIO??'rich';
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
    const cost=scenario==='empty'?null:{model:'fixture-reported-model',estimatedUsd:1.125,inputTokens:0,cachedInputTokens:0,cacheWriteInputTokens:3,outputTokens:17};
    if(cost)options.onCost?.(cost);
    if(scenario==='observations'||scenario==='observation-cancel'){
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
      db.exec('CREATE TABLE scans(id TEXT,scan_dir TEXT,status TEXT,phase TEXT,updated_at TEXT); CREATE TABLE scan_progress(scan_id TEXT,scope_file_count INTEGER,phase_items_total INTEGER,phase_items_completed INTEGER,phase_progress_unit TEXT,updated_at TEXT);');
      const stamp=new Date().toISOString();
      db.prepare('INSERT INTO scans VALUES(?,?,?,?,?)').run('fixture-workbench',options.outputDir,'running','fixture_native_phase',stamp);
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
    const result={scanId:'fixture-input',manifest:{scan:{status:scenario==='canceled'?'canceled':'completed',producer:{version:'fixture-plugin-1'}},futureManifest:{retained:true}},
      findings:{findings:['empty','partial-empty'].includes(scenario)?[]:[finding],futureFindings:{retained:true}},coverage:{completeness:scenario==='partial-empty'?'partial':'complete',futureCoverage:{retained:true}},
      cost,turn:{model:'fixture-reported-model',usage:{input_tokens:0,output_tokens:17,cached_input_tokens:0,reasoning_output_tokens:5,total_tokens:17}},repositoryFindings:{fixtureOnly:true},unknownNativeResult:{retained:[null,0,false,'']}};
    await mkdir(options.outputDir,{recursive:true});
    await writeFile(path.join(options.outputDir,'report.md'),'Synthetic SDK report. No model called.\n');
    return {...result,pluginVersion:'fixture-plugin-1',toJSON(){return {...result,fixtureOnly:true}}};
  }
  async close(){
    const scenario=process.env.AXIOM_RESULT_TEST_SCENARIO;
    if(!this.output)return;
    if(scenario==='late-exit')throw new Error('Synthetic late SDK close failure');
    const file=path.join(this.output,'normalized/result.json'),markerFile=path.join(this.output,'normalized/result.commit.json');
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
