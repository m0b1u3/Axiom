// Static production Web in a disposable, isolated SDK-fixture stack.
import assert from 'node:assert/strict';
import {chromium} from 'playwright-core';
import {existsSync} from 'node:fs';
import {readFile} from 'node:fs/promises';
const base=process.env.AXIOM_TEST_WEB_URL;
if(!base||!['127.0.0.1','localhost','[::1]'].includes(new URL(base).hostname))throw new Error('Deployment acceptance requires a local fixture stack');
const executable=process.env.AXIOM_TEST_BROWSER_PATH??(process.platform==='darwin'?'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome':undefined);
const browser=await chromium.launch({headless:true,...(executable&&existsSync(executable)?{executablePath:executable}:{})});
try{
 const page=await browser.newPage(),failures=[];
 page.on('pageerror',error=>failures.push(error.message));
 await page.goto(base+'/login');
 await page.getByLabel('用户名',{exact:true}).fill(process.env.AXIOM_TEST_ADMIN_USER);
 await page.getByLabel('密码',{exact:true}).fill(process.env.AXIOM_TEST_ADMIN_PASSWORD);
 await page.getByRole('button',{name:'登录控制台',exact:true}).click();
 await page.waitForURL(url=>!url.pathname.endsWith('/login'));
 await page.goto(base+'/scans/'+process.env.AXIOM_TEST_DEPLOYMENT_TASK);
 await page.reload();
 const result=page.getByRole('region',{name:'归档结果',exact:true});
 await result.getByText('$1.125',{exact:true}).waitFor();
 await page.getByText('完整证据解释：用于合同验收',{exact:true}).waitFor();
 const downloaded=page.waitForEvent('download');
 await result.getByRole('button',{name:'下载完整结果 JSON',exact:true}).click();
 const file=await downloaded,data=JSON.parse(await readFile(await file.path(),'utf8'));
 assert.equal(data.result.model.model,'fixture-deployment-model');
 assert.equal(data.result.extensions.sdk.fixtureOnly,true);
 assert.equal(data.result.cost.estimatedUsd,1.125);
 if(process.env.AXIOM_TEST_STORAGE_RECOVERY_JOB){
  await page.goto(base+'/settings?section=storage');
  await page.getByRole('button',{name:'查看清理记录 '+process.env.AXIOM_TEST_STORAGE_RECOVERY_JOB,exact:true}).click();
  const plan=page.getByRole('region',{name:'清理计划',exact:true});await plan.getByText('执行未收尾 · 可继续',{exact:true}).waitFor();
  await plan.getByRole('button',{name:'继续清理',exact:true}).click();await plan.getByText('已完成',{exact:true}).waitFor();
 }
 assert.deepEqual(failures,[],'static Web emitted runtime errors');
 process.stdout.write('PASS: static Web browser login, SPA refresh, evidence and complete JSON download\n');
}finally{await browser.close()}
