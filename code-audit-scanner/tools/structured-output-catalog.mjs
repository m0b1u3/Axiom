import {readFile,writeFile} from 'node:fs/promises';
import {createHash} from 'node:crypto';
import {pathToFileURL,fileURLToPath} from 'node:url';
const root=fileURLToPath(new URL('../../',import.meta.url)).replace(/\/$/,'');
const sha=s=>createHash('sha256').update(s).digest('hex');
const {standaloneValidationPrompt}=await import(pathToFileURL(root+'/code-audit-scanner/node_modules/@openai/codex-security/dist/standalone-validation-prompt.js'));
const sdkPrompt=await standaloneValidationPrompt(root+'/code-audit-scanner/node_modules/@openai/codex-security/_bundled_plugin');
const native=JSON.parse(await readFile(root+'/contracts/fixtures/sdk050-standalone-validation-format.json','utf8'));
const files=await Promise.all(['README.md','src/query.ts','src/routes.ts'].map(async path=>{const content=await readFile(root+'/test-fixtures/interim-review-mini/'+path,'utf8');return {path,content,sha256:sha(content)}}));
const prompt=sdkPrompt+'\n\nFixtures / Mock — fixed offline structured-output diagnostic. This is not a repository scan or a real finding assessment. Use only the three inline source files below. No tools, network access, accounts or business data are available. State the difference between static inference and runtime reproduction.\n\n'+files.map(f=>'FILE '+f.path+'\n'+f.content).join('\n')+'\n\nAssess the fixed candidate: lookup() passes user input to findUser(), which concatenates a SQL string passed to db.execute(). There is no implementation of db.execute() and no running database. Return one JSON object containing disposition and report. The report must be a complete Markdown assessment with rationale, the source-to-sink evidence, uncertainty, and the absence of runtime reproduction. A heading alone is incomplete. The JSON structure for every comparison variant is: '+JSON.stringify(native.format.schema);
const messages=[{role:'system',content:'Return only one JSON object. Assess only the supplied Fixtures / Mock evidence. Never claim runtime reproduction.'},{role:'user',content:prompt}];
const described=structuredClone(native.format);
described.schema.properties.disposition.description='Static assessment disposition based only on supplied evidence. reportable does not mean reproduced. Use deferred when evidence is insufficient.';
described.schema.properties.report.description='Complete Markdown assessment, not only a title. Include rationale, source-to-sink evidence with actual file references, counterevidence and uncertainty, and whether reproduction was performed. Do not invent source files or runtime observations.';
const requests=[];
for(const protocol of ['responses'])for(const [profile,format] of [['native_schema',native.format],['described_schema',described],['json_object',{type:'json_object'}]]){
 const body={model:'$MODEL',input:messages.map(m=>({type:'message',role:m.role,content:[{type:'input_text',text:m.content}]})),text:{format},stream:true};
 const bodyJson=JSON.stringify(body);
 requests.push({profile,protocol,path:'/responses',bodyJson,bodySha256:sha(bodyJson),formatSha256:sha(JSON.stringify(format)),promptSha256:sha(prompt)});
}
const body=(disposition,report)=>JSON.stringify({disposition,report});
const title='## SQL Injection in `lookup()` (`a036597803b7bd848456a8ddfe314614ae901dbc6baad40631c55f7b77a44ae0` — 用户输入直接拼接进 SQL 语句)';
const complete='## Static SQL construction\n\nFixtures / Mock: src/routes.ts passes input to findUser() in src/query.ts; name is concatenated into SQL before db.execute(). This is a static inference. The database driver is absent, so exploitability and runtime impact remain unknown. No reproduction was performed.';
const scenarios=[
 {id:'title_only',label:'保留的标题响应（#11）',transport:'completed',httpStatus:200,responseJson:body('reportable',title)},
 {id:'complete',label:'合成的完整短报告',transport:'completed',httpStatus:200,responseJson:body('reportable',complete)},
 {id:'invalid_json',label:'无效 JSON',transport:'completed',httpStatus:200,responseJson:'{"disposition":'},
 {id:'wrong_disposition',label:'不支持的结论值',transport:'completed',httpStatus:200,responseJson:body('verified',complete)},
 {id:'missing_report',label:'缺少报告字段',transport:'completed',httpStatus:200,responseJson:'{"disposition":"reportable"}'},
 {id:'interrupted',label:'流中断且未完成',transport:'interrupted',httpStatus:200,responseJson:'{"disposition":"reportable","report":"## '},
 {id:'upstream_error',label:'合成 HTTP 502',transport:'http_error',httpStatus:502,responseJson:'{"error":"Fixtures / Mock upstream unavailable"}'},
].map(s=>({...s,responseSha256:sha(s.responseJson)}));
const catalog={schemaVersion:1,sdkVersion:'0.5.0',catalogId:'fixed-validation-v1',fixture:{label:'Fixtures / Mock · 三文件静态证据',sourceSha256:sha(JSON.stringify(files)),fileCount:files.length,totalBytes:files.reduce((n,f)=>n+Buffer.byteLength(f.content),0),files},sdkPromptSha256:sha(sdkPrompt),nativeFormatSha256:native.formatSha256,requests,scenarios,references:[{label:'OpenAI Structured Outputs / JSON mode',url:'https://developers.openai.com/api/docs/guides/structured-outputs?api-mode=responses',protocols:['responses']} ]};
const target=root+'/contracts/data/structured-output-catalog.json',raw=JSON.stringify(catalog,null,2)+'\n';
if(process.argv.includes('--write'))await writeFile(target,raw);
else if(await readFile(target,'utf8')!==raw)throw new Error('Fixed diagnostic fixture drift; review SDK prompt and regenerate explicitly with --write');
console.log(JSON.stringify({requests:requests.length,scenarios:scenarios.length,files:files.length,bytes:catalog.fixture.totalBytes,promptSha256:sha(prompt),nativeFormatSha256:native.formatSha256}));
