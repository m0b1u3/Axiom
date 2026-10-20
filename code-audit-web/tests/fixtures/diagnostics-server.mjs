// Fixtures / Mock only. No database, SDK, credentials or writable business API.
import {createServer} from 'node:http';
import {readFile} from 'node:fs/promises';
import {fileURLToPath} from 'node:url';

const readFixture = async name => JSON.parse(await readFile(new URL(`../../../contracts/fixtures/v1/${name}.json`, import.meta.url), 'utf8'));

export async function startDiagnosticsFixture(port = 0) {
  const [runtime, identity, operation, storage] = await Promise.all(
    ['runtime-ready', 'identity-snapshot', 'identity-operation', 'storage-ready'].map(readFixture),
  );
  const now = new Date().toISOString();
  const settings = {global: 2, perModel: 2, perProject: 2, revision: 1, updatedAt: now, updatedBy: 'Fixtures / Mock'};
  const agent = (id, online, state) => ({
    agentId: id, dispatchMode: 'unbounded', slots: 0, lastSeen: now, online,
    runtimeReportedAt: now,
    runtime: {...runtime, agentBuild: 'Fixtures / Mock diagnostics', scannerDescriptionVersion: 12, state,
      diagnostic: state === 'blocked' ? {code: 'protocol_version_unsupported', origin: 'agent', retryable: false,
        message: 'Fixtures / Mock：Scanner 预检被阻止，等待配套组件更新。'} : null},
    inputCapabilities: {assignmentVersions: [5, 6], jobVersions: [7, 8], maxCostUsd: true},
    resultCapabilities: {jobVersions: [2], resultVersions: [1], completionVersions: [1], commitVersions: [1]},
    observationCapabilities: {jobVersions: [7, 8], heartbeatVersions: [1, 2], progressVersions: [2], eventVersions: [2]},
  });
  const agents = [agent('fixtures-ready-agent', true, 'ready'), agent('fixtures-blocked-agent', true, 'blocked'),
    agent('fixtures-offline-agent-with-a-long-identifier-for-diagnostic-wrapping-and-reading', false, 'ready')];
  identity.identity.agentId = agents[0].agentId;
  identity.identity.name = 'Fixtures / Mock · 授权诊断账户';
  identity.checkedAt = now;
  operation.identity = identity.identity;
  operation.status = 'completed';
  operation.action = 'account';
  storage.observedAt = now;
  const overview = {dispatchMode: 'unbounded', onlineAgents: 2, readyAgents: 1, settings, agents,
    tasks: [], active: 0, pending: 0, onlineSlots: 0, effectiveCapacity: null, serverTime: now};
  const state = {fail: new Set(), empty: false, delay: 0, writes: [], requests: [], overview, identity, storage};
  const server = createServer(async (req, res) => {
    const pathname = new URL(req.url, 'http://fixtures.invalid').pathname;
    state.requests.push({method: req.method, pathname});
    res.setHeader('Content-Type', 'application/json; charset=utf-8');
    res.setHeader('Cache-Control', 'no-store');
    const send = (status, body) => {res.writeHead(status); res.end(JSON.stringify(body));};
    if (req.method !== 'GET') {
      state.writes.push({method: req.method, pathname});
      send(405, {message: 'Fixtures / Mock：所有业务写入与 SDK 操作均已禁用。'}); return;
    }
    const routes = {
      '/api/v1/auth/me': {id: 1, username: 'Fixtures / Mock', active: true, role: req.headers.authorization?.includes('viewer') ? 'viewer' : 'admin',
        permissions: req.headers.authorization?.includes('viewer') ? ['read'] : ['read', 'operate', 'admin']},
      '/api/v1/executions/overview': state.empty ? {...overview, agents: []} : overview,
      '/api/v1/settings/execution': {dispatchMode: 'unbounded', settings, canEdit: false,
        history: state.empty ? [] : [{...settings, revision: 2}, settings]},
      '/api/v1/storage': storage,
      '/api/v1/sdk-identities': state.empty ? [] : [identity],
      '/api/v1/sdk-identities/81/operations': [operation],
    };
    if (state.delay && pathname !== '/api/v1/auth/me') await new Promise(resolve => setTimeout(resolve, state.delay));
    if (state.fail.has(pathname)) {send(503, {message: 'Fixtures / Mock：模拟诊断读取中断，请重新读取。'}); return;}
    if (!(pathname in routes)) {send(404, {message: 'Fixtures / Mock：此接口未在隔离诊断环境中提供。'}); return;}
    send(200, {data: routes[pathname]});
  });
  await new Promise(resolve => server.listen(port, '127.0.0.1', resolve));
  return {state, url: `http://127.0.0.1:${server.address().port}`, close: () => new Promise(resolve => server.close(resolve))};
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const fixture = await startDiagnosticsFixture(Number(process.env.FIXTURE_PORT || 18086));
  process.stdout.write(`Fixtures / Mock diagnostics API: ${fixture.url}\n`);
}
