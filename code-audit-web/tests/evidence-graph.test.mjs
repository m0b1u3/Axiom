import {test} from 'node:test';import assert from 'node:assert/strict';import {readFile} from 'node:fs/promises';import ts from 'typescript';
const code=ts.transpileModule(await readFile(new URL('../src/utils/evidence-graph.ts',import.meta.url),'utf8'),{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText;
const {evidenceGraphView,graphSource,graphSlice,graphPositions,layoutEvidenceGraph}=await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
const fixture=JSON.parse(await readFile(new URL('../../contracts/fixtures/v1/evidence-graph.json',import.meta.url),'utf8'));
// Browser routes use safe numeric DB IDs; the portable contract independently
// tests string identities larger than Number.MAX_SAFE_INTEGER.
function local(){const v=structuredClone(fixture);v.execution.taskId='17';return v;}
test('Web evidence guard binds task, attempt, finding and source snapshot',()=>{
 const v=local();assert.ok(evidenceGraphView(v,17,2,v.findingFingerprint,v.sourceContentHash));
 for(const mutate of [x=>x.execution.taskId='18',x=>x.execution.attempt++,x=>x.findingFingerprint='other',x=>x.sourceContentHash='b'.repeat(64),x=>x.projectorVersion=2,x=>x.nodes[0].state='verified',x=>x.nodes[0].sourcePointer='/raw/missing',x=>x.edges[0].to='missing',x=>x.nodes.push(x.nodes[0]),x=>x.edges.push(x.edges[0]),x=>x.nodes[1].code='unrelated',x=>x.nodes[1].location.startLine=3,x=>x.edges.find(e=>e.kind==='supports').from=x.nodes[2].id]){const f=local();mutate(f);assert.equal(evidenceGraphView(f,17,2,v.findingFingerprint,v.sourceContentHash),null);}
});
test('graph and list views share deterministic nodes without manufacturing links',()=>{
 const v=local(),path=graphSlice(v,'path');assert.ok(path.nodes.length>0);assert.ok(path.edges.every(e=>e.kind==='describes_next'||e.kind==='constrained_by'));
 const limited=graphSlice(v,'relations',3);assert.equal(limited.nodes.length,3);assert.equal(limited.total,v.nodes.length);assert.ok(limited.edges.every(e=>limited.nodes.some(n=>n.id===e.from)&&limited.nodes.some(n=>n.id===e.to)));
 assert.deepEqual(graphPositions(v.nodes,'relations'),graphPositions(v.nodes,'relations'));
 assert.equal(graphSlice({...v,nodes:[],edges:[]},'path').nodes.length,0);
});
test('raw pointer access handles escaped keys, null and hostile property names',()=>{
 assert.equal(graphSource(fixture.sourceFinding,'/raw/unknownNative/x~1y~0z').value,'原始字段');
 assert.deepEqual(graphSource({value:null},'/value'),{found:true,value:null});assert.equal(graphSource({},'/__proto__').found,false);assert.equal(graphSource(['x'],'/01').found,false);assert.equal(graphSource({},'/~2').found,false);
});
test('SDK review status cannot become verified and code observation requires source evidence',()=>{
 const v=local();assert.equal(v.sourceFinding.raw.validation.status,'verified');assert.ok(v.nodes.every(n=>n.state==='inferred'));
 const n=v.nodes.find(n=>n.kind==='code_evidence');n.state='observed';assert.equal(evidenceGraphView(v,17,2,v.findingFingerprint),null);
 n.sourceCheck='matched';n.sourceFileSha256='c'.repeat(64);assert.ok(evidenceGraphView(v,17,2,v.findingFingerprint));
});

test('container layout keeps every node in bounds and separated at all column thresholds',()=>{
 const saved=JSON.stringify(fixture.nodes);
 for(const font of [10,12,16])for(const view of ['relations','path']){
  let columns=0;
  for(let width=180;width<=1200;width+=13){
   const layout=layoutEvidenceGraph(fixture.nodes,view,{width,nodeWidth:font*12.5,nodeHeight:(font+4)*2+16,gap:24,padding:24,minHeight:408});
   assert.ok(layout.columns>=columns);columns=layout.columns;
   assert.equal(new Set(layout.positions.map(p=>p.id)).size,fixture.nodes.length);
   for(const p of layout.positions){
    assert.ok(p.x-layout.nodeWidth/2>=0&&p.x+layout.nodeWidth/2<=width+.001);
    assert.ok(p.y-layout.nodeHeight/2>=0&&p.y+layout.nodeHeight/2<=layout.height+.001);
   }
   for(let i=0;i<layout.positions.length;i++)for(let j=i+1;j<layout.positions.length;j++){
    const a=layout.positions[i],b=layout.positions[j];
    assert.ok(Math.abs(a.x-b.x)>=layout.nodeWidth+24-.001||Math.abs(a.y-b.y)>=layout.nodeHeight+24-.001,`overlap at ${width}px / ${font}px / ${view}`);
   }
  }
 }
 assert.equal(JSON.stringify(fixture.nodes),saved,'layout must not mutate archived evidence');
});

test('single-column, single-node, empty and expanded evidence retain finite pixel extents',()=>{
 for(const count of [0,1,2,16,48,256])for(const width of [180,240,370,372,546,720,960]){
  const nodes=Array.from({length:count},(_,i)=>({...fixture.nodes[i%fixture.nodes.length],id:'layout-fixture-'+i}));
  const options={width,nodeWidth:150,nodeHeight:48,gap:24,padding:24,minHeight:408};
  const a=layoutEvidenceGraph(nodes,'relations',options),b=layoutEvidenceGraph(nodes,'relations',options);
  assert.deepEqual(a,b);assert.equal(a.positions.length,count);
  assert.ok(Object.values(a.bounds).every(Number.isFinite));assert.ok(a.bounds.width>0&&a.bounds.height>0);
  if(width<372)assert.equal(a.columns,1);
  if(count===1){assert.equal(a.positions[0].x,width/2);assert.equal(a.positions[0].y,a.height/2);assert.equal(a.bounds.width,2);assert.equal(a.bounds.height,2);}
 }
});
