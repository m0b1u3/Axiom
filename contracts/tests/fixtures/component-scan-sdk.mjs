// Fixtures / Mock only. The real 0.1.25 component scheduler runs; inference
// boundaries are replaced and never contact an account, model or publisher.
import {appendFile,mkdir,readFile,readdir,writeFile} from 'node:fs/promises';
import path from 'node:path';
const requireFixture=()=>{if(process.env.AXIOM_COMPONENT_SCAN_FIXTURE!=='1'||!process.env.AXIOM_INPUT_TEST_CAPTURE)throw new Error('Component SDK fixture is not enabled')};
export async function runComponentFixture(source,options,config){
 requireFixture();const index=Number(path.basename(path.dirname(source))),scenario=process.env.AXIOM_COMPONENT_SCAN_SCENARIO??'normal';
 const capture=process.env.AXIOM_INPUT_TEST_CAPTURE;await mkdir(path.dirname(capture),{recursive:true});
 const evidence={fixtureOnly:true,kind:'component_scan',index,source,options:{mode:options.mode,target:options.target,maxCostUsd:options.maxCostUsd,outputDir:options.outputDir,scanPrompt:options.scanPrompt,postScanPrompt:options.postScanPrompt,knowledgeBasePaths:options.knowledgeBasePaths},knowledge:await Promise.all((options.knowledgeBasePaths??[]).map(async file=>({path:file,content:await readFile(file,'utf8')}))),config,credentialPresent:!!process.env.OPENAI_API_KEY,leakedServerCredentials:['AXIOM_DATABASE_URL','AXIOM_AGENT_TOKEN','AXIOM_MASTER_KEY','AXIOM_JWT_SECRET'].some(n=>process.env[n]!==undefined)};
 await appendFile(capture+'.component-calls',JSON.stringify(evidence)+'\n');await writeFile(capture,JSON.stringify(evidence));
 options.onProgress?.({phase:'discovery',filesCompleted:0,filesTotal:1});
 options.onActivity?.({id:'fixture-inspect-'+index,kind:'tool',status:'running',description:'Fixtures / Mock：读取组件源码',paths:options.target,worker:1});
 options.onSessionEvent?.({threadId:'fixture-component-thread-'+index,parentThreadId:null,worker:1,event:{type:'event_msg',payload:{type:'task_started'}}});
 if(scenario==='controlled'){await new Promise((resolve,reject)=>{const deadline=Date.now()+25000;const timer=setInterval(async()=>{try{await readFile(capture+'.release');clearInterval(timer);resolve()}catch{if(Date.now()>deadline){clearInterval(timer);reject(new Error('Fixture controlled hold expired'))}}},100);options.signal?.addEventListener('abort',()=>{clearInterval(timer);reject(options.signal.reason)},{once:true})});}
 const cost=scenario==='unknown'&&index===0?null:{estimatedUsd:0.125,inputTokens:10,outputTokens:5};if(cost)options.onCost?.(cost);
 await new Promise((resolve,reject)=>{const timer=setTimeout(resolve,scenario==='hold'?30000:80);options.signal?.addEventListener('abort',()=>{clearTimeout(timer);reject(new Error('Fixtures / Mock：组件已取消'))},{once:true});if(options.signal?.aborted){clearTimeout(timer);reject(options.signal.reason)}});
 if((scenario==='partial'&&index===1)||(scenario==='partial-two'&&index>0))throw new Error('Fixtures / Mock：组件执行合成故障');
 if(scenario==='mutate'&&index===0)await writeFile(path.join(source,'fixture-mutation.txt'),'Fixture only');
 async function first(directory){for(const entry of (await readdir(directory,{withFileTypes:true})).sort((a,b)=>a.name.localeCompare(b.name))){if(entry.name==='.git')continue;const name=path.join(directory,entry.name);if(entry.isFile())return name;if(entry.isDirectory()){const f=await first(name);if(f)return f}}}
 const target=options.target[0],file=path.join(source,target);let relative;
 try{await readFile(file);relative=target}catch{relative=path.relative(source,await first(file)).split(path.sep).join('/')}
 const code=(await readFile(path.join(source,relative),'utf8')).split('\n')[0];
 const finding={findingId:'fixture-component-finding-'+index,occurrenceId:'fixture-component-occurrence-'+index,fingerprints:{primary:'fixture-shared-fingerprint'},title:'Fixtures / Mock 组件证据 '+index,summary:'合成发现，用于验证版本和组件身份，不是真实漏洞。',severity:{level:'high'},locations:[{path:relative,startLine:1,endLine:1,role:'sink'}],codeEvidence:[{id:'E-'+index,path:relative,startLine:1,endLine:1,code,explanation:'Fixtures / Mock：冻结源码引用'}],validation:{status:'hypothesis',fixtureOnly:true},unknownNativeField:{retained:true}};
 const result={manifest:{scan:{id:'fixture-component-scan-'+index,status:'completed'}},findings:{findings:[finding]},coverage:{completeness:'complete'},cost,fixtureOnly:true};
 await mkdir(options.outputDir,{recursive:true});await writeFile(path.join(options.outputDir,'report.md'),'# Fixtures / Mock\nNo real audit.\n');
 return {...result,toJSON:()=>result};
}
export async function matchScanFindings(input,options){
 requireFixture();await appendFile(process.env.AXIOM_INPUT_TEST_CAPTURE+'.matching',JSON.stringify({fixtureOnly:true,before:input.before.map(f=>f.occurrenceId),after:input.after.map(f=>f.occurrenceId)})+'\n');
 if(process.env.AXIOM_COMPONENT_SCAN_SCENARIO==='matching-failure')throw new Error('Fixtures / Mock：跨组件匹配合成故障');
 const before=input.before[0],after=input.after[0];
 if(!before||!after)return {matches:[],uncertain:[],related:[]};
 if(input.before.length===1)return {matches:[],uncertain:[{beforeOccurrenceId:before.occurrenceId,afterOccurrenceId:after.occurrenceId,reason:'Fixtures / Mock：不足以确认同一根因，保留独立发现'}],related:[]};
 return {matches:[{beforeOccurrenceIds:[before.occurrenceId],afterOccurrenceIds:[after.occurrenceId],confidence:'high',reason:'Fixtures / Mock：合成高置信匹配，仍然是推断'}],uncertain:[],related:[]};
}
