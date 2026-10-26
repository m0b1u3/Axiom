<script setup lang="ts">
import {computed,onMounted,ref,watch} from 'vue';
import {api} from '../api';
import type {StructuredDiagnosticCatalog,StructuredDiagnosticRecord,StructuredDiagnosticProfile,StructuredDiagnosticScenario,StructuredDiagnosticPage} from '../contracts';
import {profileLabels} from '../utils/structured-output';
import {absoluteTime} from '../utils/time';
import {useAuth} from '../composables/auth';
import PageHeader from './PageHeader.vue';
import Button from './ui/Button.vue';
import Select from './ui/Select.vue';
import DiagnosticSnapshot from './DiagnosticSnapshot.vue';
const {canAdmin}=useAuth();
const catalog=ref<StructuredDiagnosticCatalog|null>(null),catalogHash=ref(''),catalogLoading=ref(false),catalogError=ref('');
const protocol='responses' as const;
const scenario=ref<StructuredDiagnosticScenario>('title_only'),profile=ref<StructuredDiagnosticProfile>('native_schema');
const downloadFeedback=ref('');
const running=ref(false),runError=ref(''),record=ref<StructuredDiagnosticRecord|null>(null),history=ref<StructuredDiagnosticPage|null>(null),historyLoading=ref(false),historyError=ref('');
const scenarios=computed(()=>catalog.value?.scenarios.map(s=>({value:s.id,label:s.label}))??[]);
const requests=computed(()=>catalog.value?.requests.filter(r=>r.protocol===protocol)??[]);
const preview=computed(()=>requests.value.find(r=>r.profile===profile.value));
const replay=computed(()=>catalog.value?.scenarios.find(r=>r.id===scenario.value));
const requestBody=computed(()=>preview.value?JSON.parse(preview.value.bodyJson):null);
const scenarioName=(id:string)=>catalog.value?.scenarios.find(s=>s.id===id)?.label??id;
const bodyLabel={body_present:'有正文（内容未核实）',title_only:'报告结构不完整',empty:'无正文',not_evaluated:'未评估'};
const codeLabel={response_json_invalid:'JSON 无法解析',response_schema_invalid:'字段不符合原生 Schema',report_body_incomplete:'报告结构不完整，未识别到独立正文',transport_incomplete:'流未完成',upstream_http_error:'模拟上游 HTTP 错误'};
const currentRecord=computed(()=>record.value?.catalogSha256===catalogHash.value);
function downloadRequest(){if(!preview.value)return;try{const url=URL.createObjectURL(new Blob([preview.value.bodyJson],{type:'application/json;charset=utf-8'}));try{const a=document.createElement('a');a.href=url;a.download='structured-'+protocol+'-'+profile.value+'-exact.json';a.click();downloadFeedback.value='已下载原始请求正文，字节与请求哈希对应。';}finally{setTimeout(()=>URL.revokeObjectURL(url),0);}}catch{downloadFeedback.value='原始请求下载失败，可展开请求正文选取文本。';}}
async function loadCatalog(){if(catalogLoading.value)return;catalogLoading.value=true;catalogError.value='';try{const v=await api.structuredDiagnosticCatalog();catalog.value=v.catalog;catalogHash.value=v.catalogSha256;runError.value='';downloadFeedback.value='';}catch(e){catalogError.value=(e as Error).message;}finally{catalogLoading.value=false;}}
let historyRequest=0;
async function loadHistory(page=1){if(!canAdmin.value)return;const version=++historyRequest;historyLoading.value=true;historyError.value='';try{const v=await api.structuredDiagnosticRecords(page);if(version===historyRequest)history.value=v;}catch(e){if(version===historyRequest)historyError.value=(e as Error).message;}finally{if(version===historyRequest)historyLoading.value=false;}}
async function run(){if(running.value||!catalog.value||catalogLoading.value||catalogError.value||!canAdmin.value)return;running.value=true;runError.value='';try{const intent={schemaVersion:1 as const,mode:'offline' as const,catalogSha256:catalogHash.value,protocol:protocol,scenario:scenario.value};const v=await api.runStructuredDiagnostic(intent);if(v.catalogSha256!==intent.catalogSha256||v.protocol!==intent.protocol||v.scenario!==intent.scenario)throw new Error('诊断记录与本次预览不匹配');record.value=v;await loadHistory();}catch(e){runError.value=(e as Error).message+'。没有调用真实模型；请先刷新诊断记录确认是否已保存，再决定是否重新提交。';}finally{running.value=false;}}
watch(profile,()=>{downloadFeedback.value='';});
watch(canAdmin,v=>{if(v)void loadHistory();else{historyRequest++;history.value=null;record.value=null;historyLoading.value=false;}});
onMounted(()=>{void loadCatalog();if(canAdmin.value)void loadHistory();});
</script>

<template>
 <div class="structured-diagnostic">
  <RouterLink class="back-link" to="/settings?section=diagnostics">返回高级诊断</RouterLink>
  <PageHeader title="结构化输出对照诊断" description="用固定小样例检查请求格式、回放结果与报告正文，保留可追溯的脱敏记录。"/>
  <p class="boundary">离线模式 · 不发送上游请求，不加载模型凭据。这里的回放通过不代表模型或网关兼容，也不会生成正式漏洞。</p>
  <p v-if="catalogError" role="alert">固定诊断模板读取失败：{{catalogError}}</p>
  <div v-if="!catalog" class="load-state"><p v-if="catalogLoading" role="status">正在读取固定小样例与请求模板…</p><Button v-else variant="outline" size="sm" @click="loadCatalog">重新加载模板</Button></div>
  <template v-if="catalog">
   <p v-if="catalogError" role="status">保留上次成功的模板，仅供查看；重新加载后才能运行对照。</p>
   <section aria-labelledby="diagnostic-input-title">
    <div class="section-heading"><h2 id="diagnostic-input-title">固定输入与对照方式</h2><Button variant="outline" size="sm" :loading="catalogLoading" :disabled="running" @click="loadCatalog">重新加载模板</Button></div>
    <dl class="metadata"><dt>证据样例</dt><dd>{{catalog.fixture.label}} · {{catalog.fixture.fileCount}} 个文件 · {{catalog.fixture.totalBytes}} B</dd><dt>SDK 基线</dt><dd>{{catalog.sdkVersion}} · 原生 Schema 已绑定哈希</dd><dt>执行边界</dt><dd>只处理内置样例；源码已内联，无工具、账户、模型或业务任务访问。</dd></dl>
    <div class="input-row"><span>请求协议：Responses</span><label>固定回放场景<Select :model-value="scenario" :options="scenarios" aria-label="固定回放场景" :disabled="running" @update:model-value="scenario=$event as StructuredDiagnosticScenario"/></label><Button size="sm" :loading="running" :disabled="!canAdmin||catalogLoading||!!catalogError" @click="run">运行离线对照</Button></div>
    <p v-if="!canAdmin" class="muted">可查看固定请求预览；保存与读取诊断记录需要管理员权限。</p>
    <p class="muted">三份请求共用同一提示词和源码，仅改变输出格式。JSON 模式只约束 JSON 语法，返回字段仍按原生 Schema 在本机检查。不会自动切换协议或重试。</p>
    <p class="muted">固定预览仅使用 Responses。实际模型接口的结构化输出能力仍须独立验收。</p>
    <div class="references"><a v-for="reference in catalog.references" :key="reference.url" :href="reference.url" target="_blank" rel="noreferrer">{{reference.label}}</a></div>
    <p v-if="runError" role="alert">{{runError}}</p>
   </section>
   <section aria-labelledby="request-comparison-title">
    <h2 id="request-comparison-title">请求对照与预览</h2>
    <div class="table-scroll" tabindex="0" role="region" aria-label="请求格式对照，可横向滚动"><table aria-label="请求格式对照"><thead><tr><th>方案</th><th>格式</th><th>strict</th><th>字段说明</th><th>操作</th></tr></thead><tbody><tr v-for="item in requests" :key="item.profile" :class="{selected:item.profile===profile}"><td>{{profileLabels[item.profile]}}</td><td class="mono">{{item.profile==='json_object'?'json_object':'json_schema'}}</td><td>{{item.profile==='json_object'?'不适用':'true'}}</td><td>{{item.profile==='described_schema'?'增加结论与报告说明':item.profile==='json_object'?'字段约定写在提示词':'无字段说明'}}</td><td><Button variant="ghost" size="sm" :aria-label="'查看'+profileLabels[item.profile]+'请求'" :aria-pressed="profile===item.profile" @click="profile=item.profile">查看请求</Button></td></tr></tbody></table></div>
    <template v-if="preview"><Button variant="outline" size="sm" @click="downloadRequest">下载原始请求正文</Button><p role="status">{{downloadFeedback}}</p><dl class="metadata"><dt>预览目标</dt><dd class="mono">POST {{preview.path}} · model=$MODEL（占位符）</dd><dt>认证</dt><dd>未加载，不生成 Authorization；不会读取模型配置或 SDK 身份。</dd><dt>请求 SHA-256</dt><dd class="mono">{{preview.bodySha256}}</dd><dt>格式 SHA-256</dt><dd class="mono">{{preview.formatSha256}}</dd><dt>提示词 SHA-256</dt><dd class="mono">{{preview.promptSha256}}</dd></dl><DiagnosticSnapshot :title="profileLabels[profile]+'请求正文'" :value="requestBody" :filename="'structured-'+protocol+'-'+profile+'-request.json'"/><p class="muted">这是固定输入的格式对照模板，使用 SDK 原生评估提示和 Schema；不冒充完整 SDK 会话。未设置输出 Token 上限、verbosity 或 stop。展开后的 JSON 为便于阅读的格式化版本，哈希对应模板的紧凑 UTF-8 请求正文。</p></template>
    <DiagnosticSnapshot title="固定小样例与版本" :value="{catalogSha256:catalogHash,fixture:catalog.fixture,sdkPromptSha256:catalog.sdkPromptSha256}" filename="structured-diagnostic-fixture.json"/>
    <DiagnosticSnapshot v-if="replay" title="固定回放响应" :value="replay" filename="structured-diagnostic-replay.json"/>
   </section>
   <section aria-labelledby="offline-result-title" aria-live="polite">
    <h2 id="offline-result-title">离线对照结果</h2>
    <p v-if="!record" class="muted">尚未运行离线对照。可先查看请求，再用同一个固定响应检查三种格式的边界。</p>
    <template v-else>
     <p class="result-boundary">{{record.protocol==='responses'?'Responses':'Chat Completions'}} · {{scenarioName(record.scenario)}} · {{absoluteTime(record.createdAt)}}<br/>上游兼容性：未验证 · 模型与凭据：未加载 · 模型用量：0 Token · 离线费用：$0</p>
     <p v-if="!currentRecord" role="status">该记录属于历史模板版本，保留原结果与哈希；当前请求预览不代表其原请求。</p>
     <div class="table-scroll" tabindex="0" role="region" aria-label="离线对照结果，可横向滚动"><table aria-label="离线对照结果"><thead><tr><th>方案</th><th>JSON 语法</th><th>原生字段检查</th><th>报告正文</th><th>诊断</th></tr></thead><tbody><tr v-for="item in record.results" :key="item.profile"><td>{{profileLabels[item.profile]}}</td><td>{{item.jsonValid===null?'未评估':item.jsonValid?'合法':'无效'}}</td><td>{{item.schemaValid===null?'未评估':item.schemaValid?'符合':'不符合'}}</td><td>{{bodyLabel[item.bodyState]}}</td><td :class="{warning:!!item.errorCode}">{{item.errorCode?codeLabel[item.errorCode]:'结构检查通过；结论未核实'}}</td></tr></tbody></table></div>
     <p class="muted">同一个预置响应会依次交给三个方案。补充说明不会使标题在 Schema 层面失效；有正文也不代表可报告、已验证或已复现。本次未询问模型，无法据此比较生成质量。</p>
     <DiagnosticSnapshot title="脱敏诊断记录" :value="record" :filename="'structured-diagnostic-'+record.id+'.json'"/>
    </template>
   </section>
   <section v-if="canAdmin" aria-labelledby="diagnostic-history-title">
    <div class="section-heading"><h2 id="diagnostic-history-title">诊断记录</h2><Button variant="outline" size="sm" :loading="historyLoading" :disabled="running" @click="loadHistory(history?.page??1)">刷新诊断记录</Button></div>
    <p class="muted">独立保存固定模板标识、哈希、时间和检查结论；不记录凭据、上游地址、真实提示词或真实响应，不改写历史扫描。默认保留记录。</p>
    <p v-if="historyError" role="alert">诊断记录读取失败：{{historyError}}</p><p v-if="historyError&&history" role="status">保留上次成功的诊断记录，当前存储状态尚未确认。</p>
    <p v-if="historyLoading&&!history" role="status">正在读取诊断记录…</p><p v-if="history&&!history.total" class="muted">尚无脱敏诊断记录。</p>
    <div v-if="history?.items.length" class="table-scroll" tabindex="0" role="region" aria-label="诊断记录，可横向滚动"><table aria-label="诊断记录"><thead><tr><th>记录时间</th><th>协议</th><th>固定场景</th><th>模式</th><th>操作</th></tr></thead><tbody><tr v-for="item in history.items" :key="item.id" :class="{selected:item.id===record?.id}"><td>{{absoluteTime(item.createdAt)}}</td><td>{{item.protocol==='responses'?'Responses':'Chat'}}</td><td>{{scenarioName(item.scenario)}}</td><td>离线 · 上游未验证</td><td><Button variant="ghost" size="sm" :aria-label="'查看诊断记录 '+item.id" :aria-pressed="item.id===record?.id" @click="record=item">查看记录</Button></td></tr></tbody></table></div>
    <div v-if="history&&history.total" class="pagination"><span>共 {{history.total}} 条 · 第 {{history.page}} / {{Math.max(1,Math.ceil(history.total/history.pageSize))}} 页</span><Button variant="outline" size="sm" :disabled="historyLoading||running||history.page===1" @click="loadHistory(history.page-1)">上一页</Button><Button variant="outline" size="sm" :disabled="historyLoading||running||history.page*history.pageSize>=history.total" @click="loadHistory(history.page+1)">下一页</Button></div>
   </section>
  </template>
 </div>
</template>

<style scoped>
.structured-diagnostic{min-width:0;max-width:var(--content-max-width)}.back-link,a{font-size:var(--font-size-sm);color:var(--accent-primary)}.back-link{display:inline-block;margin-bottom:var(--space-3)}.boundary,.result-boundary{margin:0 0 var(--space-4);padding:var(--space-3);border:1px solid var(--border-default);background:var(--surface-secondary);font-size:var(--font-size-sm);line-height:var(--line-height-body)}section{min-width:0;margin-top:var(--space-6);padding-top:var(--space-4);border-top:1px solid var(--border-default)}h2{margin:0 0 var(--space-3);font-size:var(--font-size-lg);font-weight:var(--font-weight-semibold)}.section-heading{display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:var(--space-2);margin-bottom:var(--space-3)}.section-heading h2{margin:0}.metadata{display:grid;grid-template-columns:128px minmax(0,1fr);gap:var(--space-2) var(--space-3);font-size:var(--font-size-sm);line-height:var(--line-height-body);margin:var(--space-3) 0 var(--space-4)}dt{color:var(--text-secondary)}dd{margin:0;min-width:0;overflow-wrap:anywhere}.mono{font-family:var(--font-mono);font-size:var(--font-size-xs)}.input-row{display:flex;align-items:end;flex-wrap:wrap;gap:var(--space-3);margin:var(--space-4) 0}.input-row label{display:flex;flex-direction:column;gap:var(--space-2);font-size:var(--font-size-sm);min-width:0;flex:1}.input-row label:first-child{flex:.6}.muted,.references,p{font-size:var(--font-size-sm);line-height:var(--line-height-body)}.muted{color:var(--text-secondary);max-width:75ch;margin:var(--space-3) 0}.references{display:flex;flex-wrap:wrap;gap:var(--space-2) var(--space-4);margin-bottom:var(--space-3)}.table-scroll{max-width:100%;overflow:auto;overscroll-behavior:contain;border:1px solid var(--border-default)}table{width:100%;border-collapse:collapse;white-space:nowrap;font-size:var(--font-size-sm)}th,td{text-align:left;padding:var(--space-2) var(--space-3);border-bottom:1px solid var(--border-subtle);height:var(--height-table-row);box-sizing:border-box}th{background:var(--surface-secondary);font-weight:var(--font-weight-medium);color:var(--text-secondary)}tbody tr:last-child td{border-bottom:0}.selected{background:var(--surface-selected)}tr:hover{background:var(--surface-hover)}[role=alert]{color:var(--state-danger);overflow-wrap:anywhere}.warning{color:var(--state-warning)}.pagination{display:flex;align-items:center;justify-content:end;flex-wrap:wrap;gap:var(--space-2);margin-top:var(--space-3);font-size:var(--font-size-xs)}.pagination span{margin-right:auto;color:var(--text-secondary)}.load-state{margin-top:var(--space-4)}
@media(max-width:600px){.input-row{align-items:stretch}.input-row label,.input-row label:first-child{flex-basis:100%}.input-row>button{align-self:start}.metadata{grid-template-columns:96px minmax(0,1fr)}.references{flex-direction:column}.boundary,.result-boundary{padding:var(--space-2)}}
</style>
