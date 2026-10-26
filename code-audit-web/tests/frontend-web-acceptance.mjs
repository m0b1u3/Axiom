// Full page/action acceptance against the real disposable Go Server and PG.
// No Agent/Scanner/SDK runs. Provider checks use an HTTP fixture on loopback.
import assert from 'node:assert/strict';
import {chromium} from 'playwright-core';
import {existsSync} from 'node:fs';
import {mkdir, readFile, writeFile, readdir, stat} from 'node:fs/promises';
import path from 'node:path';
import {createHash} from 'node:crypto';

const base = process.env.AXIOM_TEST_WEB_URL;
const local = value => ['127.0.0.1', 'localhost', '[::1]'].includes(new URL(value).hostname);
if (!base || !local(base) || !local(process.env.AXIOM_TEST_FRONTEND_PROVIDER)) throw new Error('Requires isolated loopback UI and provider fixtures');
const archive = process.env.AXIOM_TEST_FRONTEND_ARCHIVE;
const project = Number(process.env.AXIOM_TEST_FRONTEND_PROJECT);
const emptyProject = Number(process.env.AXIOM_TEST_FRONTEND_EMPTY_PROJECT);
const history = Number(process.env.AXIOM_TEST_FRONTEND_HISTORY);
if (!archive || !project || !emptyProject || !history) throw new Error('Missing explicit frontend fixtures');
const executable = process.env.AXIOM_TEST_BROWSER_PATH ?? (process.platform === 'darwin' ? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' : undefined);
const browser = await chromium.launch({headless: true, ...(executable && existsSync(executable) ? {executablePath: executable} : {})});
const context = await browser.newContext({viewport: {width: 1440, height: 1000}, acceptDownloads: true});
const page = await context.newPage();
page.setDefaultTimeout(10000);
const checks = [], failures = [], remote = [], requests = [], screenshots = [];
const stubs = [];
async function intercept(match, handler) { stubs.push({match, handler}); }
async function restore(match, handler) { for (let index = stubs.length - 1; index >= 0; index--) if (stubs[index].match === match && (!handler || stubs[index].handler === handler)) stubs.splice(index, 1); }
page.on('pageerror', error => failures.push(error.message));
page.on('request', request => {
  const url = new URL(request.url());
  if (url.pathname.startsWith('/api/')) requests.push({method: request.method(), path: url.pathname});
});
await context.route('**/*', async route => {
  const url = new URL(route.request().url());
  const stub = stubs.findLast(stub => stub.match(url, route.request()));
  if (stub) return stub.handler(route);
  if (!['http:', 'https:'].includes(url.protocol) || local(url.href)) return route.continue();
  remote.push(url.origin); await route.abort('blockedbyclient');
});
const button = (name, root = page) => root.getByRole('button', {name, exact: true});
const combobox = (name, root = page) => root.getByRole('combobox', {name, exact: true});
const alert = text => page.getByRole('alert').filter({hasText: text});
const responseBody = async response => (await response.json()).data;
async function signIn(user = 'fixture-frontend-admin') {
  await page.goto(base + '/login');
  await page.getByLabel('用户名', {exact: true}).fill(user);
  await page.getByLabel('密码', {exact: true}).fill('fixture-frontend-password');
  await button('登录控制台').click();
  await page.waitForURL(url => url.pathname !== '/login');
  await page.locator('.workspace').waitFor();
}
async function choose(name, option, root = page) {
  await combobox(name, root).click();
  await page.getByRole('option', {name: option, exact: true}).click();
}
async function fault(endpoint, method = 'GET', status = 503, text = 'Fixture 请求暂不可用') {
  const match = (url, request) => url.pathname === '/api/v1' + endpoint && request.method() === method;
  const handler = route => route.fulfill({status, contentType: 'application/json', body: JSON.stringify({code: status * 100, message: text})});
  await intercept(match, handler);
  return () => restore(match, handler);
}
async function failThenRetry(route, endpoint, ready, retry = '重试', status = 503) {
  const undo = await fault(endpoint, 'GET', status);
  await page.goto(base + route);
  await alert('Fixture 请求暂不可用').first().waitFor();
  await undo();
  await button(retry).first().click();
  await ready();
  checks.push(`${route}: HTTP ${status} is visible and retry restores data`);
}
async function snapshotUpload() {
  const received = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/uploads');
  await page.locator('input[type=file]:not(#git-archive)').setInputFiles(archive);
  const response = await received;
  assert.equal(response.status(), 200);
  await page.waitForFunction(() => document.querySelector('input[type=file]').value === '');
  return responseBody(response);
}
async function acknowledge(title, action = '确定') {
  await button(action, page.getByRole('dialog', {name: title, exact: true})).click();
}
async function shoot(name) {
  const directory = process.env.AXIOM_TEST_FRONTEND_REPORT_DIR;
  if (!directory) return;
  await mkdir(directory, {recursive: true});
  await page.evaluate(async () => { document.activeElement?.blur(); await document.fonts.ready; await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))); });
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), name + ' causes page-level horizontal overflow');
  const file = path.join(directory, name + '.png');
  await page.screenshot({path: file, fullPage: true, animations: 'disabled'});
  screenshots.push(name + '.png');
}

try {
  await page.goto(base + '/projects');
  await page.waitForURL(url => url.pathname === '/login');
  await page.getByLabel('用户名', {exact: true}).fill('');
  await button('登录控制台').click();
  await alert('请输入用户名和密码').waitFor();
  await page.getByLabel('用户名', {exact: true}).fill('fixture-frontend-admin');
  await page.getByLabel('密码', {exact: true}).fill('fixture-wrong-password');
  await button('登录控制台').click();
  await page.getByRole('alert').waitFor();
  assert.equal(new URL(page.url()).pathname, '/login');
  await signIn();
  checks.push('Login validation, rejected password, authenticated redirect');

  for (const invalid of ['/no-such-page', '/scans/invalid', '/projects/0']) {
    await page.goto(base + invalid);
    await page.getByRole('heading', {name: '页面不存在', exact: true}).waitFor();
    await page.getByRole('link', {name: '返回工作台', exact: true}).click();
    await page.getByRole('heading', {name: '安全工作台', exact: true}).waitFor();
  }
  checks.push('Unknown routes and malformed object IDs have recovery navigation');

  await failThenRetry('/', '/dashboard', () => page.getByRole('link', {name: /Fixture 前端完整性项目/}).first().waitFor());
  let releaseDashboard;
  const dashboardGate = new Promise(resolve => { releaseDashboard = resolve; });
  const dashboardMatch = url => url.pathname === '/api/v1/dashboard';
  await intercept(dashboardMatch, async route => { await dashboardGate; return route.continue(); });
  await page.goto(base + '/'); await page.getByRole('status').filter({hasText:'正在加载工作台…'}).waitFor();
  releaseDashboard(); await restore(dashboardMatch); await page.getByRole('link',{name:/Fixture 前端完整性项目/}).first().waitFor();
  await intercept(dashboardMatch, route => route.abort('failed')); await page.goto(base + '/'); await alert('工作台数据加载失败').waitFor();
  await restore(dashboardMatch); await button('重试').click(); await page.getByRole('link',{name:/Fixture 前端完整性项目/}).first().waitFor();
  checks.push('Slow initial load is visible; offline API read stays recoverable');
  await button('刷新工作台').click();
  await page.keyboard.press('Control+k'); await page.getByRole('dialog', {name: '搜索工作空间', exact: true}).waitFor(); await page.keyboard.press('Escape');
  await button('搜索项目、扫描和漏洞').click();
  const searchDialog = page.getByRole('dialog', {name: '搜索工作空间', exact: true});
  await searchDialog.getByRole('textbox', {name: '搜索工作空间', exact: true}).fill('Fixture 合成证据');
  await searchDialog.getByRole('button', {name: /Fixture 合成证据/}).click();
  await page.waitForURL(url => url.pathname === '/scans/' + history);
  checks.push('Dashboard refresh and global object search navigate to evidence');

  await failThenRetry('/projects', '/projects', () => page.getByRole('cell', {name: 'Fixture 前端完整性项目', exact: true}).waitFor(), '重试', 403);
  await button('新建项目').click();
  let dialog = page.getByRole('dialog', {name: '新建项目', exact: true});
  await button('创建', dialog).click();
  await dialog.getByRole('alert').filter({hasText: '请输入项目名称'}).waitFor();
  await dialog.getByLabel('项目名称', {exact: true}).fill('Fixture Web 用户项目');
  let undo = await fault('/projects', 'POST', 503, 'Fixture 创建失败');
  await button('创建', dialog).click();
  await dialog.getByRole('alert').filter({hasText: 'Fixture 创建失败'}).waitFor();
  assert.equal(await dialog.getByLabel('项目名称', {exact: true}).inputValue(), 'Fixture Web 用户项目');
  await undo(); await button('创建', dialog).click(); await dialog.waitFor({state: 'hidden'});
  const userProject = page.getByRole('row').filter({hasText: 'Fixture Web 用户项目'});
  await userProject.waitFor();
  await userProject.focus(); await page.keyboard.press('Home'); await page.keyboard.press('End');
  const lastProject = page.locator('.dt tbody tr').last();
  assert.ok(await lastProject.evaluate(el => el === document.activeElement));
  await page.keyboard.press('k'); await page.keyboard.press('ArrowDown');
  assert.ok(await lastProject.evaluate(el => el === document.activeElement));
  const resize = page.getByRole('separator', {name: '调整列宽 项目', exact: true});
  const width = Number(await resize.getAttribute('aria-valuenow'));
  await resize.focus(); await page.keyboard.press('ArrowRight');
  assert.equal(Number(await resize.getAttribute('aria-valuenow')), width + 16);
  await resize.press('Home');
  const handle = await resize.boundingBox();
  await page.mouse.move(handle.x + handle.width / 2, handle.y + handle.height / 2);
  await page.mouse.down(); await page.mouse.move(handle.x + 40, handle.y + handle.height / 2); await page.mouse.up();
  assert.ok(Number(await resize.getAttribute('aria-valuenow')) > width);
  await userProject.focus(); await page.keyboard.press('Enter');
  await page.waitForURL(url => /^\/projects\/\d+$/.test(url.pathname));
  const createdProject = Number(new URL(page.url()).pathname.split('/').at(-1));
  await page.getByText('还没有代码快照', {exact: true}).waitFor();
  checks.push('Project creation preserves form on failure; rows support arrows/J/K/Enter and column resizing');

  await failThenRetry('/assets', '/snapshots', () => button('上传代码包').waitFor());
  const snap = await snapshotUpload();
  const duplicate = await snapshotUpload(); assert.equal(duplicate.id, snap.id, 'duplicate upload created a second asset');
  await page.getByLabel('搜索代码快照',{exact:true}).fill('no-such-fixture-asset'); await page.getByText('没有匹配的快照',{exact:true}).waitFor(); await page.getByLabel('搜索代码快照',{exact:true}).fill('');
  await page.goto(base + '/projects/' + createdProject);
  await combobox('待关联快照').click();
  await page.getByRole('option', {name: new RegExp('^#' + snap.id + ' ')}).click();
  await button('关联').click();
  await page.getByRole('cell', {name: /^fixture-ui-source\.zip/}).waitFor();
  await button('扫描快照 fixture-ui-source.zip').click(); await page.waitForURL(url => url.pathname === '/scans/new' && url.searchParams.get('snapshotId') === String(snap.id));
  await combobox('代码快照').getByText(new RegExp('^#' + snap.id)).waitFor();
  await page.goto(base + '/projects/' + createdProject);
  await button('删除快照 fixture-ui-source.zip').click();
  dialog = page.getByRole('dialog', {name: '清理与删除预览', exact: true});
  await dialog.getByRole('region', {name: '清理计划', exact: true}).waitFor();
  await button('关闭', dialog).click();
  undo = await fault('/storage/cleanup', 'POST');
  await button('删除快照 fixture-ui-source.zip').click();
  await dialog.getByRole('alert').waitFor(); await undo();
  await button('重新生成预览', dialog).click();
  await dialog.getByRole('region', {name: '清理计划', exact: true}).waitFor();
  await button('确认执行清理', dialog).click(); await dialog.waitFor({state: 'hidden'});
  await page.getByText('还没有代码快照', {exact: true}).waitFor();
  checks.push('Asset upload/deduplication, project association, cleanup preview/cancel/failure/retry/execute');

  await page.goto(base + '/projects/' + emptyProject); await page.getByText('还没有扫描记录', {exact: true}).waitFor();
  await page.goto(base + '/projects/999999'); await alert('数据加载失败').waitFor();
  assert.equal(await button('上传代码包').count(), 0, 'missing project offered mutations');
  await page.goto(base + '/scans/999999'); await alert('扫描详情加载失败').waitFor();
  checks.push('Empty project and missing project/scan preserve a usable error state');

  await failThenRetry('/settings/model-configs', '/model-configs', () => page.getByRole('cell', {name: /^Fixture UI 模型/}).waitFor());
  undo = await fault('/model-providers'); await page.reload(); await alert('模型供应商读取失败').waitFor();
  await undo(); await button('重新读取供应商').click();
  await button('添加模型').click(); dialog = page.getByRole('dialog', {name: '添加模型', exact: true});
  await button('添加', dialog).click(); await dialog.getByRole('alert').filter({hasText: '请选择供应商'}).waitFor();
  await dialog.locator('button.provider').filter({hasText: /自定义/}).click();
  await dialog.getByPlaceholder('给这条配置起个名字', {exact: true}).fill('Fixture Web 模型');
  await dialog.getByPlaceholder('sk-…', {exact: true}).fill('fixture-new-never-valid-key');
  await button('显示 API Key', dialog).click();
  assert.equal(await dialog.getByPlaceholder('sk-…', {exact: true}).getAttribute('type'), 'text');
  await button('隐藏 API Key', dialog).click();
  await dialog.getByPlaceholder('https://api.example.com/v1', {exact: true}).fill(process.env.AXIOM_TEST_FRONTEND_PROVIDER);
  await dialog.getByPlaceholder('模型 ID', {exact: true}).fill('fixture-ui-model');
  await button('测试连接', dialog).click(); await dialog.getByText(/SDK 工具和完整复核能力尚未验证/).waitFor();
  await button('获取列表', dialog).click();
  await button('获取列表', dialog).click(); await page.getByRole('menuitem', {name: 'fixture-ui-model', exact: true}).click();
  undo = await fault('/model-configs', 'POST', 503, 'Fixture 模型保存失败');
  await button('添加', dialog).click(); await dialog.getByRole('alert').waitFor();
  assert.equal(await dialog.getByPlaceholder('模型 ID', {exact: true}).inputValue(), 'fixture-ui-model');
  await undo(); await button('添加', dialog).click(); await dialog.waitFor({state: 'hidden'});
  const modelRow = page.getByRole('row').filter({hasText: 'Fixture Web 模型'});
  await modelRow.waitFor(); await modelRow.getByRole('switch').click();
  await page.getByRole('switch', {name: '设为工作模型 Fixture Web 模型', exact: true}).waitFor({state: 'visible'});
  await page.waitForFunction(() => [...document.querySelectorAll('[role=switch][aria-checked=true]')].length === 1);
  await button('编辑', modelRow).click(); dialog = page.getByRole('dialog', {name: '编辑模型配置', exact: true});
  undo = await fault('/model-configs/2/test', 'POST', 500, 'Fixture 连接测试失败');
  await button('测试已存配置',dialog).click(); await dialog.getByRole('alert').filter({hasText:'Fixture 连接测试失败'}).waitFor(); await undo();
  await button('测试已存配置',dialog).click(); await dialog.getByRole('status').filter({hasText:'本次仅验证连通性'}).waitFor();
  const saved = page.waitForRequest(request => request.method() === 'PUT' && /\/api\/v1\/model-configs\/\d+$/.test(new URL(request.url()).pathname));
  await button('保存', dialog).click(); assert.ok(!Object.hasOwn((await saved).postDataJSON(), 'apiKey'));
  await dialog.waitFor({state: 'hidden'});
  await page.getByRole('switch', {name: /设为工作模型 Fixture UI 模型/}).click();
  await button('删除', modelRow).click(); await acknowledge('删除模型配置'); await modelRow.waitFor({state: 'hidden'});
  await page.getByLabel('搜索模型配置',{exact:true}).fill('no-such-fixture-model'); await page.getByText('没有匹配的模型',{exact:true}).waitFor(); await page.getByLabel('搜索模型配置',{exact:true}).fill('');
  await choose('筛选模型状态','未启用'); await page.getByText('没有匹配的模型',{exact:true}).waitFor(); await choose('筛选模型状态','全部状态');
  checks.push('Model presets/load recovery, validation, key reveal, synthetic probe/list, create/edit/retain-key, exclusive enable and delete');

  undo = await fault('/model-configs'); await page.goto(base + '/scans/new');
  await alert('模型配置读取失败').waitFor();
  assert.equal(await page.getByText(/还没有模型配置/).count(), 0, 'network outage was displayed as empty data');
  await page.getByLabel('扫描范围（可选）', {exact: true}).fill('src/fixture.ts');
  assert.ok(await button('创建扫描').isDisabled());
  await undo(); await button('重新读取配置').click(); await combobox('模型配置').waitFor();
  assert.equal(await page.getByLabel('扫描范围（可选）', {exact: true}).inputValue(), 'src/fixture.ts');
  const newSnap = await snapshotUpload();
  undo = await fault('/uploads','POST',413,'Fixture 上传超过限制');
  await page.locator('input[type=file]:not(#git-archive)').setInputFiles({name:'fixture-rejected.zip',mimeType:'application/zip',buffer:await readFile(archive)});
  await page.getByText('Fixture 上传超过限制',{exact:true}).first().waitFor(); await undo();
  assert.ok((await combobox('代码快照').innerText()).includes('fixture-ui-source.zip'));
  assert.equal((await snapshotUpload()).id,newSnap.id);
  checks.push('Asset filters/navigation and upload rejection preserve the chosen immutable source');
  await combobox('归属项目').click(); await page.getByRole('option', {name: /Fixture 前端完整性项目/}).click();
  await button('清除归属项目').focus(); await page.keyboard.press('Enter');
  assert.ok(!(await combobox('归属项目').innerText()).includes('Fixture 前端完整性项目'));
  assert.equal(await page.locator('button button').count(), 0, 'nested buttons break keyboard interaction');
  await page.getByLabel('单次估算上限（USD，可选）', {exact: true}).fill('2');
  await page.getByLabel('任务总预算（USD，可选）', {exact: true}).fill('3');
  undo = await fault('/scans', 'POST'); await button('创建扫描').click(); await alert('Fixture 请求暂不可用').first().waitFor();
  assert.equal(await page.getByLabel('任务总预算（USD，可选）', {exact: true}).inputValue(), '3');
  await undo(); const createdResponse = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/scans');
  await button('创建扫描').click(); const newTask = await responseBody(await createdResponse);
  assert.equal(newTask.sourceSnapshotId, newSnap.id); await page.waitForURL(url => url.pathname === '/scans/' + newTask.id);
  undo = await fault('/scans/' + newTask.id + '/cancel', 'POST');
  await button('取消').click(); await acknowledge('取消扫描'); await alert('取消请求未完成').waitFor(); await undo();
  await button('取消').click(); await acknowledge('取消扫描'); await button('重新执行').waitFor();
  undo = await fault('/scans/' + newTask.id + '/recovery', 'POST'); await button('重新执行').click(); await page.getByLabel('我已知悉这会重新调用模型并可能重复产生费用',{exact:true}).check(); await button('确认重新执行').click(); await alert('重新执行未完成').waitFor(); await undo();
  await button('确认重新执行').click(); await button('取消').waitFor(); await button('取消').click(); await acknowledge('取消扫描'); await button('重新执行').waitFor();
  checks.push('Scan input failure/retry retains fields; select clear; Web create/cancel/retry with persistent failures and no worker');

  await failThenRetry('/scans', '/scans', () => page.getByRole('navigation', {name: '扫描任务分页', exact: true}).getByText(/共/).waitFor());
  await button('下一页').click(); await page.getByText(/第 2 \/ 2 页/).waitFor();
  await page.getByLabel('搜索扫描任务', {exact: true}).fill('no-such-fixture-task');
  await page.getByText('没有匹配的扫描任务', {exact: true}).waitFor();
  await page.getByLabel('搜索扫描任务', {exact: true}).fill(newTask.scanNo);
  await button('删除扫描 ' + newTask.scanNo).click(); dialog = page.getByRole('dialog', {name: '清理与删除预览', exact: true});
  await dialog.getByRole('region', {name: '清理计划', exact: true}).waitFor(); await button('关闭', dialog).click();
  assert.equal(new URL(page.url()).pathname, '/scans', 'row action also navigated the row');
  checks.push('Scan pagination/filter no-match and contextual delete use cleanup without opening the row');

  await failThenRetry('/findings', '/findings', () => page.getByRole('cell', {name: /Fixture 合成证据/}).waitFor());
  await page.getByLabel('搜索漏洞', {exact: true}).fill('no-such-finding'); await page.getByText('暂无匹配漏洞', {exact: true}).waitFor();
  await page.getByLabel('搜索漏洞', {exact: true}).fill('Fixture 合成证据');
  await choose('漏洞状态', '已修复'); await page.getByText('暂无匹配漏洞', {exact: true}).waitFor();
  await button('清除漏洞状态').click(); await page.getByRole('cell', {name: /Fixture 合成证据/}).click();
  await page.waitForURL(url => url.pathname === '/scans/' + history);
  undo = await fault('/scans/' + history + '/findings'); await page.reload(); await alert('漏洞读取失败').waitFor();
  assert.equal(await page.getByText('暂无漏洞', {exact: true}).count(), 0);
  assert.equal(await page.locator('.f-cell .f-val').first().innerText(),'1','confirmed Server total was replaced with an empty failed list');
  assert.equal(await page.locator('.f-cell .f-val').nth(1).innerText(),'—','unavailable severity counts were displayed as zero');
  await undo(); await button('重新读取漏洞').click(); await page.getByRole('heading', {name: 'Fixture 合成证据：界面验收', exact: true}).waitFor();
  undo = await fault('/scans/' + history + '/compare'); await page.reload(); await alert('扫描对比暂不可用').waitFor();
  await undo(); await button('重新读取对比').click(); await page.getByText(/与上次扫描对比/).waitFor();
  undo = await fault('/findings/2/status','PUT',400,'Fixture 状态保存被拒绝'); await choose('漏洞处理状态','已确认'); await alert('漏洞处理状态未保存').waitFor();
  assert.ok((await combobox('漏洞处理状态').innerText()).includes('未处理')); await undo();
  for (const status of ['已确认', '误报', '已修复', '未处理']) await choose('漏洞处理状态', status);
  await page.keyboard.press('Escape'); await button('关闭漏洞证据').waitFor({state: 'hidden'});
  await page.locator('[data-finding-id]').first().press('ArrowDown'); await button('关闭漏洞证据').waitFor();
  await button('原始 JSON（上游完整对象）').click();
  await page.getByRole('region', {name: '原始漏洞 JSON', exact: true}).waitFor();
  await button('原始 JSON（上游完整对象）').click();
  await button('导出').click(); const downloaded = page.waitForEvent('download'); await page.getByRole('menuitem', {name: /HTML/}).click();
  assert.match(await readFile(await (await downloaded).path(), 'utf8'), /Fixture 合成证据/);
  await button('导出').click(); const pdfDownload = page.waitForEvent('download'); await page.getByRole('menuitem', {name: /PDF/}).click();
  assert.equal((await readFile(await (await pdfDownload).path())).subarray(0, 5).toString(), '%PDF-');
  undo = await fault('/scans/' + history + '/report.pdf', 'GET', 501, 'Fixture PDF 渲染不可用');
  await button('导出').click(); await page.getByRole('menuitem', {name: /PDF/}).click(); await page.getByText(/Fixture PDF 渲染不可用/).first().waitFor(); await undo();
  checks.push('Genuine HTML/PDF downloads and JSON-blob 501 download failure');
  const assetDirectory = new URL('../dist/assets/', import.meta.url);
  const monaco = (await Promise.all((await readdir(assetDirectory)).filter(name => /^index-.*\.js$/.test(name)).map(async name => ({name, size: (await stat(new URL(name, assetDirectory))).size})))).find(asset => asset.size > 3_000_000)?.name;
  assert.ok(monaco, 'identify the separately loaded Monaco chunk for failure injection');
  const chunkMatch = url => url.pathname === '/assets/' + monaco;
  await intercept(chunkMatch, route => route.abort('failed'));
  await page.reload(); await button('原始 JSON（上游完整对象）').click(); await alert('代码查看器加载失败').first().waitFor();
  await page.getByRole('code').filter({hasText: 'const fixtureOnly'}).or(page.getByLabel('原始代码文本', {exact: true})).first().waitFor();
  await restore(chunkMatch); await page.reload();
  checks.push('Code viewer chunk failure retains raw evidence without an unhandled exception');
  checks.push('Findings combine live filters; detail read/compare retry; all review states, raw evidence and genuine HTML download');

  await page.goto(base + '/settings?section=account'); await page.getByRole('heading', {name: '登录会话', exact: true}).waitFor();
  await button('修改密码').click(); await button('取消', page.getByRole('dialog', {name: '修改密码', exact: true})).click();
  for (const [section, name] of [['users', '账户管理'], ['operations', '操作日志'], ['about', '关于系统'], ['storage', '存储与清理'], ['execution', '扫描执行']]) {
    await button(name).click(); await page.waitForURL(url => url.searchParams.get('section') === section);
    assert.equal(await button(name).getAttribute('aria-current'), 'page');
  }
  await page.getByText('平台不设置全局、模型或项目并发配额。',{exact:true}).waitFor();
  assert.equal(await page.getByLabel('全局任务并发',{exact:true}).count(),0);
  assert.equal(await button('保存并发配置').count(),0);
  undo = await fault('/executions/overview', 'GET', 503, 'Fixture 执行状态读取失败');
  await button('重新加载').click();await alert('扫描执行状态读取失败').waitFor();
  await page.getByRole('status').filter({hasText:'上次成功的执行快照'}).waitFor();
  await undo();await button('重新加载').click();await page.getByText('平台不设置全局、模型或项目并发配额。',{exact:true}).waitFor();
  checks.push('Settings deep links/tabs, password dismissal, retired quota controls removed and execution read failure preserves snapshot');

  await page.goto(base + '/projects');
  const removeProject = page.getByRole('row').filter({hasText: 'Fixture Web 用户项目'});
  await button('删除记录', removeProject).click(); await acknowledge('删除项目'); await removeProject.waitFor({state: 'hidden'});
  await signIn('fixture-frontend-viewer');
  await page.goto(base + '/'); await page.getByRole('heading', {name: '安全工作台', exact: true}).waitFor();
  assert.equal(await button('新建扫描').count(), 0);
  await page.goto(base + '/projects/' + project); await page.getByRole('heading', {name: 'Fixture 前端完整性项目', exact: true}).waitFor();
  assert.equal(await button('上传代码包').count(), 0);
  await page.goto(base + '/settings?section=users'); await page.getByRole('alert').filter({hasText:'需要管理员权限'}).waitFor();
  assert.equal(await button('账户管理').count(), 0);
  checks.push('Project deletion stays in list; viewer has no mutation actions or administrator deep-link access');

  await signIn();
  // List emptiness and long content use explicit response fixtures, never demo data.
  for (const [route, endpoint, title] of [['/projects', '/projects', '还没有项目'], ['/assets', '/snapshots', '还没有代码快照'], ['/findings', '/findings', '暂无匹配漏洞'], ['/settings/model-configs', '/model-configs', '还没有模型配置']]) {
    const match = url => url.pathname === '/api/v1' + endpoint;
    await intercept(match, request => request.fulfill({contentType: 'application/json', body: JSON.stringify({data: endpoint === '/snapshots' || endpoint === '/findings' ? {items:[],total:0,page:1,pageSize:50,counts:{'':0}} : []})}));
    await page.goto(base + route); await page.getByText(title, {exact: true}).waitFor(); await restore(match);
  }
  checks.push('Empty list states remain distinct from failed reads across projects/assets/findings/models');
  const projectMatch = url => url.pathname === '/api/v1/projects';
  const longName = 'Fixture 极长名称 العربية <script>window.fixtureInjected=true</script> ' + '路径与证据'.repeat(45);
  await intercept(projectMatch, route => route.fulfill({contentType:'application/json',body:JSON.stringify({data:[{id:900001,name:longName,snapshotCount:0,scanCount:0,createdAt:'2026-10-15T00:00:00Z'}]})}));
  await page.goto(base + '/projects'); await page.getByRole('cell', {name:longName,exact:true}).waitFor();
  assert.equal(await page.evaluate(() => window.fixtureInjected), undefined);
  await shoot('projects-long-desktop'); await page.setViewportSize({width:390,height:844}); await shoot('projects-long-mobile'); await page.setViewportSize({width:1440,height:1000});
  await restore(projectMatch);
  checks.push('Long CJK/RTL/script-like content stays text and within viewport; full catalog pagination has separate real-DB coverage');
  await page.goto(base + '/'); await page.setViewportSize({width:390,height:844});
  await button('切换导航').click(); await page.getByRole('complementary',{name:'应用导航',exact:true}).waitFor();
  await page.keyboard.press('Shift+Tab'); assert.ok(await page.locator('.sidebar').evaluate(el=>el.contains(document.activeElement)));
  await page.keyboard.press('Escape'); assert.ok(await button('切换导航').evaluate(el=>el===document.activeElement));
  await page.setViewportSize({width:1440,height:1000});
  checks.push('Mobile navigation excludes hidden controls, contains keyboard focus and returns it on Escape');
  const visualRoutes = [['dashboard', '/'], ['projects', '/projects'], ['project', '/projects/' + project], ['scans', '/scans'], ['findings', '/findings'], ['assets', '/assets'], ['new-scan', '/scans/new'], ['scan-detail', '/scans/' + history], ['models', '/settings/model-configs'], ['settings', '/settings?section=account'], ['not-found', '/not-a-page']];
  for (const [name, route] of visualRoutes) {
    await page.goto(base + route); await page.locator('main h1').waitFor();
    await page.waitForLoadState('networkidle');
    await shoot(name + '-desktop');
    await page.setViewportSize({width: 390, height: 844}); await shoot(name + '-mobile');
    await page.setViewportSize({width: 1440, height: 1000});
    await button('切换深色模式').click(); await shoot(name + '-dark'); await button('切换浅色模式').click();
  }
  await page.goto(base + '/login'); await shoot('login-desktop'); await page.setViewportSize({width: 390, height: 844}); await shoot('login-mobile');
  assert.deepEqual(remote, [], 'UI attempted an external request');
  assert.deepEqual(failures, [], 'unhandled browser exceptions');
  assert.ok(!requests.some(request=>request.method==='DELETE' && /^\/api\/v1\/(scans|snapshots)\/\d+$/.test(request.path)), 'UI fell back to legacy direct deletion');
  const digest = data => createHash('sha256').update(data).digest('hex');
  const report = {status:'passed',completedAt:new Date().toISOString(),webBuildIndexSha256:digest(await readFile(new URL('../dist/index.html',import.meta.url))),testSourceSha256:digest(await readFile(new URL(import.meta.url))),fixtureOnly: true, realModelCalls: 0, pages: 11, checks, screenshots, requests: [...new Map(requests.map(item => [item.method + ' ' + item.path, item])).values()], pending: ['Real SDK/model acceptance excluded by request', 'Native Firefox/WebKit and assistive-technology manual review not certified']};
  if (process.env.AXIOM_TEST_FRONTEND_REPORT_DIR) await writeFile(path.join(process.env.AXIOM_TEST_FRONTEND_REPORT_DIR, 'report.json'), JSON.stringify(report, null, 2) + '\n');
  process.stdout.write(JSON.stringify(report) + '\n');
} catch (error) {
  if (process.env.AXIOM_TEST_FRONTEND_REPORT_DIR) {
    await mkdir(process.env.AXIOM_TEST_FRONTEND_REPORT_DIR,{recursive:true});
    await writeFile(path.join(process.env.AXIOM_TEST_FRONTEND_REPORT_DIR,'report.json'),JSON.stringify({status:'failed',failedAt:new Date().toISOString(),fixtureOnly:true,realModelCalls:0,checks,screenshots},null,2)+'\n');
  }
  process.stderr.write('Completed checks: ' + JSON.stringify(checks) + '\n');
  process.stderr.write('Frontend fixture failure at ' + page.url() + '\n' + (await page.locator('body').innerText()).slice(0, 8000) + '\n');
  throw error;
} finally { await browser.close(); }
