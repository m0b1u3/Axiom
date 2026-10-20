// Read-only browser acceptance with an in-memory Fixtures / Mock API.
// Run: node tests/diagnostics-style-web-acceptance.mjs
import assert from 'node:assert/strict';
import {mkdir, readFile, writeFile} from 'node:fs/promises';
import {existsSync} from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {createServer} from 'vite';
import {chromium} from 'playwright-core';
import {startDiagnosticsFixture} from './fixtures/diagnostics-server.mjs';

const webRoot = fileURLToPath(new URL('../', import.meta.url));
const report = process.env.AXIOM_TEST_DIAGNOSTICS_REPORT_DIR || path.resolve(webRoot, '../output/playwright/diagnostics-style');
await mkdir(report, {recursive: true});
const fixture = await startDiagnosticsFixture();
const vite = await createServer({root: webRoot, logLevel: 'error', server: {host: '127.0.0.1', port: 0, proxy: {'/api': {target: fixture.url}}}});
let browser, page;
const checks = [], errors = [], external = [], writes = [], captures = [], contrastChecks = [];
try {
  await vite.listen();
  const origin = `http://127.0.0.1:${vite.httpServer.address().port}`;
  const executable = process.env.AXIOM_TEST_BROWSER_PATH || (process.platform === 'darwin' ? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' : undefined);
  browser = await chromium.launch({headless: true, ...(executable && existsSync(executable) ? {executablePath: executable} : {})});
  async function context(role = 'admin') {
    const ctx = await browser.newContext({viewport: {width: 1440, height: 1000}, reducedMotion: 'reduce', serviceWorkers: 'block', acceptDownloads: true});
    await ctx.addInitScript(role => {
      localStorage.setItem('axiom_token', 'fixtures-only-' + role);
      localStorage.setItem('axiom_theme', 'light');
      Object.defineProperty(navigator, 'clipboard', {configurable: true, value: {
        writeText: async text => {window.__fixtureClipboard = text;},
      }});
    }, role);
    await ctx.route('**/*', async route => {
      const req = route.request(), url = new URL(req.url());
      if (url.origin !== origin) {external.push(req.url()); await route.abort(); return;}
      if (url.pathname.startsWith('/api/') && req.method() !== 'GET') {writes.push(req.method() + ' ' + url.pathname); await route.abort(); return;}
      await route.continue();
    });
    const p = await ctx.newPage();
    p.on('pageerror', e => errors.push(e.message));
    return {ctx, p};
  }
  const admin = await context();
  page = admin.p;
  const diagnosticsURL = origin + '/settings?section=diagnostics';
  const title = name => page.getByRole('heading', {name, exact: true});
  const snapshot = name => page.locator('details.diagnostic-snapshot').filter({has: page.locator('.snapshot-title', {hasText: name})});
  const shot = async name => {
    await page.evaluate(async () => {document.activeElement?.blur(); await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)));});
    await page.screenshot({path: path.join(report, name + '.png'), fullPage: true});
    await page.screenshot({path: path.join(report, name + '-viewport.png')});
    captures.push(name);
  };
  async function theme(dark) {
    const narrow = page.viewportSize().width <= 760;
    if (narrow) await page.getByRole('button', {name: '切换导航', exact: true}).click();
    await page.getByRole('button', {name: dark ? '切换深色模式' : '切换浅色模式', exact: true}).click();
    await page.waitForFunction(dark => document.documentElement.classList.contains('dark') === dark && document.documentElement.style.colorScheme === (dark ? 'dark' : 'light'), dark);
    await page.evaluate(() => new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r))));
    if (narrow) await page.keyboard.press('Escape');
  }
  async function checkReaderContrast(mode) {
    const colors = await snapshot('调度原始快照').evaluate(el => {
      const content = getComputedStyle(el.querySelector('.snapshot-content')).backgroundColor;
      const button = el.querySelector('[aria-pressed=true]');
      return [
        {name: 'raw', foreground: getComputedStyle(el.querySelector('pre')).color, background: content},
        {name: 'toolbar', foreground: getComputedStyle(el.querySelector('.snapshot-format')).color, background: content},
        {name: 'selected-wrap', foreground: getComputedStyle(button).color, background: getComputedStyle(button).backgroundColor},
      ];
    });
    const luminance = value => value.match(/[\d.]+/g).slice(0, 3).map(Number).map(v => v / 255).map(v => v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4).reduce((sum, v, i) => sum + v * [.2126, .7152, .0722][i], 0);
    for (const item of colors) {
      const a = luminance(item.foreground), b = luminance(item.background);
      const ratio = (Math.max(a, b) + .05) / (Math.min(a, b) + .05);
      assert.ok(ratio >= 4.5, mode + ' ' + item.name + ' text contrast');
      contrastChecks.push({mode, ...item, ratio: Number(ratio.toFixed(2))});
    }
  }
  const noOverflow = async () => assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth && scrollX === 0), 'document overflows horizontally');
  async function ready() {
    await title('高级诊断').waitFor();
    await page.getByRole('table', {name: '执行节点兼容状态', exact: true}).waitFor();
    await page.getByText('Fixtures / Mock · 授权诊断账户', {exact: true}).waitFor();
    await page.evaluate(() => document.fonts.ready);
  }
  await page.goto(diagnosticsURL); await ready();
  assert.equal(await page.locator('details.diagnostic-snapshot[open]').count(), 0);
  for (const state of ['在线', '离线', '预检被阻止']) await page.locator('.state-label').filter({hasText: new RegExp('^' + state + '$')}).waitFor();
  await page.getByText('这些记录仅供历史追溯；平台并发配额已停用，不再参与当前调度。', {exact: true}).waitFor();
  checks.push('collapsed-raw-evidence-and-online-offline-blocked-history-semantics');

  // Exercise each evidence reader against the exact API data, including historical fields.
  for (const [name, expected, filename] of [
    ['节点原始上报', fixture.state.overview.agents, 'execution-nodes.json'],
    ['调度原始快照', fixture.state.overview, 'dispatch-snapshot.json'],
    ['授权原始快照', fixture.state.identity, 'authorization-snapshot.json'],
    ['原始存储快照', fixture.state.storage, 'storage-snapshot.json'],
  ]) {
    const reader = snapshot(name), summary = reader.locator('summary');
    await summary.focus(); await page.keyboard.press('Enter');
    const raw = reader.locator('pre'); await raw.waitFor();
    assert.equal(await raw.textContent(), JSON.stringify(expected, null, 2));
    assert.equal(await summary.getAttribute('aria-controls'), await reader.locator('.snapshot-content').getAttribute('id'));
    await reader.getByRole('button', {name: '复制' + name, exact: true}).click();
    await reader.getByRole('status').filter({hasText: '已复制完整 JSON。'}).waitFor();
    assert.equal(await page.evaluate(() => window.__fixtureClipboard), JSON.stringify(expected, null, 2));
    const downloaded = page.waitForEvent('download');
    await reader.getByRole('button', {name: '下载' + name, exact: true}).click();
    const download = await downloaded;
    assert.equal(download.suggestedFilename(), filename);
    assert.deepEqual(JSON.parse(await readFile(await download.path(), 'utf8')), expected);
    await summary.focus(); await page.keyboard.press('Space');
    assert.equal(await reader.getAttribute('open'), null);
  }
  checks.push('all-four-raw-readers-retain-complete-JSON-copy-download-and-keyboard-disclosure');

  const nodeReader = snapshot('节点原始上报');
  await nodeReader.locator('summary').click();
  const raw = nodeReader.locator('pre'), wrap = nodeReader.getByRole('button', {name: '节点原始上报自动换行', exact: true});
  assert.equal(await wrap.getAttribute('aria-pressed'), 'true');
  await wrap.click(); assert.equal(await wrap.getAttribute('aria-pressed'), 'false');
  await raw.focus(); await page.keyboard.press('PageDown');
  await page.waitForFunction(el => el.scrollTop > 0, await raw.elementHandle());
  await page.evaluate(() => {navigator.clipboard.writeText = async () => {throw new DOMException('Fixtures / Mock denied', 'NotAllowedError');};});
  await nodeReader.getByRole('button', {name: '复制节点原始上报', exact: true}).click();
  await nodeReader.getByRole('status').filter({hasText: '无法访问剪贴板'}).waitFor();
  assert.deepEqual(JSON.parse(await raw.textContent()), fixture.state.overview.agents);
  await page.evaluate(() => {window.__originalCreateObjectURL = URL.createObjectURL; URL.createObjectURL = () => {throw new Error('Fixtures / Mock download denied');};});
  await nodeReader.getByRole('button', {name: '下载节点原始上报', exact: true}).click();
  await nodeReader.getByRole('status').filter({hasText: '下载未能开始'}).waitFor();
  await page.evaluate(() => {URL.createObjectURL = window.__originalCreateObjectURL;});
  await wrap.click();
  checks.push('local-raw-keyboard-scroll-wrap-toggle-and-copy-download-failure-recovery');

  // Refresh failures retain evidence rather than replacing it with fabricated current state.
  const endpoints = ['/api/v1/executions/overview', '/api/v1/settings/execution', '/api/v1/storage', '/api/v1/sdk-identities'];
  endpoints.forEach(p => fixture.state.fail.add(p));
  await page.getByRole('button', {name: '刷新运行诊断', exact: true}).click();
  await page.getByRole('button', {name: '刷新授权诊断', exact: true}).click();
  for (const text of ['执行节点诊断读取失败', '配置变更读取失败', '存储诊断不可用', '授权诊断读取失败']) await page.getByRole('alert').filter({hasText: text}).waitFor();
  for (const text of ['上次成功的节点快照', '上次成功的存储快照', '上次成功的授权快照']) await page.getByRole('status').filter({hasText: text}).waitFor();
  await shot('errors-retain-evidence');
  fixture.state.fail.clear();
  await page.getByRole('button', {name: '刷新运行诊断', exact: true}).click();
  await page.getByRole('button', {name: '刷新授权诊断', exact: true}).click();
  await page.waitForFunction(() => document.querySelectorAll('[role=alert]').length === 0);
  checks.push('independent-read-failures-retain-node-storage-authorization-evidence-and-retry');

  // One batched visual inspection: wide/intermediate/mobile, light/dark, long identifiers and raw content.
  await page.reload(); await ready(); await noOverflow();
  await shot('desktop-light');
  await snapshot('调度原始快照').locator('summary').click();
  await snapshot('授权原始快照').locator('summary').click();
  await checkReaderContrast('light');
  await shot('desktop-expanded');
  const operations = page.locator('details').filter({has: page.locator('summary', {hasText: /^授权操作与凭据版本$/})});
  await operations.locator('summary').click();
  await page.getByRole('table', {name: '授权操作版本记录', exact: true}).getByRole('cell', {name: '已完成', exact: true}).waitFor();
  assert.match(await page.getByRole('link', {name: '返回此授权账户', exact: true}).getAttribute('href'), /identity=81/);
  await operations.locator('summary').click();
  await page.getByText('手动指定执行位置', {exact: true}).click();
  await page.locator('#manual-authorization-agent').fill('Fixtures-manual-agent');
  assert.equal(await page.getByRole('button', {name: '使用此执行位置添加授权', exact: true}).isEnabled(), true);
  await snapshot('调度原始快照').locator('pre').scrollIntoViewIfNeeded();
  assert.ok(await page.getByRole('navigation', {name: '设置分类', exact: true}).evaluate(el => el.getBoundingClientRect().top >= 0));
  await shot('desktop-scrolled');
  await theme(true); await checkReaderContrast('dark'); await shot('desktop-dark');
  await theme(false);
  for (const [width, height, name] of [[900, 1000, 'intermediate-light'], [390, 844, 'mobile-light'], [320, 760, 'mobile-compact']]) {
    await page.setViewportSize({width, height}); await page.evaluate(() => scrollTo(0, 0)); await noOverflow();
    await shot(name);
    await snapshot('调度原始快照').locator('summary').scrollIntoViewIfNeeded(); await noOverflow();
    await shot(name + '-expanded');
    assert.ok(await snapshot('调度原始快照').locator('pre').evaluate(el => el.clientHeight <= 280));
  }
  await page.setViewportSize({width: 390, height: 844});
  await theme(true); await shot('mobile-dark');
  const table = page.getByRole('region', {name: '执行节点数据表，可横向滚动', exact: true});
  await table.focus(); await page.keyboard.press('ArrowRight');
  await page.waitForFunction(el => el.scrollLeft > 0, await table.elementHandle()); await noOverflow();
  checks.push('wide-sticky-settings-nav-intermediate-390-and-320-local-scroll-light-dark');

  fixture.state.delay = 600;
  await page.reload();
  await page.getByRole('status').filter({hasText: '正在读取执行节点'}).waitFor();
  await page.getByRole('button', {name: '刷新运行诊断', exact: true}).isDisabled().then(value => assert.equal(value, true));
  await ready(); fixture.state.delay = 0;
  fixture.state.empty = true;
  await page.reload();
  await page.getByText('尚未收到执行节点上报。请确认执行服务已启动并连接到 Server。', {exact: true}).waitFor();
  await page.getByText('尚无授权账户。', {exact: true}).waitFor();
  await page.getByText('尚无配置变更记录。', {exact: true}).waitFor();
  await shot('empty-mobile'); fixture.state.empty = false;
  checks.push('initial-pending-disabled-refresh-and-honest-empty-node-identity-history-states');

  const viewer = await context('viewer'); page = viewer.p;
  await page.goto(diagnosticsURL); await ready();
  assert.equal(await page.getByText('手动指定执行位置', {exact: true}).count(), 0);
  assert.equal(await page.getByText('授权操作与凭据版本', {exact: true}).count(), 0);
  assert.equal(await page.getByRole('navigation', {name: '设置分类', exact: true}).getByRole('button', {name: '账户管理', exact: true}).count(), 0);
  await page.goto(diagnosticsURL + '#storage-diagnostics');
  await title('存储诊断').waitFor(); await snapshot('原始存储快照').locator('summary').click();
  await snapshot('原始存储快照').locator('pre').filter({hasText: '"automaticHistoryDeletion": false'}).waitFor();
  checks.push('viewer-read-permissions-manual-binding-admin-boundary-and-storage-bookmark');

  assert.deepEqual(errors, []); assert.deepEqual(external, []); assert.deepEqual(writes, []); assert.deepEqual(fixture.state.writes, []);
  await writeFile(path.join(report, 'browser.json'), JSON.stringify({status: 'passed', checks, captures, contrastChecks, fixtureOnly: true,
    errors, external, writes, businessDatabaseAccess: false, sdkVersion: '0.1.25', realAudits: 0, realSDKLogins: 0, realPlanningModelCalls: 0, externalPublications: 0}, null, 2) + '\n');
  process.stdout.write(JSON.stringify({status: 'passed', checks}) + '\n');
} catch (error) {
  await writeFile(path.join(report, 'browser-failure.json'), JSON.stringify({status: 'failed', checks, error: String(error), errors, external, writes}, null, 2) + '\n');
  if (page) {await page.screenshot({path: path.join(report, 'failure.png'), fullPage: true}).catch(() => {}); process.stderr.write((await page.locator('body').innerText()).slice(-5000) + '\n');}
  throw error;
} finally {
  await browser?.close(); await vite.close(); await fixture.close();
}
