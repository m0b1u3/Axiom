// Fixtures / Mock only: no model, authentication, or external network calls.
import {CodexSecurity as FixtureSDK, ScanInterruptedError, pricedAcceptanceUsage} from '../../../contracts/tests/fixtures/input-sdk.mjs';
export * from '../../../contracts/tests/fixtures/input-sdk.mjs';
import {readFile, writeFile, mkdir, readdir, appendFile} from 'node:fs/promises';
import {createHash} from 'node:crypto';
import {spawn} from 'node:child_process';
import path from 'node:path';

const digest = bytes => createHash('sha256').update(bytes).digest('hex');
const delay = (ms, signal) => new Promise((resolve, reject) => {
  const abort = () => {clearTimeout(timer); reject(new ScanInterruptedError('Fixtures / Mock: interrupted stability execution'));};
  const timer = setTimeout(() => {signal?.removeEventListener('abort', abort); resolve();}, ms);
  signal?.addEventListener('abort', abort, {once: true}); if (signal?.aborted) abort();
});

async function sourceInventory(root) {
  const names = [];
  async function walk(folder, prefix = '') {
    for (const entry of await readdir(folder, {withFileTypes: true})) {
      const name = prefix + entry.name;
      if (entry.isDirectory()) await walk(path.join(folder, entry.name), name + '/');
      else if (entry.isFile() && name !== 'fixture-manifest.json') names.push(name);
      else if (!entry.isFile()) throw new Error('Stability source contains a non-file entry');
    }
  }
  await walk(root); names.sort(); let bytes = 0; const tree = createHash('sha256');
  for (const name of names) {const content = await readFile(path.join(root, name)); bytes += content.length; tree.update(name + '\0' + digest(content) + '\n');}
  return {files: names.length, bytes, sha256: tree.digest('hex')};
}

export class CodexSecurity extends FixtureSDK {
  async run(source, options) {
    if (process.env.AXIOM_STABILITY_FIXTURE !== '1') throw new Error('Stability fixture is not enabled');
    const output = path.dirname(path.dirname(options.outputDir));
    const job = JSON.parse(await readFile(path.join(output, 'runtime/job.json'), 'utf8'));
    const taskId = String(job.execution.taskId), attempt = job.execution.attempt;
    const root = path.dirname(process.env.AXIOM_INPUT_TEST_CAPTURE);
    const directory = path.join(root, `${taskId}-${attempt}`);
    await mkdir(directory, {recursive: true});
    // Each Node process owns its capture. A shared writeFile target would race
    // when the parallel fixtures return together.
    process.env.AXIOM_INPUT_TEST_CAPTURE = path.join(directory, 'sdk.json');
    await appendFile(path.join(root, 'invocations.jsonl'), JSON.stringify({taskId, attempt, pid: process.pid}) + '\n');
    const manifest = JSON.parse(await readFile(path.join(source, 'fixture-manifest.json'), 'utf8'));
    const inventory = await sourceInventory(source);
    if (!manifest.fixtureOnly || JSON.stringify(inventory) !== JSON.stringify(manifest.inventory)) throw new Error('Immutable synthetic source changed');
    const control = async () => JSON.parse(await readFile('/opt/axiom/stability-control/config.json', 'utf8'));
    const settings = await control();
    if (!Number.isSafeInteger(settings.sessionBurst) || settings.sessionBurst < 1) throw new Error('Stability control missing');
    const started = Date.now(); let emitted = 0, tick = 0;
    const event = payload => {emitted++; options.onSessionEvent?.({threadId: `fixture-stability-${taskId}-${attempt}`, parentThreadId: null, worker: 1, event: {type: 'response_item', payload: {fixtureOnly: true, profile: 'stability', ...payload}}});};
    const child = spawn(process.execPath, ['-e', 'setInterval(()=>{},1000)'], {stdio: 'ignore'});
    await writeFile(path.join(directory, 'started.json'), JSON.stringify({fixtureOnly: true, taskId, attempt, scannerPid: process.pid, childPid: child.pid, startedAt: new Date(started).toISOString(), inventory,
      leakedCredentials: ['AXIOM_DATABASE_URL', 'AXIOM_AGENT_TOKEN', 'AXIOM_MASTER_KEY', 'AXIOM_JWT_SECRET'].some(name => process.env[name] !== undefined)}), {flag: 'wx'});
    options.onWorkerStatus?.({kind: 'preflight', delegation: 'available', configuredSlots: 1});
    options.onProgress?.({phase: 'discovery', filesTotal: inventory.files, filesCompleted: 0});
    options.onUsage?.(pricedAcceptanceUsage(125000));
    try {
      for (let i = 0; i < settings.sessionBurst; i++) event({index: i, type: 'synthetic_evidence', authorization: 'Bearer fixture-stability-private-token'});
      for (let i = 0; i < 520; i++) options.onActivity?.({id: `fixture-burst-${i}`, kind: 'tool', status: 'completed', description: 'Fixtures / Mock: synthetic source inspection ' + i, paths: ['src/file-000000.ts'], worker: 1});
      while (!(await control()).releaseAll) {
        await delay(1000, options.signal); tick++;
        event({type: 'synthetic_tick', tick});
        options.onActivity?.({id: `fixture-tick-${tick}`, kind: 'tool', status: 'running', description: 'Fixtures / Mock: sustained scan tick ' + tick, worker: 1});
        await writeFile(path.join(directory, 'tick.json'), JSON.stringify({tick, emitted, elapsedMs: Date.now() - started}));
        if (Date.now() - started > settings.maximumSeconds * 1000) throw new Error('Fixture control expired without explicit release');
      }
      const result = await super.run(source, options);
      const value = result.toJSON(); value.stability = {fixtureOnly: true, taskId, attempt, inventory, emitted, elapsedMs: Date.now() - started};
      await writeFile(path.join(directory, 'finished.json'), JSON.stringify(value.stability));
      return {...result, toJSON: () => value};
    } finally {
      child.kill('SIGKILL');
    }
  }
  async loginApiKey() {throw new Error('Authentication is outside the stability fixture');}
  async loginChatGPTDeviceCode() {throw new Error('Authentication is outside the stability fixture');}
  async account() {throw new Error('Authentication is outside the stability fixture');}
  async logout() {throw new Error('Authentication is outside the stability fixture');}
}
