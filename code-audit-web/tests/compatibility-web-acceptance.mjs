// Real browser and Server data from the disposable, explicitly synthetic
// compatibility pipeline. The inspected task never starts a model process.
import assert from 'node:assert/strict';
import {chromium} from 'playwright-core';
import {existsSync} from 'node:fs';
import {mkdir, readFile} from 'node:fs/promises';
import path from 'node:path';
const base=process.env.AXIOM_TEST_WEB_URL,id=process.env.AXIOM_TEST_COMPATIBILITY_ID,legacy=process.env.AXIOM_TEST_LEGACY_ID;
if(!base||!id||!legacy||!['127.0.0.1','localhost','[::1]'].includes(new URL(base).hostname))throw new Error('Compatibility acceptance requires a local synthetic Server');
const executable=process.env.AXIOM_TEST_BROWSER_PATH??(process.platform==='darwin'?'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome':undefined);
const browser=await chromium.launch({headless:true,...(executable&&existsSync(executable)?{executablePath:executable}:{})});
let page;const errors=[];
try {
 page=await browser.newPage({viewport:{width:1440,height:1000}});page.setDefaultTimeout(10000);page.on('pageerror',e=>errors.push(e.message));
 await page.goto(base+'/login');await page.getByLabel('用户名',{exact:true}).fill('input-fixture-admin');await page.getByLabel('密码',{exact:true}).fill('fixture-password');await page.getByRole('button',{name:'登录控制台',exact:true}).click();await page.waitForURL(u=>!u.pathname.endsWith('/login'));
 await page.goto(base+'/scans/'+id);
 const panel=page.getByRole('region',{name:'任务执行兼容性',exact:true});
 await panel.getByText('输入 1 · 完整结果 1 · 进度与事件 1',{exact:true}).waitFor();await panel.getByText('需要预算控制能力',{exact:true}).waitFor();
 const nodes=panel.getByRole('table',{name:'执行节点兼容状态',exact:true});
 const blocked=nodes.getByRole('row').filter({hasText:'fixture-input-agent'}),old=nodes.getByRole('row').filter({hasText:'fixture-legacy-node'});
 await blocked.getByText('预检被阻止',{exact:true}).waitFor();await blocked.getByText(/Scanner 能力描述缺失、无效或版本不受支持/).waitFor();
 await old.getByText('组件版本未上报',{exact:true}).waitFor();assert.match(await old.innerText(),/费用预算/);assert.match(await old.innerText(),/完整结果/);assert.match(await old.innerText(),/进度与事件/);
 await panel.getByText(/Server 与执行节点的兼容矩阵不一致/).waitFor();const matrixNode=nodes.getByRole('row').filter({hasText:'fixture-matrix-node'});await matrixNode.getByText(/配套组件矩阵/).waitFor();await matrixNode.getByText(/token_budget/).waitFor();
 assert.equal(await page.getByRole('progressbar').count(),0,'waiting compatibility cannot invent progress');
 const raw=panel.getByText('兼容判定原始数据',{exact:true});await raw.focus();await page.keyboard.press('Space');await panel.getByText(/"matrix_incompatible"/).waitFor();await page.keyboard.press('Space');
 const shots=process.env.AXIOM_TEST_COMPAT_SCREENSHOT_DIR;
 if(shots){
  await mkdir(shots,{recursive:true});await page.evaluate(()=>{document.activeElement?.blur();window.scrollTo({top:0,behavior:'instant'})});await page.screenshot({path:path.join(shots,'compatibility-desktop.png'),fullPage:true,animations:'disabled'});
  await page.setViewportSize({width:390,height:844});await panel.scrollIntoViewIfNeeded();assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'compatibility overflows mobile');await page.screenshot({path:path.join(shots,'compatibility-mobile.png'),animations:'disabled'});await page.setViewportSize({width:1440,height:1000});
 }
 await page.goto(base+'/settings?section=diagnostics');await page.getByRole('heading',{name:'高级诊断',exact:true}).waitFor();await page.getByRole('table',{name:'执行节点兼容状态',exact:true}).waitFor();await page.getByText('组件版本未上报',{exact:true}).waitFor();
 if(shots){await page.screenshot({path:path.join(shots,'compatibility-settings-desktop.png'),fullPage:true,animations:'disabled'});await page.setViewportSize({width:390,height:844});await page.getByRole('table',{name:'执行节点兼容状态',exact:true}).scrollIntoViewIfNeeded();assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'node settings overflow mobile');await page.screenshot({path:path.join(shots,'compatibility-settings-mobile.png'),animations:'disabled'});await page.setViewportSize({width:1440,height:1000});}
 // Simulate an old Server's existing response shape, leaving actual saved
 // tasks and report APIs intact. Missing metadata is explicitly unknown.
 await page.route('**/api/v1/executions/overview**',async route=>{const response=await route.fetch();const value=await response.json();for(const t of value.data.tasks)delete t.compatibility;delete value.data.maxRecoveries;await route.fulfill({response,json:value});});
 await page.goto(base+'/scans/'+id);await page.getByText(/Server 未上报兼容诊断/).waitFor();assert.equal(await page.getByRole('region',{name:'任务执行兼容性',exact:true}).count(),0);
 await page.goto(base+'/scans/'+legacy);await page.getByText('合成合同发现（非真实漏洞）',{exact:true}).first().waitFor();
 const history=page.getByText('执行历史',{exact:false}).first();await history.click();await page.getByRole('button',{name:'查看结果与日志',exact:true}).first().click();const selected=page.getByRole('region',{name:'所选执行结果',exact:true});
 const downloadEvent=page.waitForEvent('download');await selected.getByRole('button',{name:'下载本次结果',exact:true}).click();const download=await downloadEvent;const archived=JSON.parse(await readFile(await download.path(),'utf8'));assert.ok(archived.available&&archived.findings.length===1);assert.ok(archived.findings[0].raw,'legacy raw evidence was removed');
 assert.deepEqual(errors,[]);process.stdout.write(JSON.stringify({fixtureOnly:true,compatibilityEvidence:true,blockedIsNotFailure:true,unknownBuild:true,keyboardRaw:true,legacyServerNotice:true,legacyHistoryDownload:true})+'\n');
}catch(e){if(page)process.stderr.write('Fixture page '+page.url()+'\n'+(await page.locator('body').innerText()).slice(0,6000)+'\n');throw e;}finally{await browser.close();}
