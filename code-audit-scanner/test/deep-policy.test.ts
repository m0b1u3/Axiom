import assert from "node:assert/strict";
import {test} from "node:test";
import {deepScanOptions,hardTimeoutMinutes} from "../src/contract/deep-policy.js";
import type {ScanTask} from "../src/contract/scan-task.js";
const task:ScanTask={taskId:1,sourcePath:"/source",outputPath:"/output",provider:"openai",model:"test",scanMode:"deep",timeoutMinutes:3};
test("native deadline precedes outer deadline, workers stay SDK default",()=>{
 assert.deepEqual(deepScanOptions(task),{maxTimeMinutes:3});assert.equal(hardTimeoutMinutes(task),13);
 assert.deepEqual(deepScanOptions({...task,scanMode:"standard"}),{});assert.equal(hardTimeoutMinutes({...task,scanMode:"standard"}),3);
 assert.deepEqual(deepScanOptions({...task,deepMaxDiscoveryRuns:8,deepStopAfterNoNew:2}),{maxTimeMinutes:3,maxDiscoveryRuns:8,stopAfterNoNew:2});
});
test("deep-only controls reject unsupported modes and invalid counts",()=>{
 for(const value of [0,-1,1.5,Infinity,NaN,Number.MAX_SAFE_INTEGER+1])assert.throws(()=>deepScanOptions({...task,deepMaxDiscoveryRuns:value}));
 assert.throws(()=>deepScanOptions({...task,scanMode:"standard",deepStopAfterNoNew:2}));
});
