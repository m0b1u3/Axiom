// Real browser against the isolated Go/PG/Agent/Scanner fixture pipeline.
// Every finding, model and credential in this test is explicitly synthetic.
import assert from 'node:assert/strict';
import {chromium} from 'playwright-core';
import {existsSync} from 'node:fs';
import {readFile,mkdir} from 'node:fs/promises';
import path from 'node:path';
const base=process.env.AXIOM_TEST_WEB_URL,ids=JSON.parse(process.env.AXIOM_TEST_RESULT_IDS??'{}');
if(!base || !['127.0.0.1','localhost','[::1]'].includes(new URL(base).hostname))throw new Error('Result browser test requires a local fixture server');
const executable=process.env.AXIOM_TEST_BROWSER_PATH??(process.platform==='darwin'?'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome':undefined);
const browser=await chromium.launch({headless:true,...(executable&&existsSync(executable)?{executablePath:executable}:{})});
let debugPage;const debugErrors=[];
try{
 const page=debugPage=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[];
 await page.addInitScript(()=>{
   window.__resultFixtureToasts=[];
   document.addEventListener('DOMContentLoaded',()=>new MutationObserver(()=>{for(const node of document.querySelectorAll('[data-sonner-toast]')){const value=node.textContent;if(value&&!window.__resultFixtureToasts.includes(value))window.__resultFixtureToasts.push(value)}}).observe(document.body,{childList:true,subtree:true,characterData:true}));
 });
 page.on('pageerror',e=>{errors.push(e.message);debugErrors.push(e.stack)});page.on('console',e=>{if(e.type()==='error')debugErrors.push(e.text())});page.on('response',r=>{if(r.url().includes('/api/'))debugErrors.push(r.status()+' '+new URL(r.url()).pathname)});
 await page.goto(base+'/login');await page.getByLabel('用户名',{exact:true}).fill('input-fixture-admin');await page.getByLabel('密码',{exact:true}).fill('fixture-password');await page.getByRole('button',{name:'登录控制台',exact:true}).click();await page.waitForURL(url=>!url.pathname.endsWith('/login'));
 await page.goto(base+'/scans/'+ids.rich);
 const result=page.getByRole('region',{name:'归档结果',exact:true});
 assert.equal(await result.getByText('SDK 原始估算 · USD',{exact:true}).count(),0);await result.getByText('0 / 17',{exact:true}).waitFor();await result.locator('.result-properties').getByText('fixture-reported-model',{exact:true}).waitFor();
 await page.getByText('合成置信理由；未执行独立验证',{exact:true}).waitFor();await page.getByText('完整证据解释：用于合同验收',{exact:true}).waitFor();await page.getByText('SDK Finding ID: fixture-finding-1',{exact:true}).waitFor();
 const downloadPromise=page.waitForEvent('download');await result.getByRole('button',{name:'下载完整结果 JSON',exact:true}).click();const download=await downloadPromise;const data=JSON.parse(await readFile(await download.path(),'utf8'));
 assert.equal(data.result.cost,null);assert.equal(data.result.extensions.executionUsage.inputTokens,0);assert.equal(data.result.extensions.executionUsage.outputTokens,17);assert.equal(data.result.extensions.sdk.cost.estimatedUsd,1.125);assert.equal(data.result.model.model,'fixture-result-configured-model');
 assert.equal(data.result.extensions.sdk.turn.usage.reasoning_output_tokens,5);assert.deepEqual(data.result.extensions.sdk.unknownNativeResult.retained,[null,0,false,'']);
 assert.equal(data.result.findings[0].engineOccurrenceId,'fixture-occurrence-1');assert.equal(data.result.findings[0].confidence.value,undefined);
 const shots=process.env.AXIOM_TEST_SCREENSHOT_DIR;
 if(shots){
  await mkdir(shots,{recursive:true});await page.evaluate(()=>{document.activeElement?.blur();window.scrollTo({top:0,behavior:'instant'})});
  await page.screenshot({path:path.join(shots,'result-desktop.png'),fullPage:true,animations:'disabled'});
  await page.setViewportSize({width:390,height:844});await result.scrollIntoViewIfNeeded();
  assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'result workspace overflows on mobile');
  await page.screenshot({path:path.join(shots,'result-mobile.png'),animations:'disabled'});
  await page.setViewportSize({width:1440,height:1000});
 }
 await result.getByText('原生 Token 记录',{exact:true}).click();await result.getByRole('heading',{name:'SDK 原生 Token 用量',exact:true}).waitFor();await result.getByText('推理输出 Token',{exact:true}).waitFor();
 await result.getByText('执行元数据与模型快照',{exact:true}).click();await result.getByText('fixture-result-configured-model',{exact:false}).first().waitFor();
 await result.getByRole('button',{name:'查看完整 JSON（含 SDK 原始结果与扩展字段）',exact:true}).click();await page.getByRole('region',{name:'完整结果 JSON',exact:true}).waitFor();
 await page.goto(base+'/scans/'+ids.empty);await page.getByRole('region',{name:'任务预算与费用',exact:true}).getByText('未知',{exact:true}).first().waitFor();
 assert.equal(await page.getByRole('region',{name:'归档结果',exact:true}).getByText('$0.00',{exact:true}).count(),0,'unknown cost appeared as zero');
 await page.goto(base+'/scans/'+ids['partial-empty']);await page.getByText(/SDK 结果为 partial/).waitFor();
 await page.goto(base+'/scans/'+ids['late-exit']);await page.getByText('SDK 结果已校验并归档；进程退出与结果状态不同，退出记录保留在执行元数据中。',{exact:true}).waitFor();
 await page.goto(base+'/scans/'+ids['wrong-task']);await page.getByText(/SDK 未生成可提交的结果/).waitFor();
 assert.deepEqual(errors,[],'browser emitted runtime errors');
 process.stdout.write(JSON.stringify({fixtureOnly:true,resultRetention:true,unknownCost:true,partial:true,lateExit:true,invalidIdentity:true})+'\n');
}catch(error){process.stderr.write("Fixture errors: "+JSON.stringify(debugErrors)+"\n");if(debugPage)process.stderr.write("Fixture toasts: "+JSON.stringify(await debugPage.evaluate(()=>window.__resultFixtureToasts))+"\n");if(debugPage)process.stderr.write("Fixture page: "+debugPage.url()+"\n"+(await debugPage.locator("body").innerText()).slice(0,4500)+"\n");throw error;}finally{await browser.close()}
