// Local browser acceptance, called by the isolated Go input-pipeline test.
// Credentials, source and model names are synthetic; no model is called here.
import assert from 'node:assert/strict';
import {chromium} from 'playwright-core';
import {existsSync} from 'node:fs';
import {mkdir} from 'node:fs/promises';
import path from 'node:path';
const base=process.env.AXIOM_TEST_WEB_URL;
if(!base||!['127.0.0.1','localhost','[::1]'].includes(new URL(base).hostname))throw new Error('Browser acceptance requires a local fixture server');
const executable=process.env.AXIOM_TEST_BROWSER_PATH??(process.platform==='darwin'?'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome':undefined);
const browser=await chromium.launch({headless:true,...(executable&&existsSync(executable)?{executablePath:executable}: {})});
try {
 const page=await browser.newPage({viewport:{width:1440,height:1000}});
 const failures=[];page.on('pageerror',error=>failures.push(error.message));
 await page.goto(base+'/login');
 await page.getByLabel('用户名',{exact:true}).fill('input-fixture-admin');
 await page.getByLabel('密码',{exact:true}).fill('fixture-password');
 await page.getByRole('button',{name:'登录控制台',exact:true}).click();
 await page.waitForURL(url=>!url.pathname.endsWith('/login'));
 await page.goto(base+'/scans/new');
 await page.locator('input[type=file]:not(#git-archive)').setInputFiles(process.env.AXIOM_TEST_WEB_ARCHIVE);
 await page.getByText(/快照已生成/).waitFor();
 await page.getByLabel('扫描范围（可选）',{exact:true}).fill('src/a,b.ts\n源码/测试.ts');
 await page.locator('#scan-timeout').fill('47');
 await page.getByRole('radio',{name:/加强可利用性验证/}).check();
 await page.locator('#business-context').fill('普通用户可注册\n第二行业务背景');
 const budget=page.getByLabel('单次估算上限（USD，可选）',{exact:true});
 let creates=0;page.on('request',request=>{if(request.method()==='POST'&&new URL(request.url()).pathname==='/api/v1/scans')creates++;});
 await budget.fill('0');
 await page.getByRole('button',{name:'创建扫描',exact:true}).click();
 await page.getByText('费用预算须为正数；不设置预算时请留空。').first().waitFor();
 assert.equal(creates,0,'invalid budget reached the Server');
 await budget.fill('4.25');
 const screenshots=process.env.AXIOM_TEST_SCREENSHOT_DIR;
 if(screenshots){
   await mkdir(screenshots,{recursive:true});
   await page.evaluate(()=>{document.activeElement?.blur();window.scrollTo(0,0);});
   await page.screenshot({path:path.join(screenshots,'input-desktop.png'),fullPage:true,animations:'disabled'});
   await page.setViewportSize({width:390,height:844});
   assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth),'mobile form overflows');
   await page.screenshot({path:path.join(screenshots,'input-mobile.png'),fullPage:true,animations:'disabled'});
   await page.setViewportSize({width:1440,height:1000});
 }
 const responsePromise=page.waitForResponse(response=>response.request().method()==='POST'&&new URL(response.url()).pathname==='/api/v1/scans');
 await page.getByRole('button',{name:'创建扫描',exact:true}).click();
 const response=await responsePromise;
 assert.equal(response.status(),200);
 const input=response.request().postDataJSON();
 assert.equal(input.maxCostUsd,4.25);
 assert.deepEqual(input.includePaths,['src/a,b.ts','源码/测试.ts']);
 assert.equal(input.timeoutMinutes,47);
 assert.equal(input.businessContext,'普通用户可注册\n第二行业务背景');
 assert.ok(input.modelConfigId>0&&input.sourceSnapshotId>0);
 const result=await response.json();
 await page.waitForURL(new RegExp('/scans/'+result.data.id+'$'));
 assert.equal(failures.length,0,failures.join('\n'));
 process.stdout.write(JSON.stringify({fixtureOnly:true,id:result.data.id,creates})+'\n');
} finally {await browser.close();}
