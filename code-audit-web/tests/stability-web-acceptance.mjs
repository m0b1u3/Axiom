// Real production Web/Go services in a fresh synthetic stack; SDK is an offline fixture.
import assert from 'node:assert/strict';
import {chromium} from 'playwright-core';
import {existsSync} from 'node:fs';
import {mkdir, readFile, writeFile} from 'node:fs/promises';
import path from 'node:path';
const base = process.env.AXIOM_TEST_WEB_URL, project = process.env.AXIOM_TEST_STABILITY_PROJECT;
const token = process.env.AXIOM_TEST_STABILITY_TOKEN, phase = process.env.AXIOM_TEST_STABILITY_PHASE;
const sdk = process.env.AXIOM_TEST_STABILITY_SDK;
const ids = JSON.parse(process.env.AXIOM_TEST_STABILITY_IDS || '{}'), out = process.env.AXIOM_TEST_STABILITY_REPORT;
assert.ok(base && out && token && project?.startsWith('axiom-accept-stability-'));
assert.match(sdk ?? '', /^\d+\.\d+\.\d+$/, 'the caller must provide the exact pinned SDK version');
const origin = new URL(base).origin;
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(new URL(base).hostname) && !['', '5173', '5174', '8080'].includes(new URL(base).port));
assert.ok(['running', 'completed', 'interrupt', 'recovery'].includes(phase)); await mkdir(out, {recursive: true});
const executable = process.env.AXIOM_TEST_BROWSER_PATH || (process.platform === 'darwin' ? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' : undefined);
const browser = await chromium.launch({headless: true, ...(executable && existsSync(executable) ? {executablePath: executable} : {})});
const checks = [], errors = [], external = [], forbidden = []; let page;
try {
  const context = await browser.newContext({viewport: {width: 1440, height: 1000}, serviceWorkers: 'block', reducedMotion: 'reduce', acceptDownloads: true});
  await context.addInitScript(value => localStorage.setItem('axiom_token', value), token);
  await context.route('**/*', route => {
    const r = route.request(), u = new URL(r.url());
    if (['http:', 'https:'].includes(u.protocol) && u.origin !== origin) {external.push(u.origin); return route.abort();}
    if (!['GET', 'HEAD'].includes(r.method())) {
      const allowed = (phase === 'running' && r.method() === 'POST' && u.pathname === `/api/v1/scans/${ids.cancel}/cancel`)
        || (phase === 'interrupt' && r.method() === 'POST' && u.pathname === `/api/v1/scans/${ids.fault}/recovery`)
        || (phase === 'recovery' && ((r.method() === 'PUT' && u.pathname === `/api/v1/scans/${ids.fault}/budget/attempts/1`) || (r.method() === 'POST' && u.pathname === `/api/v1/scans/${ids.fault}/recovery`)));
      if (!allowed) {forbidden.push(r.method() + ' ' + u.pathname); return route.abort();}
    }
    return route.continue();
  });
  await context.routeWebSocket('**/*', socket => {forbidden.push('WebSocket'); socket.close();});
  page = await context.newPage(); page.setDefaultTimeout(30000); page.on('pageerror', e => errors.push(e.message));
  const models = await context.request.get(origin + '/api/v1/model-configs', {headers: {Authorization: 'Bearer ' + token}});
  assert.equal(models.status(), 200); assert.ok((await models.json()).data.some(m => m.name === 'Fixtures / Mock stability'));
  async function screenshot(name) {
    await page.evaluate(async () => {document.activeElement?.blur(); await document.fonts.ready; await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)));});
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    await page.screenshot({path: path.join(out, name + '.png')});
  }
  if (phase === 'running') {
    await page.goto(origin + '/settings?section=execution');
    await page.getByText('平台不设置全局、模型或项目并发配额。', {exact: true}).waitFor();
    assert.equal((await page.locator('.queue-capacity strong').allTextContents())[0], String(ids.parallel));
    await screenshot('parallel-running');
    await page.goto(origin + '/scans/' + ids.long); await page.reload();
    await page.getByText(/Fixtures \/ Mock: sustained scan tick/).first().waitFor();
    await screenshot('long-task-running');
    await page.goto(origin + '/scans/' + ids.cancel);
    await page.getByRole('button', {name: '取消', exact: true}).click();
    const canceled = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === `/api/v1/scans/${ids.cancel}/cancel`);
    await page.getByRole('dialog', {name: '取消扫描', exact: true}).getByRole('button', {name: '确定', exact: true}).click();
    const cancelResponse = await canceled;
    assert.equal(cancelResponse.status(), 200);
    assert.ok(['cancel_requested', 'canceled'].includes((await cancelResponse.json()).data.status));
    checks.push('same-model-same-project-parallelism-visible', 'live-activity-survives-route-refresh', 'cancel-through-Web-without-closing-other-tasks');
  } else if (phase === 'completed') {
    await page.goto(origin + '/scans/' + ids.long); await page.reload();
    const archive = page.getByRole('region', {name: '归档结果', exact: true}); await archive.getByText('$1.125', {exact: true}).waitFor();
    const event = page.waitForEvent('download'); await archive.getByRole('button', {name: '下载完整结果 JSON', exact: true}).click();
    const download = await event, value = JSON.parse(await readFile(await download.path(), 'utf8'));
    assert.equal(value.result.extensions.sdk.stability.fixtureOnly, true); assert.equal(value.result.extensions.sdk.stability.inventory.files, Number(process.env.AXIOM_TEST_STABILITY_FILES || 12000));
    await screenshot('completed-large-repository'); await page.setViewportSize({width: 390, height: 844}); await screenshot('completed-mobile');
    checks.push('large-source-result-and-evidence-download-through-static-Web', 'completed-result-route-refresh-and-mobile-layout');
  } else if (phase === 'interrupt') {
    await page.goto(origin + '/scans/' + ids.fault);
    const recovery = page.getByRole('region', {name: '扫描中断与恢复', exact: true});
    await recovery.getByRole('button', {name: /^扫描中断与恢复/}).click();
    await recovery.getByText(`SDK ${sdk} · 此入口不支持断点续扫`, {exact: true}).waitFor();
    // Leave the panel open while the genuine 60-second lease expires. No Agent
    // remains online to poll the queue or mutate the task status for this test.
    const interrupt = recovery.getByRole('button', {name: '登记租约过期中断', exact: true});
    assert.equal(await interrupt.count(), 0);
    await interrupt.waitFor({timeout: 100000});
    const response = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === `/api/v1/scans/${ids.fault}/recovery`);
    await interrupt.click(); assert.equal((await response).status(), 200);
    await screenshot('naturally-expired-lease-visible-in-open-panel');
    checks.push('open-panel-refreshes-after-natural-lease-expiry-with-every-Agent-offline', 'explicit-Web-interruption-does-not-reexecute-the-SDK');
  } else {
    await page.goto(origin + '/scans/' + ids.fault);
    const budget = page.getByRole('region', {name: '任务预算与费用', exact: true});
    await budget.getByText('费用未知 · 累计预算重试受阻', {exact: true}).waitFor();
    assert.ok(await page.getByRole('button', {name: '重新执行', exact: true}).isDisabled());
    await screenshot('unknown-fee-after-agent-crash');
    await budget.getByRole('button', {name: '核对第 1 次费用', exact: true}).click();
    let dialog = page.getByRole('dialog', {name: '核对执行费用', exact: true});
    await dialog.getByLabel('本次执行估算费用（USD）', {exact: true}).fill('0.125');
    await dialog.getByLabel('核对依据', {exact: true}).fill('Fixtures / Mock only: synthetic callback estimate retained after forced Agent death; no supplier bill.');
    await dialog.getByRole('button', {name: '保存人工估算', exact: true}).click(); await dialog.waitFor({state: 'hidden'});
    await page.getByRole('button', {name: '重新执行', exact: true}).click();
    dialog = page.getByRole('dialog', {name: '确认重新执行扫描', exact: true});
    assert.ok(await dialog.getByRole('button', {name: '确认重新执行', exact: true}).isDisabled());
    await dialog.getByLabel('我已知悉这会重新调用模型并可能重复产生费用', {exact: true}).check();
    await dialog.getByRole('button', {name: '确认重新执行', exact: true}).click(); await dialog.waitFor({state: 'hidden'});
    assert.equal(await page.getByText('入队第 0 位', {exact: true}).count(), 0);
    assert.equal(await page.getByText(/受模型或项目配额限制的任务/).count(), 0);
    await screenshot('explicit-new-attempt');
    checks.push('unknown-fee-blocks-Web-retry', 'Web-reconciliation-retains-evidence', 'explicit-reexecution-requires-fee-acknowledgement');
  }
  assert.deepEqual(errors, []); assert.deepEqual(external, []); assert.deepEqual(forbidden, []);
  await writeFile(path.join(out, 'browser.json'), JSON.stringify({status: 'passed', phase, sdkVersion: sdk, checks, fixtureOnly: true, errors, external, forbidden, businessDatabaseAccess: false, realSDKLogins: 0, realAudits: 0, externalPublications: 0}, null, 2) + '\n');
} catch (error) {
  await writeFile(path.join(out, 'browser.json'), JSON.stringify({status: 'failed', phase, sdkVersion: sdk, checks, fixtureOnly: true, error: String(error), errors, external, forbidden, businessDatabaseAccess: false, realSDKLogins: 0, realAudits: 0, externalPublications: 0}, null, 2) + '\n');
  if (page) await page.screenshot({path: path.join(out, 'failure.png'), fullPage: true}).catch(() => {});
  throw error;
}
finally {await browser.close();}
