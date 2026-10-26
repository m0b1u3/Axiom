// Real disposable Go Server + Web. No Agent, SDK, provider or business data.
import assert from 'node:assert/strict';
import {chromium} from 'playwright-core';
import {mkdir,readFile,writeFile} from 'node:fs/promises';
import {existsSync} from 'node:fs';
import path from 'node:path';
import {createHash} from 'node:crypto';
const base=process.env.AXIOM_TEST_STRUCTURED_URL;
if(!base||!['127.0.0.1','localhost','[::1]'].includes(new URL(base).hostname))throw new Error('Requires the disposable loopback Go Server fixture');
const output=process.env.AXIOM_TEST_STRUCTURED_REPORT_DIR||path.resolve('../output/playwright/structured-output');await mkdir(output,{recursive:true});
const executable=process.env.AXIOM_TEST_BROWSER_PATH||(process.platform==='darwin'?'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome':undefined);
const browser=await chromium.launch({headless:true,...(executable&&existsSync(executable)?{executablePath:executable}:{})});
const failures=[],external=[],writes=[],screenshots=[],checks=[];
const origin=new URL(base).origin,endpoint='/api/v1/diagnostics/structured-output';
let context,page;
const sha=s=>createHash('sha256').update(s).digest('hex');
async function ctx(){const c=await browser.newContext({viewport:{width:1440,height:1000},acceptDownloads:true,reducedMotion:'reduce',serviceWorkers:'block'});await c.route('**/*',async route=>{const r=route.request(),u=new URL(r.url());if(u.origin!==origin){external.push(u.origin);return route.abort();}if(u.pathname.startsWith('/api/')&&r.method()!=='GET'){writes.push({method:r.method(),path:u.pathname});if(!['/api/v1/auth/login',endpoint+'/records'].includes(u.pathname))return route.abort();}return route.continue();});return c;}
const button=(name,root=page)=>root.getByRole('button',{name,exact:true});
const reader=(name)=>page.locator('details.diagnostic-snapshot').filter({has:page.locator('.snapshot-title',{hasText:new RegExp('^'+name+'$')})});
const choose=async(name,value)=>{await page.getByRole('combobox',{name,exact:true}).click();await page.getByRole('option',{name:value,exact:true}).click();};
async function login(c,user,password){const p=await c.newPage();p.on('pageerror',e=>failures.push(e.message));await p.goto(base+'/login');await p.getByLabel('用户名',{exact:true}).fill(user);await p.getByLabel('密码',{exact:true}).fill(password);await button('登录控制台',p).click();await p.waitForURL(u=>u.pathname!='/login');return p;}
async function theme(dark){
 const current=await page.evaluate(()=>document.documentElement.classList.contains('dark'));
 if(current!==dark){const narrow=page.viewportSize().width<=760;if(narrow)await button('切换导航').click();await button(dark?'切换深色模式':'切换浅色模式').click();if(narrow)await page.keyboard.press('Escape');}
 await page.waitForFunction(d=>document.documentElement.classList.contains('dark')===d&&document.documentElement.style.colorScheme===(d?'dark':'light'),dark);
 await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
}
const noOverflow=async()=>assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth&&scrollX===0),'document horizontal overflow');
const waitHistory=async total=>{await page.getByText(new RegExp('^共 '+total+' 条 ·')).waitFor();};
async function run(double=false){const response=page.waitForResponse(r=>new URL(r.url()).pathname===endpoint+'/records'&&r.request().method()==='POST');if(double)await button('运行离线对照').evaluate(e=>{e.click();e.click();});else await button('运行离线对照').click();const r=await response;assert.equal(r.status(),201);const value=(await r.json()).data;await page.getByRole('table',{name:'离线对照结果',exact:true}).waitFor();await button('运行离线对照').waitFor({state:'visible'});await page.waitForFunction(()=>!Array.from(document.querySelectorAll('button')).find(e=>e.textContent.trim()==='运行离线对照')?.disabled);return value;}
try{
 context=await ctx();page=await login(context,'admin','fixture-admin-password');page.setDefaultTimeout(10000);
 await page.goto(base+'/settings?section=diagnostics');await page.getByRole('link',{name:'打开结构化输出对照诊断',exact:true}).click();await page.getByRole('heading',{name:'结构化输出对照诊断',exact:true}).waitFor();await page.getByRole('table',{name:'请求格式对照',exact:true}).waitFor();await page.getByText('尚无脱敏诊断记录。',{exact:true}).waitFor();
 assert.equal(writes.filter(w=>w.path===endpoint+'/records').length,0);await noOverflow();checks.push('real-server-Web-entry-fixed-input-no-model-or-write-on-load');
 const token=await page.evaluate(()=>localStorage.getItem('axiom_token'));
 const catalog=(await (await context.request.get(base+endpoint,{headers:{Authorization:'Bearer '+token}})).json()).data;
 for(const [profile,label] of [['native_schema','SDK 原生 Schema'],['described_schema','补充字段说明的 Schema'],['json_object','JSON 模式']]){
  await button('查看'+label+'请求').click();assert.equal(await button('查看'+label+'请求').getAttribute('aria-pressed'),'true');
  const r=reader(label+'请求正文');await r.locator('summary').focus();await page.keyboard.press('Enter');await r.locator('pre').waitFor();const source=JSON.parse(await r.locator('pre').textContent());const expected=catalog.catalog.requests.find(p=>p.protocol==='responses'&&p.profile===profile);assert.deepEqual(source,JSON.parse(expected.bodyJson));
  const downloadPromise=page.waitForEvent('download');await button('下载原始请求正文').click();const download=await downloadPromise;const raw=await readFile(await download.path(),'utf8');assert.equal(sha(raw),expected.bodySha256);assert.equal(raw,expected.bodyJson);
  await r.locator('summary').focus();await page.keyboard.press('Space');
 }
 checks.push('all-three-exact-request-previews-SDK-schema-descriptions-JSON-mode-keyboard-and-byte-hash-download');
 let first;for(let round=0;round<2;round++){
  assert.equal(await page.getByRole('combobox',{name:'诊断请求协议',exact:true}).count(),0);await page.getByText('请求协议：Responses',{exact:true}).waitFor();
  for(const s of catalog.catalog.scenarios){await choose('固定回放场景',s.label);const before=writes.filter(w=>w.path===endpoint+'/records').length;const value=await run(!first);if(!first)first=value;assert.equal(writes.filter(w=>w.path===endpoint+'/records').length,before+1,'duplicate request');assert.equal(value.mode,'offline');assert.equal(value.upstreamContacted,false);assert.equal(value.credentialLoaded,false);assert.equal(value.providerCompatibility,'not_tested');assert.equal(value.estimatedUsd,0);assert.equal(value.results.length,3);const table=page.getByRole('table',{name:'离线对照结果',exact:true});
   if(s.id==='title_only'){assert.equal(await table.getByText('报告结构不完整',{exact:true}).count(),3);assert.ok(value.results.every(v=>v.schemaValid===true&&v.errorCode==='report_body_incomplete'));}
   if(s.id==='complete'){assert.equal(await table.getByText('有正文（内容未核实）',{exact:true}).count(),3);assert.ok(value.results.every(v=>v.schemaValid===true&&v.errorCode===null));}
   if(s.id==='interrupted')assert.ok(value.results.every(v=>v.jsonValid===null&&v.errorCode==='transport_incomplete'));
  }
 }
 await waitHistory(14);checks.push('fourteen-real-Server-offline-comparisons-no-double-submit-candidate-and-provider-semantics');
 await button('下一页').click();await page.getByText('共 14 条 · 第 2 / 2 页',{exact:true}).waitFor();await button('查看诊断记录 '+first.id).click();assert.equal(await page.getByRole('table',{name:'离线对照结果',exact:true}).getByText('报告结构不完整',{exact:true}).count(),3);checks.push('full-record-pagination-and-original-hash-linked-history');
 const recordReader=reader('脱敏诊断记录');await recordReader.locator('summary').click();const recordText=await recordReader.locator('pre').textContent();assert.deepEqual(JSON.parse(recordText),first);assert.ok(!recordText.includes('bodyJson')&&!recordText.includes('responseJson')&&!recordText.includes('fixtures-only-encrypted-value'));
 await context.addInitScript(()=>{Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:async t=>{window.__diagnosticClipboard=t}}});});
 await page.evaluate(()=>{Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:async t=>{window.__diagnosticClipboard=t}}});});await recordReader.getByRole('button',{name:'复制脱敏诊断记录',exact:true}).click();assert.deepEqual(JSON.parse(await page.evaluate(()=>window.__diagnosticClipboard)),first);await recordReader.locator('summary').click();checks.push('redacted-receipt-retains-metadata-no-raw-body-or-secret-copy');
 // Fail a save, then lose a response after durable publication. Neither is retried.
 const recordURL=base+endpoint+'/records';let rejectPost=true;
 const reject=async route=>{if(route.request().method()==='POST'&&rejectPost){rejectPost=false;return route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({message:'Fixtures / Mock receipt unavailable'})});}return route.continue();};
 await page.route(recordURL+'*',reject);await button('运行离线对照').click();await page.getByRole('alert').filter({hasText:'receipt unavailable'}).waitFor();await page.unroute(recordURL+'*',reject);assert.equal(await page.getByRole('table',{name:'离线对照结果',exact:true}).getByText('报告结构不完整',{exact:true}).count(),3);await button('刷新诊断记录').click();await waitHistory(14);
 let lost=true;const drop=async route=>{if(route.request().method()==='POST'&&lost){lost=false;await route.fetch();return route.abort('failed');}return route.continue();};await page.route(recordURL+'*',drop);await button('运行离线对照').click();await page.getByRole('alert').filter({hasText:'没有调用真实模型'}).waitFor();await page.unroute(recordURL+'*',drop);await button('刷新诊断记录').click();await waitHistory(15);checks.push('save-failure-and-lost-response-retain-evidence-no-auto-repeat-durable-history-recovery');
 // Failed refresh retains data; a stale preview is rejected by the real Server.
 const historyFailure=async route=>route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({message:'Fixtures / Mock read unavailable'})});await page.route(recordURL+'*',historyFailure);await button('刷新诊断记录').click();await page.getByRole('status').filter({hasText:'保留上次成功的诊断记录'}).waitFor();await page.unroute(recordURL+'*',historyFailure);await button('刷新诊断记录').click();await waitHistory(15);
 const stale=async route=>{const response=await route.fetch();const payload=await response.json();payload.data.catalogSha256='a'.repeat(64);return route.fulfill({response,json:payload});};await page.route(base+endpoint,stale);await button('重新加载模板').click();await page.getByRole('status').filter({hasText:'该记录属于历史模板版本'}).waitFor();await button('运行离线对照').click();await page.getByRole('alert').filter({hasText:'固定诊断模板已更新'}).waitFor();await page.unroute(base+endpoint,stale);await button('重新加载模板').click();await page.getByRole('status').filter({hasText:'该记录属于历史模板版本'}).waitFor({state:'hidden'});
 const broken=async route=>route.fulfill({status:200,contentType:'application/json',body:JSON.stringify({data:{catalog:{schemaVersion:99},catalogSha256:'a'.repeat(64)}})});await page.route(base+endpoint,broken);await button('重新加载模板').click();await page.getByRole('alert').filter({hasText:'固定诊断模板'}).waitFor();assert.ok(await button('运行离线对照').isDisabled());await page.unroute(base+endpoint,broken);await button('重新加载模板').click();await page.getByRole('alert').filter({hasText:'固定诊断模板读取失败'}).waitFor({state:'hidden'});checks.push('read-failure-retains-history-stale-preview-409-and-unknown-contract-blocks-run');
 // One visual batch: desktop and narrow layout, light/dark, inspectors and results.
 await choose('固定回放场景',catalog.catalog.scenarios.find(s=>s.id==='title_only').label);assert.equal(await page.getByRole('alert').count(),0);await button('查看SDK 原生 Schema请求').click();await reader('SDK 原生 Schema请求正文').locator('summary').click();await page.evaluate(()=>document.fonts.ready);
 for(const [name,width,dark]of [['desktop-light',1440,false],['desktop-dark',1440,true],['mobile-light',390,false],['mobile-dark',390,true]]){
  await page.setViewportSize({width,height:1000});await theme(dark);await noOverflow();await page.evaluate(()=>scrollTo(0,0));await page.screenshot({path:path.join(output,name+'.png'),fullPage:true,animations:'disabled'});await page.screenshot({path:path.join(output,name+'-viewport.png'),animations:'disabled'});screenshots.push(name);
 }
 checks.push('desktop-mobile-light-dark-contained-tables-raw-reader-and-keyboard-focus');
 const viewer=await ctx();const v=await login(viewer,'viewer','fixture-password');await v.goto(base+'/settings/structured-output');await v.getByRole('table',{name:'请求格式对照',exact:true}).waitFor();assert.ok(await button('运行离线对照',v).isDisabled());assert.equal(await v.getByRole('heading',{name:'诊断记录',exact:true}).count(),0);const viewerToken=await v.evaluate(()=>localStorage.getItem('axiom_token'));const denied=await viewer.request.post(recordURL,{headers:{Authorization:'Bearer '+viewerToken},data:{schemaVersion:1,mode:'offline',catalogSha256:catalog.catalogSha256,protocol:'responses',scenario:'title_only'}});assert.equal(denied.status(),403);await viewer.close();checks.push('viewer-preview-only-and-real-server-write-denial');
 await page.setViewportSize({width:1440,height:1000});await theme(false);
 await page.goto(base+'/settings?section=models');await button('添加模型').click();
 const modelDialog=page.getByRole('dialog',{name:'添加模型',exact:true});
 await modelDialog.getByText('Responses',{exact:true}).waitFor();await modelDialog.getByText('模型接口须支持 Responses。',{exact:true}).waitFor();
 assert.equal(await modelDialog.getByText('Chat Completions',{exact:true}).count(),0);
 await modelDialog.locator('button.provider').filter({hasText:/自定义/}).click();await modelDialog.getByText('Responses',{exact:true}).waitFor();await noOverflow();
 await page.screenshot({path:path.join(output,'model-responses-only.png'),animations:'disabled'});screenshots.push('model-responses-only');checks.push('model-dialog-Responses-only-without-protocol-picker-or-model-requests');
 assert.deepEqual(external,[]);assert.deepEqual(failures,[]);assert.ok(writes.every(w=>w.method==='POST'&&['/api/v1/auth/login',endpoint+'/records'].includes(w.path)));
 const report={status:'passed',fixtureOnly:true,realModelCalls:0,SDKLogins:0,externalRequests:external,checks,mutations:writes,screenshots};await writeFile(path.join(output,'browser.json'),JSON.stringify(report,null,2));console.log(JSON.stringify({status:'passed',checks:checks.length,screenshots:screenshots.length,realModelCalls:0}));
}catch(error){await writeFile(path.join(output,'failure.json'),JSON.stringify({message:error.message,checks,failures,external,writes},null,2));throw error;}finally{await browser.close();}
