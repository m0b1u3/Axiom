import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';

const source = fs.readFileSync(new URL('../src/views/ScanDetail.vue', import.meta.url), 'utf8');
function harness(task = {}, compareScan = async () => ({previousScanId: 9, added: [], removed: [], unchanged: 1})) {
  const ref = value => ({value});
  const requests = [];
  const context = {
    task: ref({id: 10, status: 'completed', ...task}), id: 10,
    selectedAttempt: ref('current'), historicalAttempt: ref(undefined),
    computed: read => Object.defineProperty({}, 'value', {get: read}),
    compare: ref(null), compareError: ref(''), findings: ref([]), selected: ref(null),
    findLoading: ref(false), findingsError: ref(''), shallowReactive: value => value,
    focusFindingId: 0, focusedOnce: false, findingsGeneration: 0, loadGeneration: 0, disposed: false,
    api: {
      listFindings: async () => {requests.push('findings'); return [{id: 7, title: 'Fixtures / Mock archived finding'}];},
      attemptResult: async () => {requests.push('historical'); return {findings: []};},
      compareScan: async () => {requests.push('compare'); return compareScan();},
    },
  };
  vm.createContext(context);
  const eligibility = source.match(/^const comparisonAvailable = .*;$/m)?.[0];
  assert.ok(eligibility);
  const functions = source.slice(source.indexOf('async function loadFindings()'), source.indexOf('\nconst terminal ='));
  vm.runInContext(ts.transpile(eligibility + '\n' + functions + '\nglobalThis.readFindings=loadFindings;globalThis.readCompare=loadCompare;'), context);
  return {context, requests};
}

for (const task of [{sdkOperation: {kind: 'validate'}, interimReviewGroupId: 7}, {sdkOperation: {kind: 'preflight'}}, {componentPlanning: {kind: 'suggest'}}]) {
  test(`${task.sdkOperation?.kind ?? 'component planning'} reads findings without requesting an unsupported comparison`, async () => {
    const {context, requests} = harness(task);
    await context.readFindings(); await context.readCompare();
    assert.deepEqual(requests, ['findings']);
    assert.equal(context.findings.value[0].id, 7);
    assert.equal(context.compare.value, null); assert.equal(context.compareError.value, '');
  });
}
test('ordinary scans still load comparison results', async () => {
  const {context, requests} = harness(); await context.readFindings(); await new Promise(setImmediate);
  assert.deepEqual(requests, ['findings', 'compare']); assert.equal(context.compare.value.previousScanId, 9);
});
test('an ordinary comparison failure remains visible and supports an explicit read retry', async () => {
  let fail = true;
  const {context, requests} = harness({}, async () => {if (fail) throw new Error('Fixtures / Mock 503'); return {previousScanId: 9};});
  await context.readFindings(); await new Promise(setImmediate);
  assert.match(context.compareError.value, /^扫描对比读取失败：.*503$/);
  assert.deepEqual(requests, ['findings', 'compare']);
  fail = false; await context.readCompare();
  assert.equal(context.compareError.value, ''); assert.equal(context.compare.value.previousScanId, 9);
});
test('historical selection neither fetches current comparison nor accepts an outstanding response', async () => {
  let resolve;
  const {context, requests} = harness({}, () => new Promise(done => {resolve = done;}));
  const pending = context.readCompare();
  context.selectedAttempt.value = '1'; context.historicalAttempt.value = {attempt: 1};
  resolve({previousScanId: 9}); await pending;
  assert.equal(context.compare.value, null);
  await context.readFindings(); await context.readCompare();
  assert.deepEqual(requests, ['compare', 'historical']);
});
