import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,mkdirSync,writeFileSync,readFileSync,rmSync} from 'node:fs';
import path from 'node:path';
import {tmpdir} from 'node:os';
import {AuditCheckpoints} from '../src/runtime/audit-checkpoints.js';
test('empty workers are retained as incomplete coverage; malformed drafts do not replace saved JSON',()=>{
 const dir=mkdtempSync(path.join(tmpdir(),'axiom-checkpoints-'));const warnings:string[]=[];const c=new AuditCheckpoints(dir,s=>warnings.push(s));
 try{
  c.session({threadId:'worker',parentThreadId:'root',event:{payload:{type:'task_complete',last_agent_message:null}}});
  c.session({threadId:'root',parentThreadId:null,event:{payload:{type:'message',role:'user',content:'private prompt'}}});
  mkdirSync(path.join(dir,'raw/codex-security'),{recursive:true});const file=path.join(dir,'raw/codex-security/findings.json');writeFileSync(file,'{"findings":[]}');c.snapshot();writeFileSync(file,'{');c.snapshot();
  assert.deepEqual(JSON.parse(readFileSync(path.join(dir,'checkpoints/findings.json'),'utf8')),{findings:[]});
  const result:any={status:'completed',coverage:{completeness:'complete'}};c.attach(result);
  assert.equal(result.status,'partial');assert.equal(result.coverage.completeness,'partial');assert.equal(warnings.length,1);
  assert.doesNotMatch(readFileSync(path.join(dir,'checkpoints/evidence.jsonl'),'utf8'),/private prompt/);
  c.session({threadId:'worker',parentThreadId:'root',event:{payload:{type:'task_complete',last_agent_message:'Reviewed scope; no findings; limitation noted'}}});
  const recovered:any={status:'completed'};c.attach(recovered);assert.deepEqual(recovered.coverage.axiomCheckpoints.emptyWorkerThreadIds,[]);
 }finally{c.close();rmSync(dir,{recursive:true,force:true});}
});
