// Real, isolated Go/PG catalog fixtures. No Agent, Scanner or model is started.
import assert from 'node:assert/strict';
import {chromium} from 'playwright-core';
import {existsSync} from 'node:fs';
import {mkdir, readFile, writeFile} from 'node:fs/promises';
import {createHash} from 'node:crypto';
import path from 'node:path';

const base=process.env.AXIOM_TEST_WEB_URL;
if(!base || new URL(base).hostname!=='127.0.0.1')throw new Error('Requires isolated loopback catalog fixture');
const project=Number(process.env.AXIOM_TEST_CATALOG_PROJECT), snapshot=Number(process.env.AXIOM_TEST_CATALOG_SNAPSHOT), history=Number(process.env.AXIOM_TEST_CATALOG_HISTORY);
if(!project || !snapshot || !history)throw new Error('Missing catalog fixture identities');
const executable=process.env.AXIOM_TEST_BROWSER_PATH ?? (process.platform==='darwin'?'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome':undefined);
const browser=await chromium.launch({headless:true,...(executable && existsSync(executable)?{executablePath:executable}:{})});
const context=await browser.newContext({viewport:{width:1440,height:1000}}),page=await context.newPage();
page.setDefaultTimeout(10000);
const checks=[],screenshots=[],requests=[],errors=[],external=[];
const reportDir=process.env.AXIOM_TEST_CATALOG_REPORT_DIR;
let intercept;
page.on('pageerror',error=>errors.push(error.message));
page.on('request',request=>{const u=new URL(request.url());if(u.pathname.startsWith('/api/'))requests.push({method:request.method(),path:u.pathname,query:u.search});});
await context.route('**/*',async route=>{
  const u=new URL(route.request().url());
  if(!['http:','https:'].includes(u.protocol))return route.continue();
  if(u.origin!==base){external.push(u.origin);return route.abort('blockedbyclient');}
  if(intercept && await intercept(route,u))return;
  return route.continue();
});
const button=(name,root=page)=>root.getByRole('button',{name,exact:true});
const nav=label=>page.getByRole('navigation',{name:label,exact:true});
const rows=()=>page.locator('.dt tbody tr');
const cell=name=>page.getByRole('cell',{name:new RegExp('^'+name.replace(/[.*+?^${}()|[\]\\]/g,'\\$&')+'(?:\\s|$)')});
const box=name=>page.getByRole('combobox',{name,exact:true});
async function choose(name,option){await box(name).click();await page.getByRole('option',{name:option,exact:true}).click();}
async function pager(label,total,current,last){await nav(label).getByText(`共 ${total} 条 · 第 ${current} / ${last} 页`,{exact:true}).waitFor();}
async function signIn(user='fixture-catalog-admin'){
  await page.goto(base+'/login');await page.getByLabel('用户名',{exact:true}).fill(user);await page.getByLabel('密码',{exact:true}).fill('fixture-catalog-password');await button('登录控制台').click();await page.waitForURL(u=>u.pathname!=='/login');
}
async function query(name,value,label,total){await page.getByLabel(name,{exact:true}).fill(value);await nav(label).getByText(new RegExp(`^共 ${total} 条 · 第 1 /`)).waitFor();}
async function shot(name){
  await page.evaluate(()=>window.scrollTo(0,0));
  if(page.viewportSize().width<=760)await page.waitForFunction(()=>{const s=document.querySelector('.sidebar');return s.inert && s.getBoundingClientRect().right<=1;});
  if(name==='findings-mobile')assert.ok((await page.getByLabel('搜索漏洞',{exact:true}).boundingBox()).width>=260,'Mobile finding search was squeezed by filter controls');
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,`${name} viewport overflow`);
  if(reportDir){await mkdir(reportDir,{recursive:true});await page.screenshot({path:path.join(reportDir,name+'.png'),fullPage:true,animations:'disabled'});screenshots.push(name+'.png');}
}
const digest=buffer=>createHash('sha256').update(buffer).digest('hex');
async function report(status,error){
  const data={status,fixtureOnly:true,realModelCalls:0,completedAt:new Date().toISOString(),webBuildIndexSha256:digest(await readFile(new URL('../dist/index.html',import.meta.url))),testSourceSha256:digest(await readFile(new URL(import.meta.url))),checks,screenshots,requests,errors,external,...(error?{failure:String(error.stack??error)}:{})};
  if(reportDir){await mkdir(reportDir,{recursive:true});await writeFile(path.join(reportDir,'report.json'),JSON.stringify(data,null,2));}return data;
}
try{
  await signIn();
  await page.goto(base+'/assets');await pager('资产分页',251,1,6);assert.equal(await rows().count(),50);
  await button('下一页',nav('资产分页')).click();await pager('资产分页',251,2,6);
  await button('末页',nav('资产分页')).click();await pager('资产分页',251,6,6);await cell('Fixture 历史中文 100%_!.zip').waitFor();assert.equal(await rows().count(),1);
  checks.push('Assets beyond legacy 200 are reachable with genuine total, next and last pages');
  await button('扫描快照 Fixture 历史中文 100%_!.zip').click();
  await box('代码快照').getByText(new RegExp('^#'+snapshot+' ')).waitFor();await pager('可选快照分页',251,1,6);
  await query('搜索已有代码快照','disposable','可选快照分页',51);await button('下一页',nav('可选快照分页')).click();await pager('可选快照分页',51,2,2);
  await box('代码快照').getByText(new RegExp('^#'+snapshot+' ')).waitFor();assert.equal(await button('创建扫描').isEnabled(),true);
  await page.goBack();await pager('资产分页',251,6,6);
  checks.push('Older asset deep link loads exact source; source search/paging keeps the selection without creating a scan');
  await query('搜索代码快照','100%_!','资产分页',1);await cell('Fixture 历史中文 100%_!.zip').waitFor();
  await page.reload();await pager('资产分页',1,1,1);assert.equal(await page.getByLabel('搜索代码快照',{exact:true}).inputValue(),'100%_!');
  await query('搜索代码快照','','资产分页',251);await choose('资产所属项目','未归属项目');await pager('资产分页',81,1,2);
  await choose('快照引用状态','有扫描引用');await pager('资产分页',0,1,1);await page.getByText('没有匹配的快照',{exact:true}).waitFor();
  await button('清除资产所属项目').click();await pager('资产分页',1,1,1);await button('清除快照引用状态').click();await pager('资产分页',251,1,6);
  checks.push('Server literal search, URL refresh, project/unassigned and reference filters compose across the full asset set');
  await button('文件数').click();await pager('资产分页',251,1,6);
  // TanStack starts numeric sorts descending; either direction must be a full
  // catalog order, not sorting the previous 50 newest rows locally.
  const direction=new URL(page.url()).searchParams.get('order');
  const first=await rows().first().textContent();assert.ok(first.includes(direction==='desc'?'Fixture asset 250.zip':'Fixture 历史中文 100%_!.zip'));
  checks.push('Table sorting is delegated to the server over all assets');
  await query('搜索代码快照','disposable','资产分页',51);await button('下一页',nav('资产分页')).click();await pager('资产分页',51,2,2);
  await page.locator('button[aria-label^="删除快照 "]').first().click();const dialog=page.getByRole('dialog',{name:'清理与删除预览',exact:true});
  await dialog.getByRole('region',{name:'清理计划',exact:true}).waitFor();await button('确认执行清理',dialog).click();await dialog.waitFor({state:'hidden'});
  await pager('资产分页',50,1,1);assert.equal(await rows().count(),50);
  checks.push('Deleting the last row uses the recoverable cleanup flow and clamps the confirmed page after deletion');
  // Hold an older real HTTP response until the later query has rendered.
  let release,started,finished;
  const gate=new Promise(resolve=>{release=resolve}),begin=new Promise(resolve=>{started=resolve}),done=new Promise(resolve=>{finished=resolve});
  intercept=async(route,u)=>{
    if(u.pathname==='/api/v1/snapshots' && u.searchParams.get('q')==='fixture-content-100'){
      const response=await route.fetch();started();await gate;await route.fulfill({response});finished();return true;
    }return false;
  };
  await page.getByLabel('搜索代码快照',{exact:true}).fill('fixture-content-100');await begin;
  await query('搜索代码快照','100%_!','资产分页',1);release();await done;
  await cell('Fixture 历史中文 100%_!.zip').waitFor();assert.equal(await cell('Fixture asset 100.zip').count(),0);intercept=undefined;
  checks.push('A delayed previous asset query cannot overwrite the accepted later search');
  const fail=async(route,u)=>{if(u.pathname==='/api/v1/snapshots'){await route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({message:'Fixture catalog temporarily unavailable',code:50300})});return true;}return false;};
  intercept=fail;await page.getByLabel('搜索代码快照',{exact:true}).fill('unmatched');await page.getByRole('alert').filter({hasText:'Fixture catalog temporarily unavailable'}).waitFor();
  await cell('Fixture 历史中文 100%_!.zip').waitFor();assert.equal(await page.getByText('没有匹配的快照',{exact:true}).count(),0);await nav('资产分页').getByText(/上次成功结果：共 1 条/).waitFor();
  intercept=undefined;await button('重试').click();await pager('资产分页',0,1,1);
  checks.push('Failed query preserves confirmed rows/totals as stale; retry resolves the current query without fake empty data');
  await page.goto(base+'/projects/'+project);await pager('项目快照分页',119,1,3);
  await query('搜索项目代码快照','100%_!','项目快照分页',1);await cell('Fixture 历史中文 100%_!.zip').waitFor();
  await query('搜索未归属快照','fixture-content-170','未归属快照分页',1);await box('待关联快照').click();await page.getByRole('option',{name:/^#171 /}).click();await button('关联').click();
  await pager('未归属快照分页',0,1,1);await query('搜索项目代码快照','fixture-content-170','项目快照分页',1);await cell('Fixture asset 170.zip').waitFor();
  checks.push('Project asset pagination and server search can associate an older unassigned asset and refresh both sets');
  await page.goto(base+'/findings');await pager('漏洞分页',401,1,9);assert.equal(await rows().count(),50);
  await button('末页',nav('漏洞分页')).click();await pager('漏洞分页',401,9,9);await cell('Fixture 历史漏洞 中文 100%_!').waitFor();
  await button('Low 1').click();await pager('漏洞分页',1,1,1);await cell('已确认').waitFor();await button('High 399').waitFor();
  await button('全部 401').click();await query('搜索漏洞','cwe-9999','漏洞分页',1);await cell('Fixture 历史漏洞 中文 100%_!').waitFor();
  await choose('漏洞状态','已修复');await pager('漏洞分页',0,1,1);await button('清除漏洞状态').click();await pager('漏洞分页',1,1,1);
  checks.push('Deduplicated findings beyond legacy 300, full severity facets, CWE search and inherited human disposition remain accurate');
  const prior=page.url();await cell('Fixture 历史漏洞 中文 100%_!').click();await page.waitForURL(u=>u.pathname==='/scans/'+history);
  await page.getByRole('complementary',{name:'漏洞证据',exact:true}).waitFor();await page.getByText('Fixture original evidence',{exact:true}).waitFor();await page.keyboard.press('Escape');
  await page.goBack();await pager('漏洞分页',1,1,1);assert.equal(page.url(),prior);
  checks.push('Historical finding evidence remains readable and browser back restores the scoped catalog query');
  await page.goto(base+'/findings?projectId=0');await pager('漏洞分页',0,1,1);await button('清除漏洞所属项目').click();await pager('漏洞分页',401,1,9);
  await page.goto(base+'/findings?sourceSnapshotId=2&q='+encodeURIComponent('100%_!'));await pager('漏洞分页',1,1,1);
  await button('清除来源筛选').click();await pager('漏洞分页',1,1,1);assert.equal(new URL(page.url()).searchParams.has('sourceSnapshotId'),false);
  checks.push('Finding project/source filters query the projected occurrence without resurrecting older records');
  await page.keyboard.press('Control+k');await page.getByRole('dialog',{name:'搜索工作空间',exact:true}).waitFor();await page.getByRole('textbox',{name:'搜索工作空间',exact:true}).fill('cwe-9999');
  await page.getByRole('button',{name:/Fixture 历史漏洞 中文/}).waitFor();await button('查看全部漏洞').click();await pager('漏洞分页',1,1,1);
  checks.push('Workspace search also resolves old findings from the server and links to the full query');
  intercept=async(route,u)=>{if(u.pathname==='/api/v1/snapshots'){await route.fulfill({contentType:'application/json',body:JSON.stringify({data:[]})});return true;}return false;};
  await page.goto(base+'/assets');await page.getByRole('alert').filter({hasText:'Web 与 Server 升级到同一版本'}).waitFor();
  assert.equal(await page.getByText('还没有代码快照',{exact:true}).count(),0);
  await nav('资产分页').getByText(/共 — 条/).waitFor();intercept=undefined;await button('重试').click();await pager('资产分页',250,1,5);
  checks.push('A legacy Server array response produces an explicit upgrade error without false empty data or capped fallback');
  await page.goto(base+'/scans/new?snapshotId=999999');await page.getByRole('alert').filter({hasText:'选定的代码快照已不存在'}).waitFor();
  assert.equal(await button('创建扫描').isDisabled(),true);await button('清除失效源码并重新选择').click();await pager('可选快照分页',250,1,5);
  await query('搜索已有代码快照','100%_!','可选快照分页',1);await box('代码快照').click();await page.getByRole('option',{name:new RegExp('^#'+snapshot+' ')}).click();
  await box('代码快照').getByText(new RegExp('^#'+snapshot+' ')).waitFor();assert.equal(await button('创建扫描').isEnabled(),true);
  checks.push('A removed source cannot silently select another asset; explicit discard and search restore a usable form');
  // One bounded screenshot batch for the changed surfaces in desktop, mobile
  // and dark; no arbitrary visual variants or mock content.
  for(const [name,route] of [['assets','/assets'],['findings','/findings'],['project','/projects/'+project],['new-scan','/scans/new?snapshotId='+snapshot]]){
    await page.goto(base+route);await page.locator('.catalog-pagination').first().getByText(/^共 /).waitFor();
    await page.waitForFunction(()=>!document.querySelector('.catalog-pagination[aria-busy="true"]'));
    await page.setViewportSize({width:1440,height:1000});await shot(name+'-desktop');
    await page.setViewportSize({width:390,height:844});await shot(name+'-mobile');
    await page.setViewportSize({width:1440,height:1000});await button('切换深色模式').click();await shot(name+'-dark');await button('切换浅色模式').click();
  }
  checks.push('Pagination, search, filters and source picker remain within desktop/mobile viewports and use existing dark tokens');
  const token=await page.evaluate(()=>localStorage.getItem('axiom_token'));await context.request.post(base+'/api/v1/auth/logout',{headers:{Authorization:'Bearer '+token}});
  await page.evaluate(()=>localStorage.removeItem('axiom_token'));await signIn('fixture-catalog-viewer');await page.goto(base+'/assets');await pager('资产分页',250,1,5);assert.equal(await button('上传代码包').count(),0);assert.equal(await page.locator('button[aria-label^="删除快照 "]').count(),0);
  await page.goto(base+'/findings?q=cwe-9999');await pager('漏洞分页',1,1,1);
  checks.push('Read-only accounts can page/search history without mutation controls');
  assert.deepEqual(errors,[]);assert.deepEqual(external,[]);
  assert.ok(!requests.some(r=>r.method==='POST'&&r.path==='/api/v1/scans'),'started a real audit');
  assert.ok(!requests.some(r=>r.method==='DELETE'&&/^\/api\/v1\/(snapshots|scans)\/\d+$/.test(r.path)),'used legacy direct deletion');
  assert.ok(requests.filter(r=>r.method==='GET'&&['/api/v1/snapshots','/api/v1/findings'].includes(r.path)).every(r=>new URLSearchParams(r.query).has('page')),'Web fell back to capped array endpoint');
  const data=await report('passed');console.log(JSON.stringify({status:data.status,checks:checks.length,screenshots:screenshots.length,fixtureOnly:true,realModelCalls:0}));
}catch(error){await report('failed',error);console.error(JSON.stringify({url:page.url(),checks,body:(await page.locator('body').innerText()).slice(-7000)}));throw error;}
finally{await browser.close();}
