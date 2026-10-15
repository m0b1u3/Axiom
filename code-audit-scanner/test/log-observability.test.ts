import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { ScanProgressSink } from '../src/runtime/scan-progress.js';
import { formatActivityLine } from '../src/runtime/session-summary.js';

test('activity preserves diagnostic content and redacts secrets before shortening',()=>{
 const line=formatActivityLine({status:'failed',kind:'command',description:'x'.repeat(600)+'\napi_key="test-secret-value" sk-123456789012345678901234'});
 assert.ok(line.includes('\n'));
 assert.ok(line.length>600);
 assert.ok(!line.includes('test-secret-value'));
 assert.ok(!line.includes('sk-123456789012345678901234'));
 assert.match(line,/REDACTED/);
});

test('concurrent progress writes remain valid and failed calls keep error severity',async()=>{
 const dir=await mkdtemp(path.join(tmpdir(),'axiom-log-test-'));
 try{
 const sink=new ScanProgressSink(dir,77);await sink.init();
 await Promise.all(Array.from({length:20},(_,i)=>sink.activity(`${i===4?'failed':'completed'} command ${'content'.repeat(80)} ${i}`)));
 await sink.finish('completed');
 const snapshot=JSON.parse(await readFile(sink.progressPath,'utf8'));
 assert.equal(snapshot.phase,'completed');
 const events=(await readFile(sink.eventsPath,'utf8')).trim().split('\n').map(x=>JSON.parse(x));
 assert.equal(events.length,22);
 assert.equal(events.filter(x=>x.level==='error').length,1);
 assert.ok(events.some(x=>x.kind==='activity'&&x.message.length>240));
 assert.equal(events.at(-1).message,'scan completed');
 }finally{await rm(dir,{recursive:true,force:true});}
});
