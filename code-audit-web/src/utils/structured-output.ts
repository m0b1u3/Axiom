import type {StructuredDiagnosticCatalog,StructuredDiagnosticPage,StructuredDiagnosticRecord,StructuredDiagnosticProfile} from '../contracts';
const object=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v);
const digest=(v:unknown):v is string=>typeof v==='string'&&/^[a-f0-9]{64}$/.test(v);
const profiles=['native_schema','described_schema','json_object'];
const scenarios=['title_only','complete','invalid_json','wrong_disposition','missing_report','interrupted','upstream_error'];
const exact=(v:Record<string,unknown>,keys:string[])=>Object.keys(v).sort().join(',')===[...keys].sort().join(',');
export const profileLabels:Record<StructuredDiagnosticProfile,string>={native_schema:'SDK 原生 Schema',described_schema:'补充字段说明的 Schema',json_object:'JSON 模式'};
export function structuredCatalogView(raw:unknown):{catalog:StructuredDiagnosticCatalog;catalogSha256:string} {
 if(!object(raw)||!digest(raw.catalogSha256)||!object(raw.catalog))throw new Error('固定诊断模板合同无效，请同步更新 Web 与 Server');
 const c=raw.catalog;
 if(c.schemaVersion!==1||c.sdkVersion!=='0.5.0'||c.catalogId!=='fixed-validation-v1'||!object(c.fixture)||c.fixture.fileCount!==3||!Array.isArray(c.fixture.files)||c.fixture.files.length!==3||!digest(c.fixture.sourceSha256)||!Array.isArray(c.requests)||![3,6].includes(c.requests.length)||!Array.isArray(c.scenarios)||c.scenarios.length!==7||!Array.isArray(c.references))throw new Error('固定诊断模板不兼容，请同步更新 Web 与 Server');
 for(const protocol of c.requests.length===6?['responses','chat']:['responses'])for(const profile of profiles){
  const matches=c.requests.filter(p=>object(p)&&p.profile===profile&&p.protocol===protocol);
  if(matches.length!==1)throw new Error('诊断请求对照不完整');
  const p=matches[0];if(!object(p)||typeof p.bodyJson!=='string'||!digest(p.bodySha256)||!digest(p.formatSha256)||!digest(p.promptSha256))throw new Error('诊断请求预览无效');
  try{const body=JSON.parse(p.bodyJson);if(body.model!=='$MODEL'||body.stream!==true)throw new Error();}catch{throw new Error('诊断请求不是固定模拟模板');}
 }
 for(const id of scenarios){const found=c.scenarios.filter(v=>object(v)&&v.id===id);if(found.length!==1||!object(found[0])||typeof found[0].label!=='string'||typeof found[0].responseJson!=='string'||!digest(found[0].responseSha256))throw new Error('固定回放场景不完整');}
 if(c.references.some(v=>!object(v)||typeof v.url!=='string'||!['developers.openai.com','docs.z.ai'].includes(new URL(v.url).hostname)||!v.url.startsWith('https://')))throw new Error('诊断文档引用无效');
 return raw as unknown as {catalog:StructuredDiagnosticCatalog;catalogSha256:string};
}
export function structuredRecordView(raw:unknown):StructuredDiagnosticRecord {
 if(!object(raw)||raw.schemaVersion!==1||raw.sdkVersion!=='0.5.0'||raw.mode!=='offline'||raw.upstreamContacted!==false||raw.credentialLoaded!==false||raw.model!=='not_loaded'||raw.providerCompatibility!=='not_tested'||raw.inputTokens!==0||raw.outputTokens!==0||raw.estimatedUsd!==0||typeof raw.id!=='string'||!/^[a-f0-9]{32}$/.test(raw.id)||typeof raw.createdAt!=='string'||!Number.isFinite(Date.parse(raw.createdAt))||!digest(raw.catalogSha256)||!digest(raw.sourceSha256)||!['responses','chat'].includes(String(raw.protocol))||!Array.isArray(raw.results)||raw.results.length!==3)throw new Error('脱敏诊断记录合同无效，未将其解释为上游兼容结果');
 if(!scenarios.includes(String(raw.scenario))||!exact(raw,['schemaVersion','id','createdAt','sdkVersion','mode','catalogSha256','sourceSha256','protocol','scenario','upstreamContacted','providerCompatibility','model','inputTokens','outputTokens','estimatedUsd','credentialLoaded','results']))throw new Error('诊断记录包含未知或不安全字段');
 const results=raw.results as unknown[];
 for(const value of results){if(!object(value)||!exact(value,['profile','requestSha256','formatSha256','responseSha256','transport','httpStatus','jsonValid','schemaValid','bodyState','disposition','errorCode'])||!digest(value.requestSha256)||!digest(value.formatSha256)||!digest(value.responseSha256)||!['completed','interrupted','http_error'].includes(String(value.transport))||!Number.isInteger(value.httpStatus)||(value.httpStatus as number)<100||(value.httpStatus as number)>599||![true,false,null].includes(value.jsonValid as boolean|null)||![true,false,null].includes(value.schemaValid as boolean|null)||!['body_present','title_only','empty','not_evaluated'].includes(String(value.bodyState))||(value.disposition!==null&&!['reportable','suppressed','not_applicable','deferred'].includes(String(value.disposition)))||(value.errorCode!==null&&!['response_json_invalid','response_schema_invalid','report_body_incomplete','transport_incomplete','upstream_http_error'].includes(String(value.errorCode))))throw new Error('诊断检查结果包含未知内容');}

 if(profiles.some(p=>results.filter((v:unknown)=>object(v)&&v.profile===p).length!==1))throw new Error('脱敏诊断对照结果不完整');
 return raw as unknown as StructuredDiagnosticRecord;
}
export function structuredPageView(raw:unknown):StructuredDiagnosticPage {
 if(!object(raw)||raw.schemaVersion!==1||!Array.isArray(raw.items)||!Number.isSafeInteger(raw.total)||(raw.total as number)<0||!Number.isSafeInteger(raw.page)||(raw.page as number)<1||!Number.isSafeInteger(raw.pageSize)||(raw.pageSize as number)<1||(raw.pageSize as number)>50||raw.items.length>(raw.pageSize as number))throw new Error('诊断记录分页合同无效');
 raw.items.forEach(structuredRecordView);return raw as unknown as StructuredDiagnosticPage;
}
