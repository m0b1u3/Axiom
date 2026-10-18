// Read-only acceptance against an explicitly isolated Fixtures / Mock demo.
import assert from 'node:assert/strict';
import {chromium} from 'playwright-core';
import {mkdir,writeFile,readFile} from 'node:fs/promises';
import {existsSync} from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {exerciseEvidenceLayout} from './evidence-layout-browser.mjs';
const base=process.env.AXIOM_TEST_WEB_URL,url=base&&new URL(base);
if(!url||!['127.0.0.1','localhost','[::1]'].includes(url.hostname)||['5173','8080'].includes(url.port)||process.env.AXIOM_TEST_EVIDENCE_FIXTURE!=='1')throw new Error('Responsive evidence tests require an explicit, isolated loopback fixture environment');
const root=fileURLToPath(new URL('../../',import.meta.url)),out=process.env.AXIOM_TEST_EVIDENCE_REPORT_DIR??path.join(root,'output/playwright/evidence-layout');await mkdir(out,{recursive:true});
const task=process.env.AXIOM_TEST_EVIDENCE_TASK??'1',errors=[],writes=[],checks=[];
const executable=process.env.AXIOM_TEST_BROWSER_PATH??(process.platform==='darwin'?'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome':undefined);
const browser=await chromium.launch({headless:true,...(executable&&existsSync(executable)?{executablePath:executable}:{})});
try{
 const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'});
 await context.route('**/*',r=>{const u=new URL(r.request().url());if(['http:','https:'].includes(u.protocol)&&u.origin!==url.origin)return r.abort();if(!['GET','HEAD'].includes(r.request().method())&&u.pathname!=='/api/v1/auth/login'){writes.push(u.pathname);return r.abort();}return r.continue();});
 const page=await context.newPage();page.setDefaultTimeout(15000);page.on('pageerror',e=>errors.push(e.message));
 await page.goto(base+'/login');await page.getByLabel('用户名',{exact:true}).fill(process.env.AXIOM_TEST_ADMIN_USER??'demo-viewer');await page.getByLabel('密码',{exact:true}).fill(process.env.AXIOM_TEST_ADMIN_PASSWORD??'evidence-demo-only');await page.getByRole('button',{name:'登录控制台',exact:true}).click();await page.waitForURL(u=>!u.pathname.endsWith('/login'));
 await page.goto(base+'/scans/'+task);const region=page.getByRole('region',{name:'漏洞证据链',exact:true});await region.getByRole('button',{name:'查看证据链',exact:true}).click();await region.locator('svg').waitFor();
 const token=await page.evaluate(()=>localStorage.getItem('axiom_token')),response=await page.request.get(base+`/api/v1/scans/${task}/attempts/1/evidence-graph?fingerprint=fixture%3Aevidence-chain`,{headers:{Authorization:'Bearer '+token}});assert.equal(response.status(),200);const graph=(await response.json()).data;
 assert.equal(graph.origin,'native_evidence');assert.equal(graph.execution.taskId,String(task));
 const result=await exerciseEvidenceLayout(page,region,graph,{screenshots:process.env.AXIOM_TEST_EVIDENCE_SCREENSHOTS==='1'?out:undefined});checks.push(...result.checks);
 assert.deepEqual(writes,[]);assert.deepEqual(errors,[]);checks.push('responsive-investigation-performs-no-scan-writes-and-emits-no-runtime-errors');
 await writeFile(path.join(out,'browser.json'),JSON.stringify({status:'passed',fixtureOnly:true,base,checks,measurements:result.measurements,realAudits:0,modelCalls:0,writes,errors},null,2));process.stdout.write(JSON.stringify({status:'passed',checks,measurements:result.measurements,modelCalls:0})+'\n');
}catch(e){await writeFile(path.join(out,'browser.json'),JSON.stringify({status:'failed',checks,error:String(e),geometryFailures:e.geometryFailures??e.actual,measurements:e.measurements,errors,writes},null,2));throw e;}
finally{await browser.close();}
