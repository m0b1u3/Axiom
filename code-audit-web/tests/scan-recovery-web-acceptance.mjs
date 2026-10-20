// Isolated PostgreSQL + production Go Agent/Node Scanner; SDK module is a
// synthetic fixture. Never connect this harness to a business deployment.
import assert from 'node:assert/strict';
import {chromium} from 'playwright-core';
import {existsSync} from 'node:fs';
import {readFile,mkdir,writeFile} from 'node:fs/promises';
import {spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const base=process.env.AXIOM_TEST_WEB_URL,ids=JSON.parse(process.env.AXIOM_TEST_RECOVERY_IDS??'{}');
if(!base||!['127.0.0.1','localhost','[::1]'].includes(new URL(base).hostname))throw new Error('Recovery acceptance requires an isolated loopback server');
const root=fileURLToPath(new URL('../../',import.meta.url)),out=process.env.AXIOM_TEST_RECOVERY_REPORT_DIR??path.join(root,'output/playwright/scan-recovery');
await mkdir(out,{recursive:true});
const sha=v=>createHash('sha256').update(v).digest('hex');
const evidence={fixtureOnly:true,modelCalls:0,realSDKLogins:0,externalPublicationWrites:0,sourceSha256:sha(await readFile(fileURLToPath(import.meta.url))),buildSha256:sha(await readFile(path.join(root,'code-audit-web/dist/index.html')))};
const executable=process.env.AXIOM_TEST_BROWSER_PATH??(process.platform==='darwin'?'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome':undefined);
const browser=await chromium.launch({headless:true,...(executable&&existsSync(executable)?{executablePath:executable}:{})});
const checks=[],errors=[],writes=[];let page;
async function login(p,user='input-fixture-admin'){
 await p.goto(base+'/login');await p.getByLabel('用户名',{exact:true}).fill(user);await p.getByLabel('密码',{exact:true}).fill('fixture-password');await p.getByRole('button',{name:'登录控制台',exact:true}).click();await p.waitForURL(u=>!u.pathname.endsWith('/login'));
}
async function open(id){
 await page.goto(base+'/scans/'+id);
 const region=page.getByRole('region',{name:'扫描中断与恢复',exact:true});await region.waitFor();
 const toggle=region.getByRole('button',{name:/^扫描中断与恢复/});if(await toggle.getAttribute('aria-expanded')==='false')await toggle.click();
 await region.getByText('SDK 0.5.0 · 此入口不支持断点续扫',{exact:true}).waitFor();return region;
}
async function snapshot(id){const r=await page.request.get(base+`/api/v1/scans/${id}/recovery`,{headers:{Authorization:'Bearer '+await page.evaluate(()=>localStorage.getItem('axiom_token'))}});assert.equal(r.status(),200);return (await r.json()).data;}
async function shot(name){
 if(process.env.AXIOM_TEST_RECOVERY_SCREENSHOTS==='0')return;
 await page.evaluate(async()=>{document.activeElement?.blur();await document.fonts.ready;window.scrollTo({top:0,behavior:'instant'});await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));});
 await page.screenshot({path:path.join(out,name+'.png'),fullPage:true,animations:'disabled'});
}
function runAgent(scenario='rich'){
 const capture=path.join(out,'fixture-sdk-'+scenario+'-'+Date.now()+'.json');
 const child=spawn(process.env.AXIOM_TEST_AGENT_BIN,['--once'],{cwd:root,env:{...process.env,AXIOM_SERVER_URL:base,AXIOM_AGENT_TOKEN:process.env.AXIOM_TEST_AGENT_TOKEN,AXIOM_AGENT_ID:'fixture-recovery-web-agent',AXIOM_AGENT_CONCURRENCY:'1',AXIOM_SCANNER_DIR:path.join(root,'code-audit-scanner'),AXIOM_SCAN_MOCK:'0',AXIOM_INPUT_TEST_CAPTURE:capture,AXIOM_RESULT_TEST_SCENARIO:scenario,NODE_OPTIONS:'--import='+path.join(root,'contracts/tests/input-sdk-register.mjs')}});
 let output='';child.stdout.on('data',v=>output+=v);child.stderr.on('data',v=>output+=v);
 const done=new Promise((resolve,reject)=>{child.on('error',reject);child.on('close',code=>code===0?resolve():reject(new Error(output)))});
 return {child,capture,done};
}
try{
 const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'});
 await context.route('**/*',r=>{const u=new URL(r.request().url());if(['http:','https:'].includes(u.protocol)&&u.origin!==new URL(base).origin)throw new Error('Recovery fixture attempted external traffic');return r.continue();});
 page=await context.newPage();page.setDefaultTimeout(15000);page.on('pageerror',e=>errors.push(e.message));page.on('request',r=>{if(r.method()==='POST'&&r.url().endsWith('/recovery'))writes.push(r.postDataJSON());});
 await login(page);
 let region=await open(ids.committed);const observed=await snapshot(ids.committed);
 assert.equal(await region.getByRole('button',{name:/断点续扫|继续扫描/}).count(),0);
 await region.getByRole('button',{name:'恢复结果入库',exact:true}).waitFor();checks.push('native-resume-explicitly-unsupported-and-committed-result-detected');
 await shot('recovery-desktop');await page.setViewportSize({width:390,height:844});assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));await shot('recovery-mobile');await page.setViewportSize({width:1440,height:1000});checks.push('desktop-and-mobile-recovery-evidence');
 await page.route('**/scans/'+ids.committed+'/recovery',r=>r.request().method()==='POST'?r.fulfill({status:503,contentType:'application/json',body:JSON.stringify({message:'合成恢复入库故障'})}):r.continue(),{times:1});
 await region.getByRole('button',{name:'恢复结果入库',exact:true}).click();await region.getByRole('alert').filter({hasText:'合成恢复入库故障'}).waitFor();assert.equal(writes.length,1);assert.equal(writes[0].action,'restore_result');checks.push('restore-failure-keeps-evidence-without-restart-fallback');
 await region.getByRole('button',{name:'恢复结果入库',exact:true}).click();await region.getByText(/结果已恢复入库 · 原第 1 次执行/).waitFor();
 await page.getByText('原进程退出状态未采集；已保留提交结果，不能据此推定进程成功退出。',{exact:true}).waitFor();const restored=await snapshot(ids.committed);assert.equal(restored.committedResult.state,'archived');assert.equal(restored.execution.attempt,1);assert.equal(restored.recovery.previousStatus,observed.taskStatus);assert.equal(restored.restart.allowed,true);
 await page.locator('.history-table-wrap>summary').click();await page.getByRole('cell',{name:/第 1 次 · 结果恢复入库/}).waitFor();checks.push('restore-with-original-attempt-history-and-fee');

 region=await open(ids.corrupt);await region.locator('p').filter({hasText:/提交标记、结果校验或执行身份不完整/}).waitFor();assert.equal(await region.getByRole('button',{name:'恢复结果入库',exact:true}).count(),0);checks.push('corrupted-result-never-offers-restore');
 region=await open(ids.missing);await region.locator('p').filter({hasText:/没有完整提交标记/}).waitFor();assert.equal(await region.getByRole('button',{name:'恢复结果入库',exact:true}).count(),0);
 const sessions=page.getByRole('region',{name:'SDK 原生会话事件',exact:true});await sessions.getByRole('button',{name:'查看原生会话事件',exact:true}).click();await sessions.getByText(/完整性未知/).waitFor();checks.push('missing-checkpoint-retains-uncommitted-session-evidence');
 await region.getByRole('button',{name:'登记租约过期中断',exact:true}).click();await region.locator('p').filter({hasText:/累计预算不足或存在未知费用/}).waitFor();assert.ok(await page.getByRole('button',{name:'重新执行',exact:true}).isDisabled());
 const budget=page.getByRole('region',{name:'任务预算与费用',exact:true});await budget.getByRole('button',{name:'核对第 1 次费用',exact:true}).click();
 let dialog=page.getByRole('dialog',{name:'核对执行费用',exact:true});await dialog.getByLabel('本次执行估算费用（USD）',{exact:true}).fill('1');await dialog.getByLabel('核对依据',{exact:true}).fill('Fixture only: interrupted execution fee evidence, no real billing');await dialog.getByRole('button',{name:'保存人工估算',exact:true}).click();await dialog.waitFor({state:'hidden'});checks.push('web-marks-expiry-and-requires-unknown-fee-reconciliation');
 await page.getByRole('button',{name:'重新执行',exact:true}).click();dialog=page.getByRole('dialog',{name:'确认重新执行扫描',exact:true});await dialog.waitFor();assert.ok(await dialog.getByRole('button',{name:'确认重新执行',exact:true}).isDisabled());await dialog.getByText(/重新执行可能重复分析并再次产生费用/).waitFor();
 await dialog.getByLabel('我已知悉这会重新调用模型并可能重复产生费用',{exact:true}).check();
 await page.route('**/scans/'+ids.missing+'/recovery',r=>r.request().method()==='POST'?r.fulfill({status:503,contentType:'application/json',body:JSON.stringify({message:'合成重新执行故障'})}):r.continue(),{times:1});
 await dialog.getByRole('button',{name:'确认重新执行',exact:true}).click();await dialog.getByRole('alert').filter({hasText:'合成重新执行故障'}).waitFor();assert.ok(await dialog.getByLabel('我已知悉这会重新调用模型并可能重复产生费用',{exact:true}).isChecked());checks.push('restart-requires-explicit-ack-and-retains-confirmation-on-error');
 await dialog.getByRole('button',{name:'确认重新执行',exact:true}).click();await dialog.waitFor({state:'hidden'});assert.equal(writes.at(-1).action,'restart');assert.equal(writes.at(-1).acknowledgeReexecution,true);assert.equal(writes.at(-1).resultSha256,null);
 const retry=runAgent();await retry.done;const called=JSON.parse(await readFile(retry.capture,'utf8'));assert.equal(called.options.maxCostUsd,1.5);assert.equal((await readFile(retry.capture+'.calls','utf8')).trim().split('\n').length,1);
 region=await open(ids.missing);const after=await snapshot(ids.missing);assert.equal(after.execution.attempt,2);assert.equal(after.taskStatus,'completed');await page.locator('.history-table-wrap>summary').click();await page.getByRole('region',{name:'调度与执行记录',exact:true}).getByRole('cell',{name:'第 1 次',exact:true}).waitFor();checks.push('confirmed-reexecution-creates-new-attempt-and-uses-remaining-budget');

 region=await open(ids.legacy);await region.locator('p').filter({hasText:/历史执行缺少完整结果合同/}).waitFor();checks.push('legacy-task-reader-does-not-require-recovery-metadata');
 await page.route('**/scans/'+ids.legacy+'/recovery',r=>r.fulfill({status:404,contentType:'application/json',body:JSON.stringify({message:'Fixture old Server'})}));
 await region.getByRole('button',{name:/^扫描中断与恢复/}).click();await region.getByRole('button',{name:/^扫描中断与恢复/}).click();await region.getByRole('alert').filter({hasText:'当前 Server 尚未提供扫描恢复入口'}).waitFor();assert.equal(await page.getByRole('heading',{name:'Fixture legacy recovery',exact:true}).count(),1);await page.unroute('**/scans/'+ids.legacy+'/recovery');checks.push('old-server-recovery-unavailable-keeps-history-readable');

 const token=await page.evaluate(()=>localStorage.getItem('axiom_token'));
 const original=await page.request.get(base+'/api/v1/scans/'+ids.missing,{headers:{Authorization:'Bearer '+token}});const config=(await original.json()).data;
 const created=await page.request.post(base+'/api/v1/scans',{headers:{Authorization:'Bearer '+token},data:{sourceSnapshotId:config.sourceSnapshotId,modelConfigId:config.modelConfigId,maxCostUsd:2,maxTotalCostUsd:2.5}});assert.equal(created.status(),200);const active=(await created.json()).data;
 const background=runAgent('reliability-hold');for(let i=0;i<60&&!existsSync(background.capture+'.processes');i++)await new Promise(r=>setTimeout(r,100));assert.ok(existsSync(background.capture+'.processes'));
 await page.goto(base+'/scans/'+active.id);await page.reload();await page.goto(base+'/scans');await background.done;
 const completed=await page.request.get(base+'/api/v1/scans/'+active.id,{headers:{Authorization:'Bearer '+token}});const continuous=(await completed.json()).data;assert.equal(continuous.status,'completed');assert.equal(continuous.attempt,1);assert.equal((await readFile(background.capture+'.calls','utf8')).trim().split('\n').length,1);checks.push('browser-refresh-and-navigation-do-not-restart-background-scan');

 const viewer=await context.newPage();await login(viewer,'recovery-fixture-viewer');await viewer.goto(base+'/scans/'+ids.committed);await viewer.getByRole('region',{name:'扫描中断与恢复',exact:true}).waitFor();assert.equal(await viewer.getByRole('button',{name:'重新执行',exact:true}).count(),0);const viewerToken=await viewer.evaluate(()=>localStorage.getItem('axiom_token'));
 const denied=await viewer.request.post(base+`/api/v1/scans/${ids.committed}/recovery`,{headers:{Authorization:'Bearer '+viewerToken},data:writes[0]});assert.equal(denied.status(),403);await viewer.close();checks.push('viewer-history-readable-and-recovery-writes-denied');
 assert.deepEqual(errors,[]);await writeFile(path.join(out,'browser.json'),JSON.stringify({...evidence,status:'passed',checks},null,2));process.stdout.write(JSON.stringify({status:'passed',checks,modelCalls:0})+'\n');
}catch(e){await writeFile(path.join(out,'browser.json'),JSON.stringify({...evidence,status:'failed',checks,error:String(e)},null,2));if(page)process.stderr.write((await page.locator('body').innerText()).slice(-6500)+'\n');throw e;}finally{await browser.close();}
