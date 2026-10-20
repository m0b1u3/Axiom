import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {evidencePointer,validate,type Finding} from '@axiom/contracts';
import {buildFindingEvidence} from '../src/converter/finding-evidence.js';
import type {ScanTask} from '../src/contract/scan-task.js';
const source=JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/evidence-source.json',import.meta.url),'utf8')) as Finding;
const task={taskId:'9007199254740993',executionAttempt:2,sourceContentHash:'2'.repeat(64)} as ScanTask;
test('SDK evidence projection is deterministic, scoped and agrees with shared fixtures',async()=>{
 const a=buildFindingEvidence(source,task,'0.1.25'),b=buildFindingEvidence(source,task,'0.1.25');
 assert.deepEqual(a,b);assert.deepEqual(a,JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/evidence-facts.json',import.meta.url),'utf8')));
 assert.ok([...a.nodes,...a.edges].every(n=>evidencePointer(source,n.sourcePointer).found));
 const next=buildFindingEvidence(source,{...task,executionAttempt:3});assert.notEqual(a.nodes[0]!.id,next.nodes[0]!.id);
});
test('explicit refs attach to the named evidence, never to an array position',()=>{
 const v=structuredClone(source);(v.raw as any).codeEvidence.reverse();const g=buildFindingEvidence(v,task);
 const target=g.nodes.find(n=>n.kind==='root_cause')!;const supports=g.edges.filter(e=>e.to===target.id&&e.kind==='supports');
 assert.equal(supports.length,2);assert.deepEqual(supports.map(e=>g.nodes.find(n=>n.id===e.from)!.sdkEvidenceId).sort(),['E-sink','E-source']);
 assert.ok(g.edges.filter(e=>e.kind==='describes_next').every(e=>e.state==='inferred'));
});
test('duplicate and missing references remain explicit gaps without guessing',()=>{
 const v=structuredClone(source),raw=v.raw as any;raw.codeEvidence[1].id='E-source';raw.rootCause.evidenceRefs=['E-source','absent'];
 const g=buildFindingEvidence(v,task),target=g.nodes.find(n=>n.kind==='root_cause')!;
 assert.equal(g.edges.filter(e=>e.to===target.id&&e.kind==='supports').length,0);
 assert.ok(g.diagnostics.some(d=>d.code==='duplicate_evidence_id'));assert.ok(g.diagnostics.some(d=>d.code==='missing_reference'));assert.equal(g.completeness,'partial');
});
test('SDK verified text and code snippets never grant observed or verified status',()=>{
 const g=buildFindingEvidence(source,task);assert.ok(g.nodes.every(n=>n.state==='inferred'&&n.verification===null&&n.sourceCheck==='not_checked'));
 assert.ok(g.nodes.some(n=>n.kind==='validation'));assert.ok(g.edges.some(e=>e.kind==='contradicts'));assert.equal((source.raw as any).validation.status,'verified');
});
test('all SDK dataflow aliases retain explicit nested references and transformations',()=>{
 for(const key of ['dataFlow','data_flow','dataflow']){const f=structuredClone(source),path=(f.raw as any).attackPath;path[key]=path.dataFlow;if(key!=='dataFlow')delete path.dataFlow;const g=buildFindingEvidence(f,task);assert.ok(g.nodes.some(n=>n.sourcePointer===`/raw/attackPath/${key}/source`));assert.ok(g.edges.some(e=>e.sourcePointer.includes(`/${key}/evidence_refs/`)));}
});
test('prose does not become a call graph and oversized evidence has explicit diagnostics',()=>{
 const f=structuredClone(source);(f.raw as any).attackPath='A -> B -> database';let g=buildFindingEvidence(f,task);
 assert.equal(g.edges.filter(e=>e.kind==='describes_next').length,0);assert.ok(g.diagnostics.some(d=>d.code==='unstructured_path'));
 (f.raw as any).codeEvidence=Array.from({length:300},(_,i)=>({id:'E-'+i,label:'Fixture '+i,code:'x',path:'fixture.ts',startLine:1}));g=buildFindingEvidence(f,task);
 assert.equal(g.nodes.length,256);assert.equal(g.completeness,'partial');assert.ok(g.diagnostics.some(d=>d.code==='projection_limit'));validate('FindingEvidence',g,task.executionAttempt?{taskId:String(task.taskId),attempt:task.executionAttempt}:undefined);
});
