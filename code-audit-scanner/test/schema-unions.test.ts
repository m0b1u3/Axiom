import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {createRequire} from 'node:module';
import {flattenSchemaAnyOf,flattenedResponseSchema} from '../src/proxy/schema-unions.js';
const Ajv=createRequire(new URL('../../contracts/package.json',import.meta.url))('ajv');

test('native SDK matching schema is equivalent after flattening its bare nullable nested union',async()=>{
 const original=JSON.parse(await readFile(new URL('fixtures/sdk050-matching-schema.json',import.meta.url),'utf8'));
 const result=flattenSchemaAnyOf(original);assert.equal(result.changed,true);
 const flat=result.schema as any;assert.equal(flat.properties.request.anyOf.length,3);assert.equal(original.properties.request.anyOf.length,2);
 const compiler=new Ajv({strict:false}),before=compiler.compile(original),after=compiler.compile(flat);
 const requests=[null,{kind:'catalogue',page:0},{kind:'catalogue',page:-1},{kind:'catalogue',page:0,extra:true},{kind:'evidence',beforeOccurrenceIds:['a'],afterOccurrenceIds:['b'],offset:0},{kind:'evidence',beforeOccurrenceIds:[],afterOccurrenceIds:[],offset:0},{kind:'evidence',beforeOccurrenceIds:['a'],afterOccurrenceIds:['b'],offset:-1},'evidence',{},false,1,[]];
 for(const request of requests)for(const matches of [[],[{beforeOccurrenceIds:['a'],afterOccurrenceIds:['b'],confidence:'high',reason:'source'}],[{beforeOccurrenceIds:[],afterOccurrenceIds:['b'],confidence:'high',reason:'source'}]])for(const extra of [{},{extra:true}]){
  const value={matches,uncertain:[],related:[],request,...extra};assert.equal(after(value),before(value),JSON.stringify(value));
 }
 assert.equal(before({matches:[],uncertain:[],related:[],request:null}),true);
 assert.deepEqual(flattenSchemaAnyOf(flat),{schema:flat,changed:false});
 const body={model:'deepseek-flash',input:'Fixtures / Mock',text:{format:{type:'json_schema',strict:true,name:'native',schema:original}}};const output=JSON.parse(flattenedResponseSchema(Buffer.from(JSON.stringify(body))).toString());assert.equal(output.text.format.strict,true);assert.equal(output.text.format.type,'json_schema');assert.equal(output.input,body.input);assert.deepEqual(output.text.format.schema,flat);
});

test('sibling assertions and data are never removed; unstructured and already flat requests stay byte identical',()=>{
 const schema={type:'object',properties:{x:{anyOf:[{anyOf:[{type:'string'},{type:'number'}],description:'preserve branch'},{type:'null'}]},literal:{const:{anyOf:[{anyOf:[1,2]}]},examples:[{anyOf:[{anyOf:[1]}]}]}}};assert.deepEqual(flattenSchemaAnyOf(schema),{schema,changed:false});
 const referenced={anyOf:[{anyOf:[{type:'string'},{type:'number'}]}],properties:{pointer:{$ref:'#/anyOf/0/anyOf/0'}}};assert.deepEqual(flattenSchemaAnyOf(referenced),{schema:referenced,changed:false});
 for(const body of [{input:'Fixtures / Mock'},{text:{format:{type:'json_object',schema}}},{text:{format:{type:'json_schema',strict:true,schema}}}]){const bytes=Buffer.from(JSON.stringify(body,null,2));assert.equal(flattenedResponseSchema(bytes),bytes);}
});

test('Responses transport flattens only when explicitly enabled and preserves strict format and response',async t=>{
 const {createServer}=await import('node:http'),{mkdtemp,rm}=await import('node:fs/promises'),{tmpdir}=await import('node:os'),path=await import('node:path');
 const {startReliableUpstream}=await import('../src/proxy/reliable-upstream.js');
 const folder=await mkdtemp(path.join(tmpdir(),'axiom-union-wire-'));t.after(()=>rm(folder,{recursive:true,force:true}));
 const schema=JSON.parse(await readFile(new URL('fixtures/sdk050-matching-schema.json',import.meta.url),'utf8')),captured:any[]=[];
 const response={id:'fixture_response',status:'completed',output:[{type:'message',role:'assistant',content:[{type:'output_text',text:'{"matches":[],"uncertain":[],"related":[],"request":null}'}]}],usage:{input_tokens:1,output_tokens:1,total_tokens:2}};
 const server=createServer((req,res)=>{void(async()=>{const chunks=[];for await(const part of req)chunks.push(part);captured.push(JSON.parse(Buffer.concat(chunks).toString()));res.writeHead(200,{'content-type':'application/json'});res.end(JSON.stringify(response));})()});
 await new Promise<void>(r=>server.listen(0,'127.0.0.1',r));t.after(()=>new Promise<void>(r=>server.close(()=>r())));const address=server.address() as {port:number};
 for(const enabled of [false,true]){
  const proxy=await startReliableUpstream(`http://127.0.0.1:${address.port}`,path.join(folder,`gate-${enabled}.sqlite`),()=>{},undefined,'native_schema',false,undefined,{nestedAnyOf:enabled});
  try{const reply=await fetch(proxy.baseUrl+'/responses',{method:'POST',body:JSON.stringify({model:'fixture',input:'Fixtures / Mock',text:{format:{type:'json_schema',name:'original',strict:true,schema}}})});assert.equal(reply.status,200);assert.deepEqual(await reply.json(),response);const body=captured.at(-1);assert.equal(body.text.format.strict,true);assert.equal(body.text.format.name,'original');assert.equal(body.text.format.type,'json_schema');assert.deepEqual(body.text.format.schema,enabled?flattenSchemaAnyOf(schema).schema:schema);}finally{await proxy.close()}
 }
 assert.equal(captured.length,2,'one request per explicit execution; no transport replay');
});
