import {test} from "node:test";
import assert from "node:assert/strict";
import {readFile} from "node:fs/promises";
import {decode} from "@axiom/contracts";
import {scanTaskFromJob} from "../src/contract/scanner-job.js";
import {buildCodexOverrides} from "../src/provider/model-provider.js";
import {deepScanOptions,hardTimeoutMinutes} from "../src/contract/deep-policy.js";

test("job input preserves string identity, comma paths, model and budget",async()=>{
 const value=JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/job-standard.json',import.meta.url),'utf8'));
 value.execution.taskId='9007199254740993';value.policy.includePaths=['src/a,b.ts','源码/测试.ts'];value.policy.maxCostUsd=4.25;
 value.model={providerId:'openai',connectionType:'openai',model:'fixture-selected-model',wireApi:'responses',credentialEnv:'OPENAI_API_KEY',configurationRevision:'fixture-revision'};
 const task=scanTaskFromJob(decode('ScannerJob',JSON.stringify(value)));
 assert.equal(task.taskId,'9007199254740993');assert.equal(task.maxCostUsd,4.25);assert.deepEqual(task.includePaths,value.policy.includePaths);
 assert.deepEqual(buildCodexOverrides(task),{model:'fixture-selected-model'});
});
test("job input uses explicit deadlines and preserves SDK worker defaults",async()=>{
 const value=JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/job-deep.json',import.meta.url),'utf8'));
 value.policy.timeBudget={scanSeconds:2820,finalizationGraceSeconds:120};delete value.policy.workers;delete value.policy.maxCostUsd;
 const task=scanTaskFromJob(decode('ScannerJob',JSON.stringify(value)));
 assert.equal(task.workers,undefined);assert.equal(task.maxCostUsd,undefined);assert.equal(hardTimeoutMinutes(task),49);assert.equal(deepScanOptions(task).maxTimeMinutes,47);
});
test("resolved providers are supported by connection metadata without a brand preset",async()=>{
 const value=JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/job-windows.json',import.meta.url),'utf8'));
 value.model.providerId='fixture-new-provider';value.model.connectionType='openai_compatible';value.model.baseUrl='https://fixture.invalid/v1';value.model.wireApi='responses';value.model.credentialEnv='FIXTURE_MODEL_KEY';
 const task=scanTaskFromJob(decode('ScannerJob',JSON.stringify(value)));
 const config=buildCodexOverrides(task)!;
 assert.equal(task.sourcePath,value.source.path);assert.equal(task.outputPath,value.output.path);
 const provider=(config['model_providers'] as Record<string,Record<string,any>>)['fixture-new-provider']!;
 assert.equal(provider.env_key,undefined);assert.equal(provider.auth.args[1],'FIXTURE_MODEL_KEY');assert.equal(provider.requires_openai_auth,false);
});

test('historical Chat contracts remain decodable but cannot become executable tasks',async()=>{
 const value=JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/job-windows.json',import.meta.url),'utf8'));
 value.model.wireApi='chat';value.model.connectionType='openai_compatible';value.model.baseUrl='https://fixture.invalid/v1';
 const job=decode('ScannerJob',JSON.stringify(value));assert.equal(job.model.wireApi,'chat');
 assert.throws(()=>scanTaskFromJob(job),/Chat 协议适配已移除/);
});
