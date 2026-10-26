// Test-only SDK execution. Native inspection and the production bridge remain real.
import assert from 'node:assert/strict';
import {readFile,writeFile,mkdir,appendFile} from 'node:fs/promises';
import {DatabaseSync} from 'node:sqlite';
import path from 'node:path';
export * from './input-sdk.mjs';
export const VERSION='0.5.0';
export class CodexSecurity {
 constructor(config){this.config=config}
 async run(source,options){
  const state=process.env.CODEX_SECURITY_STATE_DIR;assert.ok(state);const db=new DatabaseSync(path.join(state,'workbench.sqlite3'),{readOnly:true});let scan;try{scan=db.prepare('SELECT * FROM scans WHERE id=?').get(options.resumeScanId)}finally{db.close()}
  assert.ok(scan,'Fixture must reuse the original Scan ID');assert.equal(options.mode,'deep');assert.equal(options.outputDir,scan.scan_dir);assert.equal(options.archiveExisting,false);
  const file=process.env.AXIOM_INPUT_TEST_CAPTURE;assert.ok(file);await mkdir(path.dirname(file),{recursive:true});
  const captured={fixtureOnly:true,source,scanId:scan.id,threadId:scan.continuation_thread_id,options:{outputDir:options.outputDir,resumeScanId:options.resumeScanId,archiveExisting:options.archiveExisting,maxCostUsd:options.maxCostUsd,mode:options.mode},credentialPresent:!!process.env.OPENAI_API_KEY,leakedServerCredentials:['AXIOM_DATABASE_URL','AXIOM_TEST_DATABASE_URL','AXIOM_AGENT_TOKEN','AXIOM_MASTER_KEY','AXIOM_JWT_SECRET'].some(n=>process.env[n]!==undefined)};
  await writeFile(file,JSON.stringify(captured));await appendFile(file+'.calls',JSON.stringify(captured)+'\n');
  options.onSessionEvent?.({threadId:scan.continuation_thread_id,parentThreadId:null,event:{type:'event_msg',timestamp:new Date().toISOString(),payload:{type:'task_started'}}});
  options.onProgress?.({phase:'validation',filesCompleted:1,filesTotal:1});options.onDeepProgress?.({completed:1,active:0,maximum:1});
  options.onUsage?.({input_tokens:1100,cached_input_tokens:550,cache_write_input_tokens:0,output_tokens:75,reasoning_output_tokens:0,total_tokens:1175});
  const cost={model:this.config.codexOverrides.model,estimatedUsd:1.25,inputTokens:1100,cachedInputTokens:550,cacheWriteInputTokens:0,outputTokens:75};options.onCost?.(cost);
  const evidence=JSON.parse(await readFile(new URL('../../fixtures/v1/evidence050-source.json',import.meta.url),'utf8')).raw;
  evidence.fingerprints={primary:'fixture-deep-resume-finding'};evidence.locations=[{path:'fixture.ts',startLine:3,role:'sink'}];
  const result={scanId:scan.id,pluginVersion:'fixture-plugin-1',manifest:{scan:{id:scan.id,status:'completed',producer:{version:'fixture-plugin-1'},target:{kind:'directory_snapshot',targetId:'fixture-deep-target'}}},findings:{findings:[evidence]},coverage:{documentType:'codex-security.coverage',schemaVersion:'1.0',surfaces:[],deferred:[],resolvedDeferred:[],openQuestions:[]},cost,turn:{model:this.config.codexOverrides.model,usage:{input_tokens:100,cached_input_tokens:50,output_tokens:25}},repositoryFindings:[],reportPath:path.join(options.outputDir,'report.md'),sarifPath:path.join(options.outputDir,'results.sarif'),fixtureOnly:true};
  await writeFile(result.reportPath,'# Fixtures / Mock Deep continuation\n');await writeFile(result.sarifPath,JSON.stringify({version:'2.1.0',runs:[],fixtureOnly:true}));
  return {...result,toJSON(){return result}};
 }
 async close(){}
}
