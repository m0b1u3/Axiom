import assert from "node:assert/strict";
import {test} from "node:test";
import {mkdtemp,writeFile} from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import {convertCodexResult} from "../src/converter/codex-finding-converter.js";
import {loadRawCodexResult} from "../src/artifact/load-raw-result.js";
const coverage={completeness:"partial",includePaths:["api"],excludePaths:["vendor"],surfaces:[{id:"auth",label:"授权",disposition:"needs_follow_up",receiptRefs:["artifacts/auth.json"]}],explicitExclusions:[{pattern:"vendor",reason:"第三方"}],deferred:[{id:"auth",reason:"缺少环境"}],openQuestions:[{question:"租户边界？"}],futureField:{retained:true}};
test("partial coverage with zero findings is partial and keeps every report field",()=>{
 const r=convertCodexResult({taskId:1,sourcePath:"/source",outputPath:"/out",provider:"openai",model:"test",scanMode:"deep",timeoutMinutes:1},{manifest:{scan:{status:"completed"}},findings:{findings:[]},coverage},{startedAt:"now",finishedAt:"now",rawArtifactDir:"raw",normalizedResultPath:"result"});
 assert.equal(r.status,"partial");assert.equal(r.findings.length,0);assert.deepEqual(r.coverage,coverage);
});
test("empty findings do not discard a preserved coverage report on interruption",async()=>{
 const dir=await mkdtemp(path.join(os.tmpdir(),"axiom-coverage-"));await writeFile(path.join(dir,"findings.json"),JSON.stringify({findings:[]}));await writeFile(path.join(dir,"coverage.json"),JSON.stringify(coverage));const value=await loadRawCodexResult(dir);assert.deepEqual(value?.coverage,coverage);assert.deepEqual(value?.findings?.findings,[]);
});
