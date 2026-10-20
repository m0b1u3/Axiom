// Fixtures / Mock: disposable Server and seven held synthetic SDK calls only.
import assert from 'node:assert/strict';
import {chromium} from 'playwright-core';
import {mkdir,writeFile} from 'node:fs/promises';
import {existsSync} from 'node:fs';
import path from 'node:path';
const base=process.env.AXIOM_TEST_WEB_URL,out=process.env.AXIOM_TEST_DISPATCH_REPORT_DIR;
if(!base||!out||!['127.0.0.1','localhost','[::1]'].includes(new URL(base).hostname)||['5173','5174','8080',''].includes(new URL(base).port))throw new Error('Independent disposable dispatch fixture required');
await mkdir(out,{recursive:true});
const executable=process.env.AXIOM_TEST_BROWSER_PATH??(process.platform==='darwin'?'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome':undefined);
const browser=await chromium.launch({headless:true,...(executable&&existsSync(executable)?{executablePath:executable}:{})});
const checks=[],errors=[],external=[],writes=[];
let page,nextOverview;
async function shot(name,target){await target.scrollIntoViewIfNeeded();await page.evaluate(async()=>{document.activeElement?.blur();await document.fonts.ready;await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)))});assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'viewport overflow');await target.screenshot({path:path.join(out,name+'.png'),animations:'disabled'})}
try{
 const context=await browser.newContext({viewport:{width:1440,height:1000},serviceWorkers:'block',reducedMotion:'reduce'});
 await context.route('**/*',async route=>{
  const req=route.request(),url=new URL(req.url());
  if(['http:','https:'].includes(url.protocol)&&url.origin!==new URL(base).origin){external.push(url.origin);return route.abort()}
  if(url.pathname.startsWith('/api/')&&!['GET','HEAD'].includes(req.method())&&url.pathname!=='/api/v1/auth/login'){writes.push(url.pathname);return route.abort()}
  if(url.pathname==='/api/v1/executions/overview'&&nextOverview){const value=nextOverview;nextOverview=null;return route.fulfill(value)}
  return route.continue();
 });
 page=await context.newPage();page.setDefaultTimeout(15000);page.on('pageerror',e=>errors.push(e.message));
 await page.goto(base+'/login');await page.getByLabel('用户名',{exact:true}).fill('input-fixture-admin');await page.getByLabel('密码',{exact:true}).fill('fixture-password');await page.getByRole('button',{name:'登录控制台',exact:true}).click();await page.waitForURL(u=>u.pathname!='/login');
 await page.goto(base+'/settings?section=execution');await page.getByRole('heading',{name:'扫描执行',exact:true}).waitFor();
 const policy=page.getByText('平台不设置全局、模型或项目并发配额。',{exact:true});await policy.waitFor();
 for(const name of ['全局任务并发','每个模型配置的并发','每个项目的并发'])assert.equal(await page.getByLabel(name,{exact:true}).count(),0);
 assert.equal(await page.getByRole('button',{name:'保存并发配置',exact:true}).count(),0);
 assert.equal((await page.locator('.queue-capacity strong').allTextContents())[0],'7');
 const snapshot=await page.evaluate(async()=>{const r=await fetch('/api/v1/executions/overview',{headers:{Authorization:'Bearer '+localStorage.getItem('axiom_token')}});return(await r.json()).data});
 assert.equal(snapshot.dispatchMode,'unbounded');assert.equal(snapshot.effectiveCapacity,null);assert.equal(snapshot.active,7);assert.equal(snapshot.agents[0].dispatchMode,'unbounded');assert.equal(snapshot.agents[0].slots,0);
 checks.push('seven-real-agent-executions-with-retired-quota-controls-removed','zero-slot-unbounded-presence-is-online-without-fake-capacity');
 await shot('dispatch-desktop',page.locator('.execution-settings'));await page.setViewportSize({width:390,height:844});await shot('dispatch-mobile',page.locator('.execution-settings'));await page.setViewportSize({width:1440,height:1000});
 nextOverview={status:503,json:{message:'Fixtures / Mock 执行状态读取失败'}};await page.getByRole('button',{name:'重新加载',exact:true}).click();await page.getByRole('alert').filter({hasText:'执行状态读取失败'}).waitFor();await page.getByRole('status').filter({hasText:'上次成功的执行快照'}).waitFor();assert.equal((await page.locator('.queue-capacity strong').allTextContents())[0],'7');await page.getByRole('button',{name:'重新加载',exact:true}).click();await policy.waitFor();checks.push('read-failure-retains-last-observed-state-and-retry-recovers');
 const oldSnapshot=structuredClone(snapshot);delete oldSnapshot.dispatchMode;nextOverview={json:{data:oldSnapshot}};await page.getByRole('button',{name:'重新加载',exact:true}).click();await page.getByRole('alert').filter({hasText:'未声明无配额调度能力'}).waitFor();assert.equal(await policy.count(),0);checks.push('old-server-is-not-misrepresented-as-unbounded');
 nextOverview={json:{data:oldSnapshot}};await page.goto(base+'/settings?section=diagnostics');await page.getByText('当前 Server 尚未确认无配额调度，请同步更新配套组件；以下保留旧版配置记录。',{exact:true}).waitFor();assert.equal(await page.getByText('这些记录仅供历史追溯；平台并发配额已停用，不再参与当前调度。',{exact:true}).count(),0);await page.goto(base+'/settings?section=execution');await policy.waitFor();checks.push('legacy-history-never-claims-retirement-with-an-old-server');
 nextOverview={json:{data:{...snapshot,agents:[],onlineAgents:0,readyAgents:0}}};await page.getByRole('button',{name:'重新加载',exact:true}).click();await page.getByRole('status').filter({hasText:'当前没有在线执行机'}).waitFor();checks.push('offline-nodes-have-domain-specific-status');
 nextOverview={json:{data:{...snapshot,agents:snapshot.agents.map(a=>({...a,dispatchMode:'bounded',slots:2}))}}};await page.getByRole('button',{name:'重新加载',exact:true}).click();await page.getByRole('status').filter({hasText:'旧版固定并发'}).waitFor();checks.push('legacy-agent-upgrade-requirement-is-visible');
 await page.goto(base+'/settings?section=diagnostics');await page.getByRole('heading',{name:'旧版并发配置记录',exact:true}).waitFor();await page.getByText('这些记录仅供历史追溯；平台并发配额已停用，不再参与当前调度。',{exact:true}).waitFor();await page.getByText('全局 1 · 模型 1 · 项目 1',{exact:true}).waitFor();await page.getByText('按需执行',{exact:true}).waitFor();
 const history=page.locator('section.diagnostic-section').filter({has:page.getByRole('heading',{name:'旧版并发配置记录',exact:true})});await shot('dispatch-history-desktop',history);await page.setViewportSize({width:390,height:844});await shot('dispatch-history-mobile',history);checks.push('legacy-config-history-is-readable-and-explicitly-inactive-desktop-mobile');
 assert.deepEqual(errors,[]);assert.deepEqual(external,[]);assert.deepEqual(writes,[]);
 const report={status:'passed',fixtureOnly:true,checks,errors,external,writes,realAudits:0,realPlanningModelCalls:0,realSDKLogins:0,externalPublications:0,businessWrites:0};await writeFile(path.join(out,'browser.json'),JSON.stringify(report,null,2)+'\n');process.stdout.write(JSON.stringify(report)+'\n');
}finally{await browser.close()}
