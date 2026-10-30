// Fixtures / Mock: real browser and app APIs, inference boundaries replaced by
// the dedicated Go pipeline. No model requests, deployments or real accounts.
import assert from 'node:assert/strict';
import {chromium} from 'playwright-core';
import {existsSync} from 'node:fs';
import {mkdir,readFile,writeFile} from 'node:fs/promises';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
const base=process.env.AXIOM_TEST_BASE_URL,token=process.env.AXIOM_TEST_TOKEN;
if(!base||!token||!['127.0.0.1','localhost'].includes(new URL(base).hostname)||process.env.AXIOM_COMPONENT_USAGE_FIXTURE!=='1')throw new Error('Matching browser checks require the isolated loopback fixture');
const ids=JSON.parse(process.env.AXIOM_TEST_COMPONENT_TASKS??'{}'),root=fileURLToPath(new URL('../../',import.meta.url)),out=path.join(root,'output/playwright/component-matching');
await mkdir(out,{recursive:true});
const executable=process.env.AXIOM_TEST_BROWSER_PATH??(process.platform==='darwin'?'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome':undefined);
const browser=await chromium.launch({headless:true,...(executable&&existsSync(executable)?{executablePath:executable}:{})});
const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'});
const errors=[],external=[],checks=[];
await context.route('**/*',route=>{
 const url=new URL(route.request().url());
 if(['http:','https:'].includes(url.protocol)&&url.origin!==new URL(base).origin){external.push(url.origin);return route.abort();}
 return route.continue();
});
const page=await context.newPage();page.setDefaultTimeout(15000);page.on('pageerror',e=>errors.push(e.message));
const panel=()=>page.getByRole('region',{name:'组件执行',exact:true});
const matchingCell=()=>panel().locator('.budget-line > div').filter({has:page.locator('dt',{hasText:'匹配费用'})}).locator('dd');
async function open(id,query=''){
 await page.goto(base+'/scans/'+id+query);
 await panel().getByRole('button',{name:'Fixtures / Mock API',exact:true}).waitFor();
}
async function expand(){
 const summary=panel().locator('details.token-cost > summary');await summary.focus();await page.keyboard.press('Enter');
 assert.equal(await panel().locator('details.token-cost').getAttribute('open'),'');
}
async function shot(name){
 await page.evaluate(async()=>{await document.fonts.ready;document.activeElement?.blur();});
 await panel().screenshot({path:path.join(out,name+'.png'),animations:'disabled'});
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,name+' page overflow');
}
try{
 await page.goto(base+'/login');await page.getByLabel('用户名',{exact:true}).fill('input-fixture-admin');await page.getByLabel('密码',{exact:true}).fill('fixture-password');await page.getByRole('button',{name:'登录控制台',exact:true}).click();await page.waitForURL(url=>url.pathname!='/login');
 await open(ids.complete);
 await matchingCell().getByText('USD 0.000049 · 估算',{exact:true}).waitFor();
 await expand();
 assert.match(await panel().locator('details.token-cost').textContent(),/输入 200 · 输出 20 · 缓存读取 120 · 缓存写入 未知/);
 assert.match(await panel().locator('details.token-cost').textContent(),/2 次请求/);
 const budget=page.getByRole('region',{name:'任务预算与费用',exact:true});
 await budget.getByText(/USD 0\.000078/).first().waitFor();
 const pending=page.waitForEvent('download');await panel().getByRole('button',{name:'下载匹配用量与费用',exact:true}).click();const download=await pending;
 const data=JSON.parse(await readFile(await download.path(),'utf8'));assert.equal(data.amountMicros,49);assert.equal(data.usage.requestCount,2);assert.equal(data.usage.cacheWriteInputTokens,null);
 checks.push('matching-scope-actual-token-cache-and-frozen-estimate','combined-fees-do-not-double-count-component-callbacks','matching-raw-download','native-details-keyboard-toggle');
 await shot('known-desktop-light');
 await page.evaluate(()=>document.documentElement.classList.add('dark'));await shot('known-desktop-dark');
 await page.setViewportSize({width:390,height:844});await shot('known-mobile-dark');
 await page.evaluate(()=>document.documentElement.classList.remove('dark'));await shot('known-mobile-light');
 await page.setViewportSize({width:1440,height:1000});
 await open(ids['missing-usage']);assert.equal((await matchingCell().textContent()).trim(),'未知');await expand();assert.match(await panel().locator('details.token-cost').textContent(),/输入 未知/);
 assert.equal((await matchingCell().textContent()).includes('$0'),false);checks.push('missing-receipts-remain-unknown');
 await open(ids['partial-usage']);assert.match(await matchingCell().textContent(),/未知.*已知下界 USD 0\.000025/);checks.push('partial-receipts-labelled-as-lower-bound');
 await open(ids['http-failure']);assert.match(await matchingCell().textContent(),/未知/);assert.equal(await panel().getByRole('cell',{name:'第 1 次 / 1',exact:true}).count(),3);checks.push('matching-failure-keeps-independent-findings');
 await open(ids['change-price']);await matchingCell().getByText('USD 0.000049 · 估算',{exact:true}).waitFor();await expand();assert.match(await panel().locator('details.token-cost').textContent(),/版本 1/);assert.match(await panel().locator('details.token-cost').textContent(),/0\.3 \/ 1\.2/);checks.push('later-price-edit-does-not-reprice-execution');
 await open(ids.complete,'?attempt=1');await matchingCell().getByText('USD 0.000049 · 估算',{exact:true}).waitFor();checks.push('history-matching-estimate-binds-selected-attempt');
 const route=base+'/api/v1/scans/'+ids.complete+'/component-executions';
 await page.route(route,async r=>{const response=await r.fetch();const body=await response.json();for(const attempt of body.data.attempts)delete attempt.matchingEstimate;await r.fulfill({response,json:body});});
 await panel().getByRole('button',{name:'刷新组件',exact:true}).click();await panel().locator('details.token-cost').waitFor({state:'detached'});assert.equal((await matchingCell().textContent()).trim(),'未知');checks.push('legacy-record-does-not-invent-matching-usage-or-zero');await page.unroute(route);
 await page.route(route,async r=>{const response=await r.fetch();const body=await response.json();body.data.attempts[0].matchingEstimate.execution.attempt=99;await r.fulfill({response,json:body});});
 await panel().getByRole('button',{name:'刷新组件',exact:true}).click();await panel().getByRole('alert').waitFor();assert.equal(await panel().locator('details.token-cost').count(),0);assert.equal(await matchingCell().count(),0);checks.push('foreign-matching-identity-clears-stale-price');
 assert.deepEqual(errors,[]);assert.deepEqual(external,[]);
 await writeFile(path.join(out,'browser.json'),JSON.stringify({status:'passed',fixtureOnly:true,realModelRequests:0,publications:0,checks,screenshots:['known-desktop-light.png','known-desktop-dark.png','known-mobile-light.png','known-mobile-dark.png']},null,2)+'\n');
 process.stdout.write(JSON.stringify({status:'passed',checks})+'\n');
}finally{await browser.close();}
