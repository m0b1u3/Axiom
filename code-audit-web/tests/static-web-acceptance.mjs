// Real static Web → Nginx → isolated mock backend. No page.route API substitution or real login.
import assert from 'node:assert/strict';
import {existsSync} from 'node:fs';
import {mkdir, readFile, writeFile} from 'node:fs/promises';
import path from 'node:path';
import {chromium} from 'playwright-core';

const base = process.env.AXIOM_TEST_STATIC_WEB_URL;
assert.ok(base && ['127.0.0.1', 'localhost', '[::1]'].includes(new URL(base).hostname), 'Requires isolated loopback static Web');
const origin = new URL(base).origin, out = process.env.AXIOM_TEST_STATIC_WEB_REPORT_DIR || path.resolve('../output/playwright/static-web');
await mkdir(out, {recursive: true});
const executable = process.env.AXIOM_TEST_BROWSER_PATH || (process.platform === 'darwin' ? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' : undefined);
const browser = await chromium.launch({headless: true, ...(executable && existsSync(executable) ? {executablePath: executable} : {})});
const checks = [], errors = [], forbidden = [], external = []; let page;
try {
  const context = await browser.newContext({viewport: {width: 1440, height: 1000}, reducedMotion: 'reduce', serviceWorkers: 'block', acceptDownloads: true});
  await context.addInitScript(() => {localStorage.setItem('axiom_token', 'fixtures-static-admin'); localStorage.setItem('axiom_theme', 'light');});
  await context.route('**/*', route => {
    const req = route.request(), u = new URL(req.url());
    if (u.origin !== origin) {external.push(u.origin); return route.abort();}
    if (!['GET', 'HEAD'].includes(req.method())) {forbidden.push(req.method() + ' ' + u.pathname); return route.abort();}
    return route.continue();
  });
  await context.routeWebSocket('**/*', socket => {forbidden.push('WebSocket'); socket.close();});
  page = await context.newPage(); page.setDefaultTimeout(20000); page.on('pageerror', e => errors.push(e.message));
  const raw = title => page.locator('details.diagnostic-snapshot').filter({has: page.locator('.snapshot-title', {hasText: title})});
  async function shot(name) {
    await page.evaluate(async () => {document.activeElement?.blur(); await document.fonts.ready; await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)));});
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    await page.screenshot({path: path.join(out, name + '.png')});
  }
  await page.goto(origin + '/settings?section=diagnostics');
  await page.getByRole('table', {name: '执行节点兼容状态', exact: true}).waitFor();
  await page.getByText('Fixtures / Mock static Web identity', {exact: true}).waitFor();
  await page.reload(); await page.getByRole('table', {name: '执行节点兼容状态', exact: true}).waitFor();
  checks.push('static-SPA-direct-route-refresh-and-real-Nginx-proxy-with-mock-identity');
  for (const title of ['节点原始上报', '调度原始快照', '授权原始快照', '原始存储快照']) {
    const reader = raw(title); await reader.locator('summary').focus(); await page.keyboard.press('Enter'); await reader.locator('pre').waitFor();
    const value = JSON.parse(await reader.locator('pre').textContent());
    const event = page.waitForEvent('download'); await reader.getByRole('button', {name: '下载' + title, exact: true}).click();
    assert.deepEqual(JSON.parse(await readFile(await (await event).path(), 'utf8')), value);
    await reader.locator('summary').focus(); await page.keyboard.press('Space');
  }
  checks.push('four-raw-readers-keyboard-and-JSON-download-on-compiled-static-bundle');
  await raw('调度原始快照').locator('summary').click(); await raw('调度原始快照').locator('pre').scrollIntoViewIfNeeded();
  await shot('static-desktop'); await page.getByRole('button', {name: '切换深色模式', exact: true}).click();
  await page.waitForFunction(() => document.documentElement.classList.contains('dark')); await shot('static-dark');
  await page.setViewportSize({width: 390, height: 844}); await raw('调度原始快照').locator('summary').scrollIntoViewIfNeeded(); await shot('static-mobile');
  checks.push('compiled-styles-desktop-dark-mobile-and-local-reader-overflow');
  const download = page.waitForEvent('download');
  await page.evaluate(() => {const link = document.createElement('a'); link.href = '/api/fixture/report'; document.body.append(link); link.click(); link.remove();});
  const file = await download; assert.equal(file.suggestedFilename(), 'fixture-report.json');
  assert.equal(JSON.parse(await readFile(await file.path(), 'utf8')).fixture, 'Fixtures / Mock 静态报告');
  checks.push('proxied-report-attachment-name-UTF8-content-and-browser-download');
  const streamed = await page.evaluate(async () => {
    const started = performance.now(), response = await fetch('/api/fixture/events'), reader = response.body.getReader();
    const first = await reader.read(), firstMs = performance.now() - started; let text = new TextDecoder().decode(first.value);
    while (true) {const next = await reader.read(); if (next.done) break; text += new TextDecoder().decode(next.value);}
    return {firstMs, text};
  });
  assert.ok(streamed.firstMs < 2500, 'SSE was buffered until completion'); assert.match(streamed.text, /event: progress/); assert.match(streamed.text, /event: completed/);
  checks.push('browser-fetch-observes-progress-before-stream-completion');
  assert.deepEqual(errors, []); assert.deepEqual(forbidden, []); assert.deepEqual(external, []);
  const report = {status: 'passed', checks, errors, forbidden, external, fixtureOnly: true, actualProxy: true, realConsoleLogins: 0, realSDKLogins: 0, realAudits: 0, externalPublications: 0};
  await writeFile(path.join(out, 'browser.json'), JSON.stringify(report, null, 2) + '\n'); process.stdout.write(JSON.stringify(report) + '\n');
} catch (error) {if (page) await page.screenshot({path: path.join(out, 'failure.png'), fullPage: true}).catch(() => {}); throw error;}
finally {await browser.close();}
