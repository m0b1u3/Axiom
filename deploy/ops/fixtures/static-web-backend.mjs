// Fixtures / Mock only. Runs on an internal Docker network; no SDK or database.
import {createServer} from 'node:http';
import {createHash} from 'node:crypto';
import {readFile} from 'node:fs/promises';

const fixture = async name => JSON.parse(await readFile(`/fixtures/contracts/${name}.json`, 'utf8'));
const [storage, identity, runtime] = await Promise.all(['storage-ready', 'identity-snapshot', 'runtime-ready'].map(fixture));
const observedAt = '2026-10-20T00:00:00Z';
const settings = {global: 2, perModel: 2, perProject: 2, revision: 1, updatedAt: observedAt, updatedBy: 'Fixtures / Mock'};
const overview = {dispatchMode: 'unbounded', onlineAgents: 1, readyAgents: 1, settings,
  agents: [{agentId: 'fixtures-static-agent', slots: 0, dispatchMode: 'unbounded', online: true, lastSeen: observedAt,
    runtimeReportedAt: observedAt, runtime: {...runtime, agentBuild: 'Fixtures / Mock static Web', scannerDescriptionVersion: 12},
    inputCapabilities: {assignmentVersions: [5, 6], jobVersions: [7, 8], maxCostUsd: true},
    resultCapabilities: {jobVersions: [2], resultVersions: [1], completionVersions: [1], commitVersions: [1]},
    observationCapabilities: {jobVersions: [7, 8], heartbeatVersions: [1, 2], progressVersions: [2], eventVersions: [2]}}],
  tasks: [], active: 0, pending: 0, onlineSlots: 0, effectiveCapacity: null, serverTime: observedAt};
identity.identity.name = 'Fixtures / Mock static Web identity';
identity.identity.agentId = 'fixtures-static-agent';
const requests = [];
let ready = true, uploadStarted = false;
const generation = process.env.FIXTURE_GENERATION || 'A';
const report = Buffer.from(JSON.stringify({fixture: 'Fixtures / Mock 静态报告', evidence: 'source → sink', cost: null}) + '\n');
createServer((req, res) => {
  const url = new URL(req.url, 'http://fixtures.invalid');
  requests.push({method: req.method, path: url.pathname});
  const json = (status, value) => {res.writeHead(status, {'Content-Type': 'application/json', 'Cache-Control': 'no-store'}); res.end(JSON.stringify(value));};
  if (url.pathname === '/__control') {ready = url.searchParams.get('ready') !== 'false'; json(200, {fixture: true}); return;}
  if (url.pathname === '/healthz') {json(200, {status: 'alive', fixture: true}); return;}
  if (url.pathname === '/readyz') {json(ready ? 200 : 503, {status: ready ? 'ready' : 'unavailable', fixture: true}); return;}
  if (url.pathname === '/api/fixture/state') {json(200, {generation, requests, uploadStarted}); return;}
  if (url.pathname === '/api/fixture/events') {
    res.writeHead(200, {'Content-Type': 'text/event-stream', 'Cache-Control': 'no-store'}); res.flushHeaders();
    res.write('event: progress\ndata: {"fixture":"Fixtures / Mock","phase":"collecting"}\n\n');
    const timer = setTimeout(() => {res.end('event: completed\ndata: {"fixture":true}\n\n');}, 3000);
    res.on('close', () => clearTimeout(timer)); return;
  }
  if (url.pathname === '/api/fixture/report') {
    res.writeHead(200, {'Content-Type': 'application/json', 'Content-Disposition': 'attachment; filename="fixture-report.json"', 'Content-Length': report.length});res.end(report);return;
  }
  if (url.pathname === '/api/fixture/disconnect') {req.socket.destroy(); return;}
  if (url.pathname === '/api/fixture/upload' && req.method === 'POST') {
    let bytes = 0; const hash = createHash('sha256');
    req.on('data', chunk => {uploadStarted = true; bytes += chunk.length; hash.update(chunk);});
    req.on('end', () => json(200, {fixture: true, bytes, sha256: hash.digest('hex')})); return;
  }
  if (req.method !== 'GET' && req.method !== 'HEAD') {json(405, {message: 'Fixtures / Mock：没有登录、审计或发布接口。'}); return;}
  if (!req.headers.authorization?.startsWith('Bearer fixtures-static-')) {json(401, {message: 'Fixtures / Mock：仅接受模拟身份。'}); return;}
  const routes = {
    '/api/v1/auth/me': {id: 1, username: 'Fixtures / Mock', role: 'admin', active: true, permissions: ['read', 'operate', 'admin']},
    '/api/v1/executions/overview': overview,
    '/api/v1/settings/execution': {dispatchMode: 'unbounded', settings, canEdit: false, history: []},
    '/api/v1/storage': storage,
    '/api/v1/sdk-identities': [identity],
    '/api/v1/sdk-identities/81/operations': [],
  };
  if (url.pathname in routes) {json(200, {data: routes[url.pathname]}); return;}
  json(404, {message: 'Fixtures / Mock：接口未提供。'});
}).listen(8080, '0.0.0.0');
